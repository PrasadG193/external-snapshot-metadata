package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	cbtv1alpha1 "github.com/PrasadG193/external-snapshot-metadata/pkg/api/cbt/v1alpha1"
	volsnapv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/PrasadG193/external-snapshot-metadata/pkg/authn"
	"github.com/PrasadG193/external-snapshot-metadata/pkg/authz"
	pgrpc "github.com/PrasadG193/external-snapshot-metadata/pkg/grpc"
	"github.com/PrasadG193/external-snapshot-metadata/pkg/kube"
)

const (
	PROTOCOL = "unix"
	SOCKET   = "/csi/csi.sock"

	podNamespaceEnvKey = "POD_NAMESPACE"
	driverNameEnvKey   = "DRIVER_NAME"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(cbtv1alpha1.AddToScheme(scheme))
	utilruntime.Must(volsnapv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func main() {
	listener, err := net.Listen("tcp", ":9000")
	if err != nil {
		panic(err)
	}

	tlsCredentials, err := loadTLSCredentials()
	if err != nil {
		log.Fatal("cannot load TLS credentials: ", err)
	}
	s := grpc.NewServer(
		grpc.Creds(tlsCredentials),
	)
	reflection.Register(s)
	pgrpc.RegisterSnapshotMetadataServer(s, newServer())
	log.Println("SERVER STARTED!")
	if err := s.Serve(listener); err != nil {
		log.Fatal(err)
	}
}

func newServer() *Server {
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
	return &Server{
		rtCli:   rtCli,
		kubeCli: kubeCli,
	}
}

type Server struct {
	client pgrpc.SnapshotMetadataClient
	pgrpc.UnimplementedSnapshotMetadataServer
	rtCli   client.Client
	kubeCli kubernetes.Interface
}

func (s *Server) convertParams(ctx context.Context, req *pgrpc.GetDeltaRequest) (*pgrpc.GetDeltaRequest, error) {
	return s.validateAndTranslateParams(ctx, req)
}

func (s *Server) validateAndTranslateParams(ctx context.Context, req *pgrpc.GetDeltaRequest) (*pgrpc.GetDeltaRequest, error) {
	newReq := pgrpc.GetDeltaRequest{}
	log.Println("Translating snapshot names to IDs ")
	// The session token is valid for basesnapshot
	baseSnapHandle, _, err := kube.GetVolSnapshotInfo(ctx, s.rtCli, req.BaseSnapshot, req.Namespace)
	if err != nil {
		return nil, err
	}
	log.Printf("Mapping snapshot %s to snapshot id %s\n", req.BaseSnapshot, baseSnapHandle)
	newReq.BaseSnapshot = baseSnapHandle

	targetSnapHandle, _, err := kube.GetVolSnapshotInfo(ctx, s.rtCli, req.TargetSnapshot, req.Namespace)
	if err != nil {
		return nil, err
	}
	log.Printf("Mapping snapshot %s to snapshot id %s\n", req.TargetSnapshot, targetSnapHandle)
	newReq.TargetSnapshot = targetSnapHandle

	newReq.StartingByteOffset = req.StartingByteOffset
	newReq.MaxResults = req.MaxResults
	return &newReq, nil
}

func (s *Server) getAudienceForDriver(ctx context.Context) (string, error) {
	driver := os.Getenv(driverNameEnvKey)
	if driver == "" {
		return "", fmt.Errorf("Missing DRIVER_NAME env value")
	}
	sms, err := kube.FindSnapshotMetadataService(ctx, s.rtCli, driver)
	if err != nil {
		return "", err
	}
	return sms.Spec.Audience, nil
}

func (s *Server) authRequest(ctx context.Context, securityToken, namespace string) error {
	// Find audienceToken from SnapshotMetadataService
	log.Println("Discovering SnapshotMetadataService for the driver and fetching audience string")
	audience, err := s.getAudienceForDriver(ctx)
	if err != nil {
		log.Println("ERROR: ", err.Error())
		return err
	}

	// Authenticate request with session Token
	// TokenAuthenticator uses TokenReview K8s API to validate token
	log.Println("Authenticate request using TokenReview API")
	authenticator := authn.NewTokenAuthenticator(s.kubeCli)
	userInfo, err := authenticator.Authenticate(ctx, securityToken, audience)
	if err != nil {
		log.Println("ERROR: ", err.Error())
		return err
	}

	// Authorize request
	// SARAuthorizer uses SAR APIs to check if the user identity provided by
	// authorizer has access to VolumeSnapshot APIs
	log.Println("Authorize request using SubjectAccessReview APIs")
	authorizer := authz.NewSARAuthorizer(s.kubeCli)
	allowed, reason, err := authorizer.Authorize(ctx, namespace, userInfo)
	if err != nil {
		log.Println(err.Error(), reason)
		return err
	}
	if !allowed {
		log.Println("ERROR: Authorization failed.", reason)
		return fmt.Errorf("ERROR: Authorization failed, %s", reason)
	}
	return nil
}

func (s *Server) GetDelta(req *pgrpc.GetDeltaRequest, cbtClientStream pgrpc.SnapshotMetadata_GetDeltaServer) error {
	log.Println("Received request::", jsonify(req))
	ctx := context.Background()

	if err := s.authRequest(ctx, req.SecurityToken, req.Namespace); err != nil {
		log.Println("ERROR: ", err.Error())
		return err
	}

	s.initCSIGRPCClient()
	spReq, err := s.convertParams(ctx, req)
	if err != nil {
		log.Println("ERROR: ", err.Error())
		return err
	}

	// Call CSI Driver's GetDelta gRPC
	log.Println("Calling CSI Driver's gRPC over linux socket and streaming response back")
	csiStream, err := s.client.GetDelta(ctx, spReq)
	if err != nil {
		log.Println("ERROR: ", err.Error())
		return err
	}
	done := make(chan bool)
	go func() {
		for {
			resp, err := csiStream.Recv()
			if err == io.EOF {
				log.Println("Received EOF")
				done <- true //means stream is finished
				return
			}
			if err != nil {
				log.Printf(fmt.Sprintf("cannot receive %v", err))
				return
			}
			log.Println("Received response from csi driver, proxying to client")
			if err := cbtClientStream.Send(resp); err != nil {
				log.Printf(fmt.Sprintf("cannot send %v", err))
				return
			}
		}
	}()
	<-done //we will wait until all response is received
	return nil
}

func (s *Server) initCSIGRPCClient() {
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, PROTOCOL, addr)
	}
	options := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(dialer),
	}
	conn, err := grpc.Dial(SOCKET, options...)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	s.client = pgrpc.NewSnapshotMetadataClient(conn)
}

func loadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load server's certificate and private key
	cert := os.Getenv("CBT_SERVER_CERT")
	key := os.Getenv("CBT_SERVER_KEY")
	serverCert, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.NoClientCert,
	}

	return credentials.NewTLS(config), nil
}

func jsonify(obj interface{}) string {
	jsonBytes, _ := json.MarshalIndent(obj, "", "  ")
	return string(jsonBytes)
}