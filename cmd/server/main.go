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
	"strings"

	cbtv1alpha1 "github.com/PrasadG193/external-snapshot-metadata/pkg/api/cbt/v1alpha1"
	"github.com/PrasadG193/external-snapshot-metadata/pkg/authz"
	"github.com/kubernetes-csi/csi-lib-utils/connection"
	volsnapv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/PrasadG193/external-snapshot-metadata/pkg/authn"
	pgrpc "github.com/PrasadG193/external-snapshot-metadata/pkg/grpc"
	"github.com/PrasadG193/external-snapshot-metadata/pkg/kube"
)

const (
	podNamespaceEnvKey = "POD_NAMESPACE"
	driverNameEnvKey   = "DRIVER_NAME"
	csiAddressKey      = "CSI_ADDRESS"

	userInfoKey = "USERINFO"
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
	srv := newServer()

	tlsCredentials, err := loadTLSCredentials()
	if err != nil {
		log.Fatal("cannot load TLS credentials: ", err)
	}
	opts := []grpc.ServerOption{
		grpc.Creds(tlsCredentials),
		grpc.StreamInterceptor(srv.streamInterceptor),
	}
	s := grpc.NewServer(opts...)
	pgrpc.RegisterSnapshotMetadataServer(s, srv)
	log.Print("SERVER STARTED!")
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

	log.Print("Translating snapshot names to IDs ")
	// The session token is valid for basesnapshot
	baseSnapHandle, _, err := kube.GetVolSnapshotInfo(ctx, s.rtCli, req.BaseSnapshotId)
	if err != nil {
		return nil, err
	}
	log.Printf("Mapping snapshot %s to snapshot id %s\n", req.BaseSnapshotId, baseSnapHandle)
	newReq.BaseSnapshotId = baseSnapHandle

	targetSnapHandle, _, err := kube.GetVolSnapshotInfo(ctx, s.rtCli, req.TargetSnapshotId)
	if err != nil {
		return nil, err
	}
	log.Printf("Mapping snapshot %s to snapshot id %s\n", req.TargetSnapshotId, targetSnapHandle)
	newReq.TargetSnapshotId = targetSnapHandle

	newReq.StartingOffset = req.StartingOffset
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

func newContextWithUserInfo(ctx context.Context, userinfo *authv1.UserInfo) context.Context {
	return context.WithValue(ctx, userInfoKey, userinfo)
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}

func newWrappedStream(ctx context.Context, s grpc.ServerStream) grpc.ServerStream {
	return &wrappedStream{s, ctx}
}

func (s *Server) streamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	log.Println("Stream interceptor: performing auth checks for req", info.FullMethod)
	userinfo, err := s.authRequest(ss.Context())
	if err != nil {
		return err
	}
	return handler(srv, newWrappedStream(newContextWithUserInfo(ss.Context(), userinfo), ss))
}

func (s *Server) authRequest(ctx context.Context) (*authv1.UserInfo, error) {
	// Find Security Token
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "missing metadata")
	}
	securityToken := md["authorization"]
	if len(securityToken) == 0 {
		return nil, status.Errorf(codes.Unauthenticated, "authorization token is not provided")
	}
	// Find audienceToken from SnapshotMetadataService
	log.Print("Discovering SnapshotMetadataService for the driver and fetching audience string")
	audience, err := s.getAudienceForDriver(ctx)
	if err != nil {
		log.Print("ERROR: ", err.Error())
		return nil, status.Errorf(codes.Unauthenticated, err.Error())
	}

	// Authenticate request with session Token
	// TokenAuthenticator uses TokenReview K8s API to validate token
	log.Print("Authenticate request using TokenReview API")
	authenticator := authn.NewTokenAuthenticator(s.kubeCli)
	userInfo, err := authenticator.Authenticate(ctx, securityToken[0], audience)
	if err != nil {
		log.Print("ERROR: ", err.Error())
		return nil, err
	}
	return userInfo, err
}

func (s *Server) GetDelta(req *pgrpc.GetDeltaRequest, cbtClientStream pgrpc.SnapshotMetadata_GetDeltaServer) error {
	log.Print("Received request::", jsonify(req))

	// Get UserInfo from context
	ctx := cbtClientStream.Context()
	userInfo, ok := ctx.Value(userInfoKey).(*authv1.UserInfo)
	if !ok {
		log.Print("ERROR: Failed to get userinfo from context")
		return fmt.Errorf("ERROR: Failed to get userinfo from context")
	}

	namespace, err := findSnapshotNamespace(req.BaseSnapshotId)
	if err != nil {
		log.Print("ERROR: ", err.Error())
		return err
	}

	// Authorize request
	// SARAuthorizer uses SAR APIs to check if the user identity provided by
	// authorizer has access to VolumeSnapshot APIs
	log.Print("Authorize request using SubjectAccessReview APIs")
	authorizer := authz.NewSARAuthorizer(s.kubeCli)
	allowed, reason, err := authorizer.Authorize(ctx, namespace, userInfo)
	if err != nil {
		log.Print(err.Error(), reason)
		return err
	}
	if !allowed {
		log.Print("ERROR: Authorization failed.", reason)
		return fmt.Errorf("ERROR: Authorization failed, %s", reason)
	}

	s.initCSIGRPCClient()
	spReq, err := s.convertParams(ctx, req)
	if err != nil {
		log.Print("ERROR: ", err.Error())
		return err
	}

	// Call CSI Driver's GetDelta gRPC
	log.Print("Calling CSI Driver's gRPC over linux socket and streaming response back")
	csiStream, err := s.client.GetDelta(ctx, spReq)
	if err != nil {
		log.Print("ERROR: ", err.Error())
		return err
	}
	done := make(chan bool)
	errCh := make(chan error, 1)
	go func() {
		for {
			resp, err := csiStream.Recv()
			if err == io.EOF {
				log.Print("Received EOF")
				done <- true //means stream is finished
				return
			}
			if err != nil {
				errCh <- errors.Wrap(err, "error while receiving response from CSI stream")
				return
			}
			log.Print("Received response from csi driver, proxying to client")
			if err := cbtClientStream.Send(resp); err != nil {
				errCh <- errors.Wrap(err, "error while sending response to client")
				return
			}
		}
	}()
	select {
	case <-done:
		// we will wait until all response is received
		log.Print("Successfully sent all responses to client!")
	case err := <-errCh:
		log.Printf(fmt.Sprintf("error while sending data, terminating connection, %v", err))
		return err
	}
	return nil
}

func (s *Server) initCSIGRPCClient() {
	csiAddr := os.Getenv(csiAddressKey)
	csiConn, err := connection.Connect(
		csiAddr,
		nil,
		connection.OnConnectionLoss(connection.ExitOnConnectionLoss()),
	)
	if err != nil {
		log.Printf("error connecting to CSI driver: %v", err)
		os.Exit(1)
	}
	s.client = pgrpc.NewSnapshotMetadataClient(csiConn)
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

func findSnapshotNamespace(namespacedName string) (string, error) {
	sp := strings.Split(namespacedName, "/")
	if len(sp) != 2 {
		return "", fmt.Errorf("Invalid parameter. The snapshot names should be passed in namespace/name format.")
	}
	return sp[0], nil
}

func jsonify(obj interface{}) string {
	jsonBytes, _ := json.MarshalIndent(obj, "", "  ")
	return string(jsonBytes)
}
