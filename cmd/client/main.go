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
	"os"
	"time"

	cbtv1alpha1 "github.com/PrasadG193/external-snapshot-metadata/pkg/api/cbt/v1alpha1"
	"github.com/PrasadG193/external-snapshot-metadata/pkg/kube"
	volsnapv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	pgrpc "github.com/PrasadG193/external-snapshot-metadata/pkg/grpc"
)

var (
	scheme = runtime.NewScheme()
)

const DefaultTokenPath = "/var/run/secrets/tokens/%s"

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(cbtv1alpha1.AddToScheme(scheme))
	utilruntime.Must(volsnapv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func main() {
	var baseVolumeSnapshot, targetVolumeSnapshot, snapNamespace, clientSA, clientNamespace, mountedTokenPath string
	var useMountedToken bool
	flag.StringVar(&baseVolumeSnapshot, "base", "", "base volume snapshot name")
	flag.StringVar(&targetVolumeSnapshot, "target", "", "target volume snapshot name")
	flag.StringVar(&snapNamespace, "namespace", "default", "snapshot namespace")
	flag.StringVar(&clientSA, "service-account", "default", "client service account")
	flag.StringVar(&clientNamespace, "client-namespace", "default", "client namespace")
	flag.StringVar(&mountedTokenPath, "token-mount-path", DefaultTokenPath, "Path to the token mounted with projected volume")
	flag.BoolVar(&useMountedToken, "use-projected-token", false, "Use token mounted using project volume instead of creating new with TokenRequest")
	flag.Parse()

	if baseVolumeSnapshot == "" || targetVolumeSnapshot == "" {
		log.Fatal("base or target volumesnapshot is missing")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := NewSnapshotMetadata()
	snapMetadataSvc, saToken, err := client.setupSecurityAccess(
		ctx,
		baseVolumeSnapshot,
		targetVolumeSnapshot,
		snapNamespace,
		clientSA,
		clientNamespace,
		useMountedToken,
		mountedTokenPath)
	if err != nil {
		log.Fatalf("could not get connection params %v", err)
	}

	if err := client.getChangedBlocks(ctx, snapMetadataSvc, baseVolumeSnapshot, targetVolumeSnapshot, saToken, snapNamespace); err != nil {
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
	rtCli, err := client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme})
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

func (c *Client) createSAToken(ctx context.Context, audience string, sa, namespace string) (string, error) {
	// https://pkg.go.dev/k8s.io/client-go@v0.27.4/kubernetes/typed/core/v1#ServiceAccountInterface
	expiry := int64(10 * 60)
	// https://pkg.go.dev/k8s.io/api/authentication/v1#TokenRequest
	tokenReq := authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			Audiences:         []string{audience},
			ExpirationSeconds: &expiry,
		},
	}
	tokenResp, err := c.kubeCli.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, sa, &tokenReq, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	log.Println("TokenRequest Response::", jsonify(tokenResp))

	return tokenResp.Status.Token, nil

}

func (c *Client) initGRPCClient(cacert []byte, URL, token, namespace string) {
	tlsCredentials, err := loadTLSCredentials(cacert)
	if err != nil {
		log.Fatal("cannot load TLS credentials: ", err)
	}
	perRPC := ServiceAccountAccess{token: token}
	conn, err := grpc.Dial(
		URL,
		grpc.WithTransportCredentials(tlsCredentials),
		grpc.WithPerRPCCredentials(perRPC),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	c.client = pgrpc.NewSnapshotMetadataClient(conn)

}

func (c *Client) getSecurityToken(
	ctx context.Context,
	useMountedToken bool,
	tokenPath,
	audience,
	clientSA,
	clientNamespace string,
) (string, error) {
	if useMountedToken {
		if tokenPath == "" {
			tokenPath = fmt.Sprintf(DefaultTokenPath, clientSA)
		}
		log.Printf("Reading mounted SA Token from %s", tokenPath)
		token, err := os.ReadFile(tokenPath)
		if err != nil {
			return "", err
		}
		return string(token), nil
	}
	log.Println("Creating SA Token using TokenRequest resource")
	return c.createSAToken(ctx, audience, clientSA, clientNamespace)
}

func (c *Client) setupSecurityAccess(
	ctx context.Context,
	baseSnap,
	targetSnap,
	snapNamespace,
	clientSA,
	clientNamespace string,
	useMountedToken bool,
	tokenPath string,
) (*cbtv1alpha1.SnapshotMetadataService, string, error) {
	// 1. Find Driver name for the snapshot
	fmt.Printf("\n## Discovering SnapshotMetadataService for the driver and creating SA Token \n\n")
	log.Print("Finding driver name for the snapshots")
	_, driver, err := kube.GetVolSnapshotInfo(ctx, c.rtCli, snapNamespace+"/"+baseSnap)
	if err != nil {
		return nil, "", err
	}

	// 2. Discover SnapshotMetadataService resource for the driver
	sms, err := kube.FindSnapshotMetadataService(ctx, c.rtCli, driver)
	if err != nil {
		return nil, "", err
	}
	audience := sms.Spec.Audience

	// 3. Create SA Token with audience
	saToken, err := c.getSecurityToken(ctx, useMountedToken, tokenPath, audience, clientSA, clientNamespace)
	if err != nil {
		return nil, "", err
	}
	return sms, saToken, nil
}

// Get changed blocks metadata with GetDelta rpc.
// The security token needs to be created either using TokenRequest API or ProjectedToken fields in Pod spec
// The token is used to in the req parameter which is used by the server to authenticate the client
// Server auth at client side is done with CA Cert found in SnapshotMetadataService resource
func (c *Client) getChangedBlocks(
	ctx context.Context,
	snapMetaSvc *cbtv1alpha1.SnapshotMetadataService,
	baseVolumeSnapshot,
	targetVolumeSnapshot,
	saToken,
	snapNamespace string,
) error {
	fmt.Printf("\n## Making gRPC Call on %s endpoint to Get Changed Blocks Metadata...\n\n", snapMetaSvc.Spec.Address)

	c.initGRPCClient(snapMetaSvc.Spec.CACert, snapMetaSvc.Spec.Address, saToken, snapNamespace)
	stream, err := c.client.GetDelta(ctx, &pgrpc.GetDeltaRequest{
		BaseSnapshotId:   snapNamespace + "/" + baseVolumeSnapshot,
		TargetSnapshotId: snapNamespace + "/" + targetVolumeSnapshot,
		StartingOffset:   0,
		MaxResults:       uint32(256),
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
