package executors

import (
	"context"
	"fmt"

	"github.com/you/rebalancer/controllers/planner"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Evictor performs graceful evictions through the Kubernetes eviction API.
type Evictor struct {
	client client.Client
}

// NewEvictor constructs an eviction executor.
func NewEvictor(c client.Client) *Evictor {
	return &Evictor{client: c}
}

// Evict triggers eviction for the provided plan entry.
func (e *Evictor) Evict(ctx context.Context, ev planner.PlanEviction) error {
	if ev.Pod.Name == "" {
		return fmt.Errorf("eviction missing pod name")
	}
	grace := int64(30)
	return e.client.SubResource("eviction").Create(ctx, &ev.Pod, &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ev.Pod.Name,
			Namespace: ev.Pod.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(grace)},
	})
}
