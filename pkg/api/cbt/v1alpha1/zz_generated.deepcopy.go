//go:build !ignore_autogenerated
// +build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SnapshotMetadataService) DeepCopyInto(out *SnapshotMetadataService) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SnapshotMetadataService.
func (in *SnapshotMetadataService) DeepCopy() *SnapshotMetadataService {
	if in == nil {
		return nil
	}
	out := new(SnapshotMetadataService)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SnapshotMetadataService) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SnapshotMetadataServiceList) DeepCopyInto(out *SnapshotMetadataServiceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]SnapshotMetadataService, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SnapshotMetadataServiceList.
func (in *SnapshotMetadataServiceList) DeepCopy() *SnapshotMetadataServiceList {
	if in == nil {
		return nil
	}
	out := new(SnapshotMetadataServiceList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SnapshotMetadataServiceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SnapshotMetadataServiceSpec) DeepCopyInto(out *SnapshotMetadataServiceSpec) {
	*out = *in
	if in.CACert != nil {
		in, out := &in.CACert, &out.CACert
		*out = make([]byte, len(*in))
		copy(*out, *in)
	}
	if in.Audiences != nil {
		in, out := &in.Audiences, &out.Audiences
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SnapshotMetadataServiceSpec.
func (in *SnapshotMetadataServiceSpec) DeepCopy() *SnapshotMetadataServiceSpec {
	if in == nil {
		return nil
	}
	out := new(SnapshotMetadataServiceSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SnapshotMetadataServiceStatus) DeepCopyInto(out *SnapshotMetadataServiceStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SnapshotMetadataServiceStatus.
func (in *SnapshotMetadataServiceStatus) DeepCopy() *SnapshotMetadataServiceStatus {
	if in == nil {
		return nil
	}
	out := new(SnapshotMetadataServiceStatus)
	in.DeepCopyInto(out)
	return out
}
