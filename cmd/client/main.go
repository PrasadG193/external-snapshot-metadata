package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	cbtv1alpha1 "github.com/PrasadG193/external-snapshot-metadata/pkg/api/cbt/v1alpha1"
	"github.com/PrasadG193/external-snapshot-metadata/pkg/kube"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgrpc "github.com/PrasadG193/external-snapshot-metadata/pkg/grpc"
)

func main() {

	var baseVolumeSnapshot, targetVolumeSnapshot, snapNamespace, clientSA, clientNamespace string
	flag.StringVar(&baseVolumeSnapshot, "base", "", "base volume snapshot name")
	flag.StringVar(&targetVolumeSnapshot, "target", "", "target volume snapshot name")
	flag.StringVar(&snapNamespace, "namespace", "default", "snapshot namespace")
	flag.StringVar(&clientSA, "service-account", "default", "client service account")
	flag.StringVar(&clientNamespace, "client-namespace", "default", "client namespace")
	flag.Parse()

	if baseVolumeSnapshot == "" || targetVolumeSnapshot == "" {
		log.Fatal("base or target volumesnapshot is missing")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := NewSnapshotMetadata()
	snapMetadataSvc, saToken, err := client.setupSession(
		ctx,
		baseVolumeSnapshot,
		targetVolumeSnapshot,
		snapNamespace,
		clientSA,
		clientNamespace)
	if err != nil {
		log.Fatalf("could not get session params %v", err)
	}
	fmt.Println("SA TOKEN::", saToken)

	if err := client.getChangedBlocks(ctx, snapMetadataSvc, baseVolumeSnapshot, targetVolumeSnapshot, saToken); err != nil {
		log.Fatalf("could not get changed blocks %v", err)
	}
}

type Client struct {
	client  pgrpc.SnapshotMetadataClient
	kubeCli kubernetes.Interface
	rtCli   client.Client
}

func NewSnapshotMetadata() Client {
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("could not init in cluster config %v", err)
	}
	rtCli, err := client.New(kubeConfig, client.Options{})
	if err != nil {
		log.Fatalf("failed to create dynamic client %v", err)
	}
	kubeCli, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Fatalf("failed to create dynamic client %v", err)
	}
	return Client{
		rtCli:   rtCli,
		kubeCli: kubeCli,
	}
}

func (c *Client) createSAToken(ctx context.Context, vsList []string, sa, namespace string) (string, error) {
	// https://pkg.go.dev/k8s.io/client-go@v0.27.4/kubernetes/typed/core/v1#ServiceAccountInterface
	expiry := int64(10 * 60)
	// https://pkg.go.dev/k8s.io/api/authentication/v1#TokenRequest
	tokenReq := authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			Audiences:         vsList, // We can add the volumesnapshot names for which the token is intended for
			ExpirationSeconds: &expiry,
		},
	}
	token, err := c.kubeCli.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, sa, &tokenReq, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return token.Status.Token, nil

}

func (c *Client) initGRPCClient(cacert []byte, URL string) {
	tlsCredentials, err := loadTLSCredentials(cacert)
	if err != nil {
		log.Fatal("cannot load TLS credentials: ", err)
	}
	conn, err := grpc.Dial(URL, grpc.WithTransportCredentials(tlsCredentials))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	c.client = pgrpc.NewSnapshotMetadataClient(conn)

}

// Create session and get session parameters with custom resource CSISnapshotSessionAccess
func (c *Client) setupSession(ctx context.Context,
	baseSnap,
	targetSnap,
	snapNamespace,
	clientSA,
	clientNamespace string,
) (*cbtv1alpha1.SnapshotMetadataService, string, error) {
	// 1. Find Driver name for the snapshot
	fmt.Printf("## Find driver name for the snapshots\n")
	_, driver, err := kube.GetVolSnapshotInfo(ctx, c.rtCli, baseSnap, snapNamespace)
	if err != nil {
		return nil, "", err
	}

	// 2. Discover SnapshotMetadataService resource for the driver
	sms, err := kube.FindSnapshotMetadataService(ctx, c.rtCli, driver)
	if err != nil {
		return nil, "", err
	}
	audiences := sms.Spec.Audiences

	// 3. Create SA Token with audiences
	saToken, err := c.createSAToken(ctx, audiences, clientSA, clientNamespace)
	if err != nil {
		return nil, "", err
	}
	return sms, saToken, nil
}

// Get changed blocks metadata with GetDelta rpc. The session needs to be created before making the rpc call
// The session is created using CSISnapshotSessionAccess resource
// Server auth at client side is done with CA Cert received in session params
// The token is used to in the req parameter which is used by the server to authenticate the client
func (c *Client) getChangedBlocks(
	ctx context.Context,
	snapMetaSvc *cbtv1alpha1.SnapshotMetadataService,
	baseVolumeSnapshot string,
	targetVolumeSnapshot string,
	saToken string,
) error {
	fmt.Printf("\n## Making gRPC Call on %s endpoint to Get Changed Blocks Metadata...\n\n", snapMetaSvc.Spec.Address)

	c.initGRPCClient(snapMetaSvc.Spec.CACert, snapMetaSvc.Spec.Address)
	stream, err := c.client.GetDelta(ctx, &pgrpc.GetDeltaRequest{
		SessionToken:       saToken,
		BaseSnapshot:       baseVolumeSnapshot,
		TargetSnapshot:     targetVolumeSnapshot,
		StartingByteOffset: 0,
		MaxResults:         uint32(256),
	})
	if err != nil {
		return err
	}
	done := make(chan bool)
	fmt.Println("Resp received:")
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				done <- true //means stream is finished
				return
			}
			if err != nil {
				log.Fatalf("cannot receive %v", err)
			}
			respJson, _ := json.Marshal(resp)
			fmt.Println(string(respJson))
		}
	}()

	<-done //we will wait until all response is received
	log.Printf("finished")
	return nil
}

func loadTLSCredentials(cacert []byte) (credentials.TransportCredentials, error) {
	// Add custom CA to the cert pool
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(cacert) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	config := &tls.Config{
		RootCAs: certPool,
	}
	return credentials.NewTLS(config), nil
}

func jsonify(obj interface{}) string {
	jsonBytes, _ := json.MarshalIndent(obj, "", "  ")
	return string(jsonBytes)
}
