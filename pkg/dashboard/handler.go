package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	rbv1 "github.com/you/rebalancer/api/v1"
	"github.com/you/rebalancer/pkg/kube"
	"github.com/you/rebalancer/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Handler serves both static assets and JSON data for the dashboard.
type Handler struct {
	mu      sync.RWMutex
	client  client.Client
	metrics metrics.Provider
	caches  *kube.SharedCacheSet

	static http.Handler
}

// NewHandler constructs an empty dashboard handler. Call Attach to provide dependencies.
func NewHandler() (*Handler, error) {
	fs, err := staticFS()
	if err != nil {
		return nil, err
	}

	return &Handler{
		static: http.FileServer(http.FS(fs)),
	}, nil
}

// Attach provides dependencies once they are available.
func (h *Handler) Attach(client client.Client, metrics metrics.Provider, caches *kube.SharedCacheSet) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.client = client
	h.metrics = metrics
	h.caches = caches
}

// Static serves the UI. If dependencies are missing it still serves HTML.
func (h *Handler) Static(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/", "/dashboard", "/dashboard/":
		content, err := static.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "dashboard index missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(content)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/dashboard/") {
		cloned := r.Clone(r.Context())
		cloned.URL.Path = strings.TrimPrefix(r.URL.Path, "/dashboard")
		if cloned.URL.Path == "" || cloned.URL.Path == "/" {
			cloned.URL.Path = "/index.html"
		}
		h.static.ServeHTTP(w, cloned)
		return
	}
	h.static.ServeHTTP(w, r)
}

// Data returns JSON for the dashboard. When dependencies aren't ready yet it returns 503.
func (h *Handler) Data(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	h.mu.RLock()
	client := h.client
	metricsProv := h.metrics
	caches := h.caches
	h.mu.RUnlock()

	if client == nil || metricsProv == nil || caches == nil {
		http.Error(w, "dashboard not initialised", http.StatusServiceUnavailable)
		return
	}

	nodesList, err := caches.Core.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeDashboardError(w, "list nodes", err)
		return
	}

	nodeUsage, err := metricsProv.CollectNodeMetrics(ctx)
	if err != nil {
		writeDashboardError(w, "collect node metrics", err)
		return
	}

	var policyList rbv1.RebalancePolicyList
	if err := client.List(ctx, &policyList); err != nil {
		writeDashboardError(w, "list policies", err)
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

	for i := range policyList.Items {
		policy := policyList.Items[i]
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
		writeDashboardError(w, "encode response", err)
	}
}

func writeDashboardError(w http.ResponseWriter, context string, err error) {
	http.Error(w, fmt.Sprintf("%s: %v", context, err), http.StatusInternalServerError)
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

// Ready reports whether the handler has all required dependencies.
func (h *Handler) Ready() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.client == nil || h.metrics == nil || h.caches == nil {
		return errors.New("dashboard handler not initialised")
	}
	return nil
}
