/*
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

package v1alpha5

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
)

type NodeGroupSpec struct {
	// Constraints are applied to all nodes.
	Constraints `json:",inline"`
	Replicas    int32 `json:"replicas"`
}

type NodeGroupStatus struct {
}

// NodeGroup is the Schema for the NodeGroups API
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=nodegroups,scope=Cluster,categories=karpenter
// +kubebuilder:subresource:status
type NodeGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeGroupSpec   `json:"spec,omitempty"`
	Status NodeGroupStatus `json:"status,omitempty"`
}

// NodeGroupList contains a list of NodeGroup
// +kubebuilder:object:root=true
type NodeGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeGroup `json:"items"`
}

func (n *NodeGroup) Validate(ctx context.Context) (errs *apis.FieldError) {
	return errs.Also(
		apis.ValidateObjectMetadata(n).ViaField("metadata"),
		n.Spec.Validate(ctx).ViaField("spec"),
	)
}

func (n *NodeGroupSpec) Validate(ctx context.Context) (errs *apis.FieldError) {
	return n.Constraints.Validate(ctx)
}

func (n *NodeGroup) SetDefaults(ctx context.Context) {
}
