package dashboard

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	rbv1 "github.com/you/rebalancer/api/v1"
	"github.com/you/rebalancer/pkg/kube"
	"github.com/you/rebalancer/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Server exposes a lightweight dashboard endpoint and static assets.
type Server struct {
	client   client.Client
	metrics  metrics.Provider
	caches   *kube.SharedCacheSet
}

// NewServer constructs a dashboard server.
func NewServer(c client.Client, metrics metrics.Provider, caches *kube.SharedCacheSet) *Server {
	return &Server{
		client:  c,
		metrics: metrics,
		caches:  caches,
	}
}

// Register hooks the handlers into the manager metrics server.
func (s *Server) Register(mgr ctrl.Manager) error {
	if err := mgr.AddMetricsExtraHandler("/dashboard/data", http.HandlerFunc(s.handleData)); err != nil {
		return err
	}

	contentFS, err := staticFS()
	if err != nil {
		return err
	}

	fileServer := http.FileServer(http.FS(contentFS))

	if err := mgr.AddMetricsExtraHandler("/dashboard/", http.StripPrefix("/dashboard", fileServer)); err != nil {
		return err
	}
	// Provide the same handler without trailing slash for convenience.
	if err := mgr.AddMetricsExtraHandler("/dashboard", fileServer); err != nil {
		return err
	}

	return nil
}

func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodesList, err := s.caches.Core.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		http.Error(w, fmt.Sprintf("list nodes: %v", err), http.StatusInternalServerError)
		return
	}

	nodeUsage, err := s.metrics.CollectNodeMetrics(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("collect node metrics: %v", err), http.StatusInternalServerError)
		return
	}

	var policyList rbv1.RebalancePolicyList
	if err := s.client.List(ctx, &policyList); err != nil {
		http.Error(w, fmt.Sprintf("list policies: %v", err), http.StatusInternalServerError)
		return
	}

	response := DashboardResponse{
		GeneratedAt: time.Now().UTC(),
	}

	var specForClassification rbv1.RebalancePolicySpec
	if len(policyList.Items) > 0 {
		specForClassification = policyList.Items[0].Spec
	}
	setDefaultPolicyValues(&specForClassification)

	// Summaries per policy.
	for i := range policyList.Items {
		policy := policyList.Items[i]
		setDefaultPolicyValues(&policy.Spec)

		response.Policies = append(response.Policies, summarisePolicy(&policy))
	}

	for i := range nodesList.Items {
		node := nodesList.Items[i]
		usage := nodeUsage[node.Name]
		response.Nodes = append(response.Nodes, summariseNode(&node, usage, specForClassification))
	}

	sort.Slice(response.Nodes, func(i, j int) bool {
		if response.Nodes[i].Classification == response.Nodes[j].Classification {
			return response.Nodes[i].CPUUtilization > response.Nodes[j].CPUUtilization
		}
		return response.Nodes[i].Classification < response.Nodes[j].Classification
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func summarisePolicy(policy *rbv1.RebalancePolicy) PolicySummary {
	spec := policy.Spec
	setDefaultPolicyValues(&spec)

	plan := policy.Status.Plan
	var lastExecution *time.Time
	if plan.LastExecutionTime != nil {
		t := plan.LastExecutionTime.Time
		lastExecution = &t
	}

	return PolicySummary{
		Name:                     policy.Name,
		TargetVariance:           spec.TargetVariance,
		HotThreshold:             spec.HotThreshold,
		ColdThreshold:            spec.ColdThreshold,
		MaxEvictionsPerMinute:    spec.MaxEvictionsPerMinute,
		MaxEvictionsPerNamespace: spec.MaxEvictionsPerNamespace,
		DryRun:                   spec.DryRun,
		Plan: PlanSummary{
			HotNodes:          plan.HotNodes,
			ColdNodes:         plan.ColdNodes,
			Variance:          plan.Variance,
			EvictionsPlanned:  plan.EvictionsPlanned,
			EvictionsExecuted: plan.EvictionsExecuted,
			LastExecutionTime: lastExecution,
			DryRun:            plan.DryRun,
		},
		NamespacesAllowed:  append([]string{}, spec.Namespaces.Include...),
		NamespacesExcluded: append([]string{}, spec.Namespaces.Exclude...),
	}
}

func summariseNode(node *corev1.Node, usage metrics.NodeUsage, spec rbv1.RebalancePolicySpec) NodeSummary {
	allocMilliCPU := node.Status.Allocatable.Cpu().MilliValue()
	allocCores := float64(allocMilliCPU) / 1000.0
	allocMemory := node.Status.Allocatable.Memory().Value()

	cpuUsage := usage.CPUCores
	memoryUsage := usage.MemoryBytes

	var cpuUtil float64
	if allocCores > 0 {
		cpuUtil = clamp(cpuUsage/allocCores, 0, 2)
	}

	var memoryUtil float64
	if allocMemory > 0 {
		memoryUtil = clamp(float64(memoryUsage)/float64(allocMemory), 0, 2)
	}

	weights := spec.ResourceWeights
	if weights.CPU == 0 && weights.Memory == 0 {
		weights.CPU = 0.7
		weights.Memory = 0.3
	}

	weighted := cpuUtil*weights.CPU + memoryUtil*weights.Memory
	classification := classify(weighted, spec.HotThreshold, spec.ColdThreshold)

	return NodeSummary{
		Name:           node.Name,
		Labels:         node.Labels,
		CPUCapacity:    allocCores,
		CPUUsage:       cpuUsage,
		CPUUtilization: cpuUtil,
		MemoryCapacity: allocMemory,
		MemoryUsage:    memoryUsage,
		MemoryUtil:     memoryUtil,
		Classification: classification,
	}
}

func classify(weighted, hot, cold float64) string {
	switch {
	case weighted >= hot:
		return "hot"
	case weighted <= cold:
		return "cold"
	default:
		return "balanced"
	}
}

func clamp(val, min, max float64) float64 {
	return math.Max(min, math.Min(max, val))
}

func setDefaultPolicyValues(spec *rbv1.RebalancePolicySpec) {
	if spec.TargetVariance == 0 {
		spec.TargetVariance = 0.15
	}
	if spec.HotThreshold == 0 {
		spec.HotThreshold = 0.8
	}
	if spec.ColdThreshold == 0 {
		spec.ColdThreshold = 0.5
	}
	if spec.ResourceWeights.CPU == 0 && spec.ResourceWeights.Memory == 0 {
		spec.ResourceWeights.CPU = 0.7
		spec.ResourceWeights.Memory = 0.3
	}
}
