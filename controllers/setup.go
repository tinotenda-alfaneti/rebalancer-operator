package controllers

import (
	"github.com/you/rebalancer/controllers/executors"
	"github.com/you/rebalancer/controllers/planner"
	"github.com/you/rebalancer/pkg/observability"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SetupWithManager wires controllers to the manager.
func SetupWithManager(mgr ctrl.Manager, plan *planner.Planner, evictor *executors.Evictor) error {
	recorder := mgr.GetEventRecorderFor("rebalancer-operator")
	metrics := observability.NewRecorder()

	reconciler := &PolicyReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Planner:  plan,
		Evictor:  evictor,
		Recorder: recorder,
		Metrics:  metrics,
	}

	return reconciler.SetupWithManager(mgr)
}
