package kube

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	policyclient "k8s.io/client-go/kubernetes/typed/policy/v1"
	schedulingclient "k8s.io/client-go/kubernetes/typed/scheduling/v1"
	"k8s.io/client-go/rest"
)

// SharedCacheSet wraps typed clients used by the planner.
type SharedCacheSet struct {
	Core       kubernetes.Interface
	Policy     policyclient.PolicyV1Interface
	Scheduling schedulingclient.SchedulingV1Interface
}

// NewSharedCacheSet creates helper clients backed by the shared REST config.
func NewSharedCacheSet(cfg *rest.Config) (*SharedCacheSet, error) {
	coreClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes clientset: %w", err)
	}

	return &SharedCacheSet{
		Core:       coreClient,
		Policy:     coreClient.PolicyV1(),
		Scheduling: coreClient.SchedulingV1(),
	}, nil
}
