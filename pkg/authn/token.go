package authn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type TokenAuthenticator struct {
	kubeCli kubernetes.Interface
}

func NewTokenAuthenticator(cli kubernetes.Interface) *TokenAuthenticator {
	return &TokenAuthenticator{kubeCli: cli}
}

func (t *TokenAuthenticator) Authenticate(ctx context.Context, token string, audiences []string) (*authv1.UserInfo, error) {
	// https://pkg.go.dev/k8s.io/api/authentication/v1#TokenReview
	tokenReview := authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token:     token,
			Audiences: audiences,
		},
	}
	auth, err := t.kubeCli.AuthenticationV1().TokenReviews().Create(ctx, &tokenReview, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	if !auth.Status.Authenticated {
		return nil, fmt.Errorf("Forbidden. SA Token authentication failed")
	}
	if !reflect.DeepEqual(audiences, auth.Status.Audiences) {
		return nil, fmt.Errorf("Forbidden. SA Token authentication failed due to invalid audiences")
	}

	// Debug: log token review resp
	trJSON, _ := json.MarshalIndent(auth, "", "  ")
	log.Printf("TokenReview Response:: %s\n", trJSON)

	return &auth.Status.User, nil
}
