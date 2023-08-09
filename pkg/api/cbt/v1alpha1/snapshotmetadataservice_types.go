/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SnapshotMetadataServiceSpec defines the desired state of SnapshotMetadataService
type SnapshotMetadataServiceSpec struct {
	// The audience string value expected in an authentication token for the service
	Audience string `json:"audiences,omitempty"`
	// The TCP endpoint address of the service
	Address string `json:"address,omitempty"`
	// CABundle client side CA used for server validation
	CACert []byte `json:"caCert,omitempty"`
}

// SnapshotMetadataServiceStatus defines the observed state of SnapshotMetadataService
type SnapshotMetadataServiceStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster

// SnapshotMetadataService is the Schema for the csisnapshotsessionservices API
type SnapshotMetadataService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SnapshotMetadataServiceSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// SnapshotMetadataServiceList contains a list of SnapshotMetadataService
type SnapshotMetadataServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SnapshotMetadataService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SnapshotMetadataService{}, &SnapshotMetadataServiceList{})
}
