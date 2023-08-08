package authz

import (
	"context"
	"log"

	authv1 "k8s.io/api/authentication/v1"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	volumeSnapshotGVR        = metav1.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}
	volumeSnapshotAccessVerb = "get"
)

type SARAuthorizer struct {
	kubeCli kubernetes.Interface
}

func NewSARAuthorizer(cli kubernetes.Interface) *SARAuthorizer {
	return &SARAuthorizer{kubeCli: cli}
}

func (s *SARAuthorizer) Authorize(ctx context.Context, namespace string, userInfo *authv1.UserInfo) (bool, string, error) {
	extra := make(map[string]authzv1.ExtraValue, len(userInfo.Extra))
	for u, e := range userInfo.Extra {
		extra[u] = authzv1.ExtraValue(e)
	}
	return s.canAccessVolumeSnapshots(ctx, namespace, userInfo, extra)
}

func (s *SARAuthorizer) canAccessVolumeSnapshots(ctx context.Context, namespace string, userInfo *authv1.UserInfo, extraValues map[string]authzv1.ExtraValue) (bool, string, error) {
	log.Println("Validating if user can access VolumeSnapshot resources")
	return s.subjectAccessReview(ctx, namespace, userInfo, extraValues, volumeSnapshotAccessVerb, volumeSnapshotGVR)
}

// Check if the user is authorized to perform given operations on the volumesnapshots and PVC resources using SubjectAccessReview API
// subjectAccessReview is a declarative API called with SubjectAccessReview resources
func (s *SARAuthorizer) subjectAccessReview(
	ctx context.Context,
	namespace string,
	userInfo *authv1.UserInfo,
	extraValues map[string]authzv1.ExtraValue,
	verb string,
	gvr metav1.GroupVersionResource,
) (bool, string, error) {
	sar := &authzv1.SubjectAccessReview{
		Spec: authzv1.SubjectAccessReviewSpec{
			ResourceAttributes: &authzv1.ResourceAttributes{
				Verb:      verb,
				Namespace: namespace,
				Group:     gvr.Group,
				Version:   gvr.Version,
				Resource:  gvr.Resource,
			},
			User:   userInfo.Username,
			Groups: userInfo.Groups,
			Extra:  extraValues,
			UID:    userInfo.UID,
		},
	}
	sarResp, err := s.kubeCli.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return false, sarResp.Status.Reason, err
	}
	if !sarResp.Status.Allowed || sarResp.Status.Denied {
		return false, sarResp.Status.Reason, nil
	}
	return true, sarResp.Status.Reason, nil
}
