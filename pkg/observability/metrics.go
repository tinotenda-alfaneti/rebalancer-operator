package observability

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/you/rebalancer/controllers/planner"
)

// Recorder exposes high-level Prometheus metrics for the operator.
type Recorder struct {
	initOnce        sync.Once
	nodeUtilization *prometheus.GaugeVec
	sigma           prometheus.Gauge
	evictionsTotal  *prometheus.CounterVec
	dryRunEvictions prometheus.Counter
	budgetRemaining *prometheus.GaugeVec
}

// NewRecorder constructs a metrics recorder and registers Prometheus collectors.
func NewRecorder() *Recorder {
	r := &Recorder{}
	r.initOnce.Do(func() {
		r.nodeUtilization = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rebalancer_node_utilization",
			Help: "Latest per-node utilization scores computed by the planner",
		}, []string{"node", "resource"})
		r.sigma = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "rebalancer_sigma",
			Help: "Cluster utilization variance",
		})
		r.evictionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "rebalancer_evictions_total",
			Help: "Number of pod evictions initiated by the operator",
		}, []string{"namespace", "reason"})
		r.dryRunEvictions = promauto.NewCounter(prometheus.CounterOpts{
			Name: "rebalancer_dry_run_evictions_total",
			Help: "Number of pods that would have been evicted when running in dry-run mode",
		})
		r.budgetRemaining = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "rebalancer_budget_remaining",
			Help: "Remaining namespace disruption budget units",
		}, []string{"namespace"})
	})
	return r
}

// ObservePlan records aggregate planner output for metrics exposure.
func (r *Recorder) ObservePlan(plan planner.Plan) {
	r.sigma.Set(plan.Stats.Variance)
}

// RecordSnapshot publishes per-node utilization gauges.
func (r *Recorder) RecordSnapshot(snapshot *planner.Snapshot) {
	for name, node := range snapshot.Nodes {
		r.RecordNodeUtil(name, node.Utilization.CPU, node.Utilization.Memory)
	}
}

// RecordNodeUtil updates the node utilization gauges for reporting.
func (r *Recorder) RecordNodeUtil(node string, cpu, memory float64) {
	r.nodeUtilization.WithLabelValues(node, "cpu").Set(cpu)
	r.nodeUtilization.WithLabelValues(node, "memory").Set(memory)
}

// RecordEviction increments eviction counters. When executed is false, the event route is considered budgeted but not performed.
func (r *Recorder) RecordEviction(ev planner.PlanEviction, executed bool) {
	if executed {
		r.evictionsTotal.WithLabelValues(ev.Namespace, ev.Reason).Inc()
	} else {
		r.dryRunEvictions.Inc()
	}
}

// RecordBudget updates remaining disruption budget for a namespace.
func (r *Recorder) RecordBudget(namespace string, remaining int) {
	r.budgetRemaining.WithLabelValues(namespace).Set(float64(remaining))
}
