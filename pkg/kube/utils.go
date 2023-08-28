package kube

import (
	"context"
	"fmt"
	"log"
	"strings"

	volsnapv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cbtv1alpha1 "github.com/PrasadG193/external-snapshot-metadata/pkg/api/cbt/v1alpha1"
)

const driverAnnotationKey = "cbt.storage.k8s.io/driver"

func FindSnapshotMetadataService(ctx context.Context, cli client.Client, driver string) (*cbtv1alpha1.SnapshotMetadataService, error) {
	log.Printf("Search SnapshotMetadataService object for driver: %s", driver)
	sssList := &cbtv1alpha1.SnapshotMetadataServiceList{}
	sssReq, err := labels.NewRequirement(driverAnnotationKey, selection.Equals, []string{driver})
	if err != nil {
		return nil, err
	}
	err1 := cli.List(ctx, sssList, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*sssReq)})
	if err1 != nil {
		return nil, err1
	}

	if len(sssList.Items) == 0 {
		return nil, nil
	}
	log.Printf("Found SnapshotMetadataService object %s for driver: %s", sssList.Items[0].GetName(), driver)
	return &sssList.Items[0], nil
}

func GetVolSnapshotInfo(ctx context.Context, cli client.Client, namespacedName string) (string, string, error) {
	volSnap := &volsnapv1.VolumeSnapshot{}
	vsName := strings.Split(namespacedName, "/")
	if len(vsName) != 2 {
		return "", "", fmt.Errorf("Invalid parameter. The snapshot names should be passed in namespace/name format.")
	}
	err := cli.Get(ctx, types.NamespacedName{Name: vsName[1], Namespace: vsName[0]}, volSnap)
	if err != nil {
		return "", "", err
	}
	if volSnap.Status.ReadyToUse == nil || !*volSnap.Status.ReadyToUse {
		return "", "", fmt.Errorf("Snapshot snapshot is not ready, name: %s", namespacedName)
	}
	vsc := &volsnapv1.VolumeSnapshotContent{}
	err1 := cli.Get(ctx, types.NamespacedName{Name: *volSnap.Status.BoundVolumeSnapshotContentName, Namespace: vsName[0]}, vsc)
	if err1 != nil {
		return "", "", err1
	}
	return *vsc.Status.SnapshotHandle, vsc.Spec.Driver, nil
}
