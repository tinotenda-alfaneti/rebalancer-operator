package controllers

import (
	"context"
	"time"

	rbv1 "github.com/you/rebalancer/api/v1"
	"github.com/you/rebalancer/controllers/executors"
	"github.com/you/rebalancer/controllers/planner"
	"github.com/you/rebalancer/pkg/observability"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PolicyReconciler reconciles RebalancePolicy resources and drives the control loop.
type PolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Planner  *planner.Planner
	Evictor  *executors.Evictor
	Recorder record.EventRecorder
	Metrics  *observability.Recorder
}

// Reconcile runs the rebalancing loop on the configured cadence.
func (r *PolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var policy rbv1.RebalancePolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	snapshot, err := r.Planner.Snapshot(ctx, &policy)
	if err != nil {
		logger.Error(err, "failed to build cluster snapshot")
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	plan := r.Planner.Plan(snapshot, &policy)

	r.Metrics.RecordSnapshot(snapshot)
	r.Metrics.ObservePlan(plan)

	logger.Info("computed rebalancing plan",
		"policy", req.NamespacedName,
		"evictions", len(plan.Evictions),
		"dryRun", policy.Spec.DryRun,
		"nextCheck", plan.RequeueAfter)
	for ns, remaining := range plan.Budgets {
		r.Metrics.RecordBudget(ns, remaining)
	}

	if policy.Spec.DryRun {
		for _, ev := range plan.Evictions {
			r.Recorder.Eventf(&policy, corev1.EventTypeNormal, "DryRunEviction",
				"Would evict pod %s/%s from node %s to reduce variance (reason=%s)",
				ev.Pod.Namespace, ev.Pod.Name, ev.FromNode, ev.Reason)
			r.Metrics.RecordEviction(ev, false)
		}
		return r.updateStatus(ctx, &policy, plan, 0, plan.RequeueAfter)
	}

	var executed int
	for _, ev := range plan.Evictions {
		if !ev.Allowed {
			continue
		}
		if err := r.Evictor.Evict(ctx, ev); err != nil {
			logger.Error(err, "eviction failed", "pod", types.NamespacedName{Namespace: ev.Pod.Namespace, Name: ev.Pod.Name})
			if errors.IsTooManyRequests(err) {
				return ctrl.Result{RequeueAfter: plan.RequeueAfter}, nil
			}
			r.Recorder.Eventf(&policy, corev1.EventTypeWarning, "EvictionFailed",
				"Failed evicting pod %s/%s: %v", ev.Pod.Namespace, ev.Pod.Name, err)
			continue
		}
		executed++
		r.Planner.RecordEviction(types.NamespacedName{Namespace: ev.Pod.Namespace, Name: ev.Pod.Name})
		r.Recorder.Eventf(&policy, corev1.EventTypeNormal, "Evicted",
			"Evicted pod %s/%s from node %s (reason=%s)",
			ev.Pod.Namespace, ev.Pod.Name, ev.FromNode, ev.Reason)
		r.Metrics.RecordEviction(ev, true)
	}

	logger.Info("evictions completed", "count", executed)
	return r.updateStatus(ctx, &policy, plan, executed, plan.RequeueAfter)
}

func (r *PolicyReconciler) updateStatus(ctx context.Context, policy *rbv1.RebalancePolicy, plan planner.Plan, executed int, requeue time.Duration) (ctrl.Result, error) {
	original := policy.DeepCopy()
	now := metav1.NewTime(time.Now())
	policy.Status.ObservedGeneration = policy.Generation
	policy.Status.Plan = rbv1.PlanStatus{
		HotNodes:          plan.Stats.HotNodes,
		ColdNodes:         plan.Stats.ColdNodes,
		Variance:          plan.Stats.Variance,
		EvictionsPlanned:  len(plan.Evictions),
		EvictionsExecuted: executed,
		LastExecutionTime: &now,
		DryRun:            policy.Spec.DryRun,
	}
	if err := r.Status().Patch(ctx, policy, client.MergeFrom(original)); err != nil {
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// SetupWithManager wires the controller to the manager.
func (r *PolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rbv1.RebalancePolicy{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
