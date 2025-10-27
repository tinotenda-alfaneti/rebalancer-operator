package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// WorkloadSLOSpec holds disruption and readiness guardrails for a namespace.
type WorkloadSLOSpec struct {
	MaxDisruptionsPerHour    int32             `json:"maxDisruptionsPerHour,omitempty"`
	MinReadySeconds          int32             `json:"minReadySeconds,omitempty"`
	AllowStatefulSetEviction bool              `json:"allowStatefulSetEviction,omitempty"`
	ProtectedLabels          map[string]string `json:"protectedLabels,omitempty"`
}

// WorkloadSLOStatus reports recent disruptive actions.
type WorkloadSLOStatus struct {
	DisruptionsInWindow int32        `json:"disruptionsInWindow,omitempty"`
	WindowStart         *metav1.Time `json:"windowStart,omitempty"`
	LastViolation       *metav1.Time `json:"lastViolation,omitempty"`
	Message             string       `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status

// WorkloadSLO configures namespace-specific guardrails.
type WorkloadSLO struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkloadSLOSpec   `json:"spec,omitempty"`
	Status WorkloadSLOStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkloadSLOList contains a list of WorkloadSLO objects.
type WorkloadSLOList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkloadSLO `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkloadSLO{}, &WorkloadSLOList{})
}

// DeepCopyInto copies the receiver into out.
func (in *WorkloadSLOSpec) DeepCopyInto(out *WorkloadSLOSpec) {
	*out = *in
	if in.ProtectedLabels != nil {
		out.ProtectedLabels = make(map[string]string, len(in.ProtectedLabels))
		for k, v := range in.ProtectedLabels {
			out.ProtectedLabels[k] = v
		}
	}
}

// DeepCopy creates a new instance.
func (in *WorkloadSLOSpec) DeepCopy() *WorkloadSLOSpec {
	if in == nil {
		return nil
	}
	out := new(WorkloadSLOSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the status receiver into out.
func (in *WorkloadSLOStatus) DeepCopyInto(out *WorkloadSLOStatus) {
	*out = *in
	if in.WindowStart != nil {
		out.WindowStart = in.WindowStart.DeepCopy()
	}
	if in.LastViolation != nil {
		out.LastViolation = in.LastViolation.DeepCopy()
	}
}

// DeepCopy creates a new status copy.
func (in *WorkloadSLOStatus) DeepCopy() *WorkloadSLOStatus {
	if in == nil {
		return nil
	}
	out := new(WorkloadSLOStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver into out.
func (in *WorkloadSLO) DeepCopyInto(out *WorkloadSLO) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a new WorkloadSLO.
func (in *WorkloadSLO) DeepCopy() *WorkloadSLO {
	if in == nil {
		return nil
	}
	out := new(WorkloadSLO)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *WorkloadSLO) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the list receiver.
func (in *WorkloadSLOList) DeepCopyInto(out *WorkloadSLOList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]WorkloadSLO, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy creates a new list.
func (in *WorkloadSLOList) DeepCopy() *WorkloadSLOList {
	if in == nil {
		return nil
	}
	out := new(WorkloadSLOList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *WorkloadSLOList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
