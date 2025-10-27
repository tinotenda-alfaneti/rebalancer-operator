package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// NodeClassSpec groups nodes by label selectors and provides hints for scoring.
type NodeClassSpec struct {
	MatchLabels      map[string]string                 `json:"matchLabels,omitempty"`
	MatchExpressions []metav1.LabelSelectorRequirement `json:"matchExpressions,omitempty"`
	PreferredFor     []string                          `json:"preferredFor,omitempty"`
	Weight           int32                             `json:"weight,omitempty"`
}

// NodeClassStatus reflects membership counts.
type NodeClassStatus struct {
	NodeCount int32        `json:"nodeCount,omitempty"`
	LastSync  *metav1.Time `json:"lastSync,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status

// NodeClass is a label-based grouping of nodes.
type NodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeClassSpec   `json:"spec,omitempty"`
	Status NodeClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NodeClassList contains a list of NodeClass.
type NodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeClass{}, &NodeClassList{})
}

// DeepCopyInto copies the receiver into out.
func (in *NodeClassSpec) DeepCopyInto(out *NodeClassSpec) {
	*out = *in
	if in.MatchLabels != nil {
		out.MatchLabels = make(map[string]string, len(in.MatchLabels))
		for k, v := range in.MatchLabels {
			out.MatchLabels[k] = v
		}
	}
	if in.MatchExpressions != nil {
		out.MatchExpressions = make([]metav1.LabelSelectorRequirement, len(in.MatchExpressions))
		for i := range in.MatchExpressions {
			in.MatchExpressions[i].DeepCopyInto(&out.MatchExpressions[i])
		}
	}
	if in.PreferredFor != nil {
		out.PreferredFor = make([]string, len(in.PreferredFor))
		copy(out.PreferredFor, in.PreferredFor)
	}
}

// DeepCopy creates a new spec copy.
func (in *NodeClassSpec) DeepCopy() *NodeClassSpec {
	if in == nil {
		return nil
	}
	out := new(NodeClassSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the status receiver into out.
func (in *NodeClassStatus) DeepCopyInto(out *NodeClassStatus) {
	*out = *in
	if in.LastSync != nil {
		out.LastSync = in.LastSync.DeepCopy()
	}
}

// DeepCopy creates a new status copy.
func (in *NodeClassStatus) DeepCopy() *NodeClassStatus {
	if in == nil {
		return nil
	}
	out := new(NodeClassStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver into out.
func (in *NodeClass) DeepCopyInto(out *NodeClass) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a new NodeClass.
func (in *NodeClass) DeepCopy() *NodeClass {
	if in == nil {
		return nil
	}
	out := new(NodeClass)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *NodeClass) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the list receiver into out.
func (in *NodeClassList) DeepCopyInto(out *NodeClassList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]NodeClass, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy creates a new list.
func (in *NodeClassList) DeepCopy() *NodeClassList {
	if in == nil {
		return nil
	}
	out := new(NodeClassList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *NodeClassList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
