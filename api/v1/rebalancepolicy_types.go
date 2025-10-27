package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// NamespaceSelector constrains which namespaces may have workloads rebalanced.
type NamespaceSelector struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// ResourceWeights configures the weighting for CPU and memory when calculating node pressure.
type ResourceWeights struct {
	CPU    float64 `json:"cpu,omitempty"`
	Memory float64 `json:"memory,omitempty"`
}

// RebalancePolicySpec defines the desired rebalancing behaviour.
type RebalancePolicySpec struct {
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +kubebuilder:default=0.15
	TargetVariance float64 `json:"targetVariance,omitempty"`

	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +kubebuilder:default=0.8
	HotThreshold float64 `json:"hotThreshold,omitempty"`

	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +kubebuilder:default=0.5
	ColdThreshold float64 `json:"coldThreshold,omitempty"`

	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=0.2
	// +kubebuilder:default=0.05
	Hysteresis float64 `json:"hysteresis,omitempty"`

	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3
	MaxEvictionsPerMinute int `json:"maxEvictionsPerMinute,omitempty"`

	Namespaces NamespaceSelector `json:"namespaces,omitempty"`

	WorkloadSelectors []metav1.LabelSelector `json:"workloadSelectors,omitempty"`

	// +kubebuilder:default={"cpu":0.7,"memory":0.3}
	ResourceWeights ResourceWeights `json:"resourceWeights,omitempty"`

	// +kubebuilder:default=true
	RespectPodPriority bool `json:"respectPodPriority,omitempty"`

	// +kubebuilder:default=true
	RespectPDB bool `json:"respectPDB,omitempty"`

	// +kubebuilder:default=true
	DryRun bool `json:"dryRun,omitempty"`

	// +kubebuilder:default="15m"
	BackoffDuration *metav1.Duration `json:"backoffDuration,omitempty"`

	// +kubebuilder:validation:Minimum=0
	MaxEvictionsPerNamespace int `json:"maxEvictionsPerNamespace,omitempty"`

	NodeClassPreference []string `json:"nodeClassPreference,omitempty"`
}

// PlanStatus captures snapshots of the most recent executed plan.
type PlanStatus struct {
	HotNodes          int          `json:"hotNodes,omitempty"`
	ColdNodes         int          `json:"coldNodes,omitempty"`
	Variance          float64      `json:"variance,omitempty"`
	EvictionsPlanned  int          `json:"evictionsPlanned,omitempty"`
	EvictionsExecuted int          `json:"evictionsExecuted,omitempty"`
	LastExecutionTime *metav1.Time `json:"lastExecutionTime,omitempty"`
	DryRun            bool         `json:"dryRun,omitempty"`
	Message           string       `json:"message,omitempty"`
}

// RebalancePolicyStatus represents observed state.
type RebalancePolicyStatus struct {
	ObservedGeneration int64      `json:"observedGeneration,omitempty"`
	Plan               PlanStatus `json:"plan,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// RebalancePolicy drives the core rebalancing logic.
type RebalancePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RebalancePolicySpec   `json:"spec,omitempty"`
	Status RebalancePolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RebalancePolicyList contains a list of RebalancePolicy.
type RebalancePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RebalancePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RebalancePolicy{}, &RebalancePolicyList{})
}

// DeepCopyInto copies the receiver into out.
func (in *RebalancePolicySpec) DeepCopyInto(out *RebalancePolicySpec) {
	*out = *in
	if in.WorkloadSelectors != nil {
		out.WorkloadSelectors = make([]metav1.LabelSelector, len(in.WorkloadSelectors))
		for i := range in.WorkloadSelectors {
			in.WorkloadSelectors[i].DeepCopyInto(&out.WorkloadSelectors[i])
		}
	}
	if in.BackoffDuration != nil {
		out.BackoffDuration = new(metav1.Duration)
		*out.BackoffDuration = *in.BackoffDuration
	}
	if in.NodeClassPreference != nil {
		out.NodeClassPreference = make([]string, len(in.NodeClassPreference))
		copy(out.NodeClassPreference, in.NodeClassPreference)
	}
}

// DeepCopy creates a new instance.
func (in *RebalancePolicySpec) DeepCopy() *RebalancePolicySpec {
	if in == nil {
		return nil
	}
	out := new(RebalancePolicySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver into out.
func (in *PlanStatus) DeepCopyInto(out *PlanStatus) {
	*out = *in
	if in.LastExecutionTime != nil {
		out.LastExecutionTime = in.LastExecutionTime.DeepCopy()
	}
}

// DeepCopy creates a new PlanStatus.
func (in *PlanStatus) DeepCopy() *PlanStatus {
	if in == nil {
		return nil
	}
	out := new(PlanStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver into out.
func (in *RebalancePolicyStatus) DeepCopyInto(out *RebalancePolicyStatus) {
	*out = *in
	in.Plan.DeepCopyInto(&out.Plan)
}

// DeepCopy creates a new RebalancePolicyStatus.
func (in *RebalancePolicyStatus) DeepCopy() *RebalancePolicyStatus {
	if in == nil {
		return nil
	}
	out := new(RebalancePolicyStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver into out.
func (in *RebalancePolicy) DeepCopyInto(out *RebalancePolicy) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a new RebalancePolicy.
func (in *RebalancePolicy) DeepCopy() *RebalancePolicy {
	if in == nil {
		return nil
	}
	out := new(RebalancePolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object interface.
func (in *RebalancePolicy) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the list receiver into out.
func (in *RebalancePolicyList) DeepCopyInto(out *RebalancePolicyList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]RebalancePolicy, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy creates a new list.
func (in *RebalancePolicyList) DeepCopy() *RebalancePolicyList {
	if in == nil {
		return nil
	}
	out := new(RebalancePolicyList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *RebalancePolicyList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
