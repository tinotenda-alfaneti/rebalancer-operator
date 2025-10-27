package planner

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	rbv1 "github.com/you/rebalancer/api/v1"
	"github.com/you/rebalancer/pkg/kube"
	"github.com/you/rebalancer/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeTemperature indicates a node's pressure level.
type NodeTemperature string

const (
	NodeHot  NodeTemperature = "hot"
	NodeCold NodeTemperature = "cold"
	NodeOK   NodeTemperature = "ok"

	defaultBackoff             = 15 * time.Minute
	DefaultEmptyDirThresholdGi = 2
	systemCriticalPriority     = int32(2000000000)
)

// Options configures the planner.
type Options struct {
	Client          client.Client
	MetricsProvider metrics.Provider
	CacheSet        *kube.SharedCacheSet
	Clock           clock.Clock
	EmptyDirLimitGi int64
}

// Planner evaluates cluster state and produces rebalancing decisions.
type Planner struct {
	client    client.Client
	caches    *kube.SharedCacheSet
	metrics   metrics.Provider
	clock     clock.Clock
	mu        sync.Mutex
	history   map[types.NamespacedName]time.Time
	nsHistory map[string][]time.Time
	options   Options
}

// New creates a Planner instance.
func New(opts Options) *Planner {
	if opts.Clock == nil {
		opts.Clock = clock.RealClock{}
	}
	if opts.EmptyDirLimitGi == 0 {
		opts.EmptyDirLimitGi = DefaultEmptyDirThresholdGi
	}
	return &Planner{
		client:    opts.Client,
		caches:    opts.CacheSet,
		metrics:   opts.MetricsProvider,
		clock:     opts.Clock,
		history:   make(map[types.NamespacedName]time.Time),
		nsHistory: make(map[string][]time.Time),
		options:   opts,
	}
}

// Snapshot holds the cluster state required for scoring moves.
type Snapshot struct {
	Nodes           map[string]*NodeState
	Pods            []*PodState
	PDBs            []*policyv1.PodDisruptionBudget
	WorkloadSLOs    map[string]*rbv1.WorkloadSLO
	NodeClasses     map[string]*rbv1.NodeClass
	PriorityClasses map[string]*schedulingv1.PriorityClass
}

// NodeState captures utilization and metadata for a node.
type NodeState struct {
	Object         *corev1.Node
	Usage          metrics.NodeUsage
	Utilization    Utilization
	Classification NodeTemperature
	LastUpdated    time.Time
	Classes        []string
}

// Utilization aggregates resource pressure.
type Utilization struct {
	CPU      float64
	Memory   float64
	Weighted float64
}

// PodState represents a pod considered for scheduling decisions.
type PodState struct {
	Object           *corev1.Pod
	Priority         int32
	Requests         corev1.ResourceList
	Limits           corev1.ResourceList
	ControllerKind   string
	ControllerName   string
	OwnerUID         types.UID
	NodeName         string
	CustomMetrics    map[string]float64
	LastEvicted      *time.Time
	NamespaceSLO     *rbv1.WorkloadSLO
	MatchesPolicy    bool
	BackoffRemaining time.Duration
}

// Plan describes the actions to take in this reconciliation.
type Plan struct {
	Evictions    []PlanEviction
	RequeueAfter time.Duration
	Stats        PlanStats
	Budgets      map[string]int
}

// PlanStats summarises the rebalance computation.
type PlanStats struct {
	HotNodes       int
	ColdNodes      int
	Variance       float64
	TargetVariance float64
}

// PlanEviction is a pod that should be evicted.
type PlanEviction struct {
	Pod              corev1.Pod
	FromNode         string
	TargetCandidates []string
	Score            float64
	Reason           string
	Allowed          bool
	Namespace        string
}

// Snapshot retrieves Kubernetes objects and metrics required for planning.
func (p *Planner) Snapshot(ctx context.Context, policy *rbv1.RebalancePolicy) (*Snapshot, error) {
	nodeMetrics, err := p.metrics.CollectNodeMetrics(ctx)
	if err != nil {
		return nil, err
	}
	podMetrics, err := p.metrics.CollectPodMetrics(ctx)
	if err != nil {
		return nil, err
	}

	nodesList, err := p.caches.Core.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	podsList, err := p.caches.Core.CoreV1().Pods(corev1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	pdbList, err := p.caches.Policy.PodDisruptionBudgets(corev1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pdbs: %w", err)
	}
	priorityClasses, err := p.caches.Scheduling.PriorityClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list priority classes: %w", err)
	}

	var sloList rbv1.WorkloadSLOList
	if err := p.client.List(ctx, &sloList); err != nil {
		return nil, fmt.Errorf("list workload SLOs: %w", err)
	}
	var nodeClassList rbv1.NodeClassList
	if err := p.client.List(ctx, &nodeClassList); err != nil {
		return nil, fmt.Errorf("list node classes: %w", err)
	}

	nodeClasses := make(map[string]*rbv1.NodeClass, len(nodeClassList.Items))
	for i := range nodeClassList.Items {
		nodeClass := nodeClassList.Items[i].DeepCopy()
		nodeClasses[nodeClass.Name] = nodeClass
	}

	nodes := make(map[string]*NodeState, len(nodesList.Items))
	for i := range nodesList.Items {
		node := nodesList.Items[i]
		usage := nodeMetrics[node.Name]
		state := &NodeState{
			Object:      node.DeepCopy(),
			Usage:       usage,
			Utilization: Utilization{},
			LastUpdated: usage.Timestamp,
		}
		nodes[node.Name] = state
	}
	pdbs := make([]*policyv1.PodDisruptionBudget, 0, len(pdbList.Items))
	for i := range pdbList.Items {
		pdb := pdbList.Items[i]
		pdbs = append(pdbs, pdb.DeepCopy())
	}

	workloadSLOs := make(map[string]*rbv1.WorkloadSLO)
	for i := range sloList.Items {
		slo := sloList.Items[i].DeepCopy()
		workloadSLOs[slo.Namespace] = slo
	}

	priorityMap := make(map[string]*schedulingv1.PriorityClass)
	for i := range priorityClasses.Items {
		pc := priorityClasses.Items[i]
		priorityMap[pc.Name] = pc.DeepCopy()
	}

	var pods []*PodState
	for i := range podsList.Items {
		pod := podsList.Items[i]
		metricsKey := types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}
		usage := podMetrics[metricsKey]
		slo := workloadSLOs[pod.Namespace]
		podState := buildPodState(pod.DeepCopy(), usage, slo, p.lastEvicted(metricsKey))
		pods = append(pods, podState)
	}

	for _, node := range nodes {
		for name, class := range nodeClasses {
			if nodeMatchesClass(node.Object, class) {
				node.Classes = append(node.Classes, name)
			}
		}
	}

	return &Snapshot{
		Nodes:           nodes,
		Pods:            pods,
		PDBs:            pdbs,
		WorkloadSLOs:    workloadSLOs,
		NodeClasses:     nodeClasses,
		PriorityClasses: priorityMap,
	}, nil
}

// Plan computes a set of eviction actions attempting to reduce variance.
func (p *Planner) Plan(snapshot *Snapshot, policy *rbv1.RebalancePolicy) Plan {
	stats := PlanStats{}
	result := Plan{
		Evictions:    []PlanEviction{},
		RequeueAfter: 30 * time.Second,
		Stats:        stats,
	}

	weights := policy.Spec.ResourceWeights
	if weights.CPU == 0 && weights.Memory == 0 {
		weights.CPU = 0.7
		weights.Memory = 0.3
	}

	hotNodes, coldNodes, utilMap := classifyNodes(snapshot.Nodes, weights, policy)
	stats.HotNodes = len(hotNodes)
	stats.ColdNodes = len(coldNodes)
	stats.Variance = computeVariance(utilMap)
	stats.TargetVariance = policy.Spec.TargetVariance
	result.Stats = stats

	if len(hotNodes) == 0 || len(coldNodes) == 0 {
		return result
	}

	candidates := p.filterCandidates(snapshot, policy, hotNodes)
	if len(candidates) == 0 {
		return result
	}

	plan := p.greedyPlan(snapshot, candidates, policy, hotNodes, coldNodes, utilMap)
	if len(plan) == 0 {
		return result
	}

	limit := policy.Spec.MaxEvictionsPerMinute
	if limit <= 0 {
		limit = 1
	}

	namespaceCounts := make(map[string]int)
	result.Budgets = make(map[string]int)
	clusterRemaining := limit
	for _, ev := range plan {
		slo := snapshot.WorkloadSLOs[ev.Namespace]
		allowed := false
		if clusterRemaining > 0 && p.canDisruptNamespace(ev.Namespace, policy, slo, namespaceCounts[ev.Namespace]) {
			allowed = true
		}
		if allowed {
			ev.Allowed = true
			namespaceCounts[ev.Namespace]++
			clusterRemaining--
		} else if !allowed {
			ev.Reason += " (budget exhausted)"
		}
		result.Evictions = append(result.Evictions, ev)
	}
	for ns, count := range namespaceCounts {
		slo := snapshot.WorkloadSLOs[ns]
		remaining := p.remainingNamespaceBudget(ns, policy, slo)
		if remaining < 0 {
			result.Budgets[ns] = -1
			continue
		}
		if remaining >= 0 {
			remaining -= count
			if remaining < 0 {
				remaining = 0
			}
			result.Budgets[ns] = remaining
		}
	}

	return result
}

func classifyNodes(nodes map[string]*NodeState, weights rbv1.ResourceWeights, policy *rbv1.RebalancePolicy) ([]*NodeState, []*NodeState, map[string]float64) {
	hot := []*NodeState{}
	cold := []*NodeState{}
	utilization := make(map[string]float64, len(nodes))

	for _, node := range nodes {
		allocCPU := node.Object.Status.Allocatable.Cpu().MilliValue()
		allocMem := node.Object.Status.Allocatable.Memory().Value()
		cpuUtil := 0.0
		memUtil := 0.0
		if allocCPU > 0 {
			cpuUtil = (node.Usage.CPUCores * 1000) / float64(allocCPU)
		}
		if allocMem > 0 {
			memUtil = float64(node.Usage.MemoryBytes) / float64(allocMem)
		}
		node.Utilization = Utilization{
			CPU:      cpuUtil,
			Memory:   memUtil,
			Weighted: cpuUtil*weights.CPU + memUtil*weights.Memory,
		}

		utilization[node.Object.Name] = node.Utilization.Weighted
		switch {
		case node.Utilization.Weighted >= policy.Spec.HotThreshold-policy.Spec.Hysteresis:
			node.Classification = NodeHot
			hot = append(hot, node)
		case node.Utilization.Weighted <= policy.Spec.ColdThreshold+policy.Spec.Hysteresis:
			node.Classification = NodeCold
			cold = append(cold, node)
		default:
			node.Classification = NodeOK
		}
	}

	sort.Slice(hot, func(i, j int) bool {
		return hot[i].Utilization.Weighted > hot[j].Utilization.Weighted
	})
	sort.Slice(cold, func(i, j int) bool {
		return cold[i].Utilization.Weighted < cold[j].Utilization.Weighted
	})

	return hot, cold, utilization
}

func computeVariance(util map[string]float64) float64 {
	if len(util) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range util {
		sum += v
	}
	mean := sum / float64(len(util))
	variance := 0.0
	for _, v := range util {
		diff := v - mean
		variance += diff * diff
	}
	return variance / float64(len(util))
}

func buildPodState(pod *corev1.Pod, usage metrics.PodUsage, slo *rbv1.WorkloadSLO, lastEviction *time.Time) *PodState {
	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		requests = addResourceLists(requests, container.Resources.Requests)
		limits = addResourceLists(limits, container.Resources.Limits)
	}

	priority := pod.Spec.Priority
	if priority == nil {
		defaultPriority := int32(0)
		priority = &defaultPriority
	}

	controllerKind, controllerName, ownerUID := controllerRef(pod.OwnerReferences)

	var sloCopy *rbv1.WorkloadSLO
	if slo != nil {
		sloCopy = slo.DeepCopy()
	}

	return &PodState{
		Object:         pod,
		Priority:       *priority,
		Requests:       requests,
		Limits:         limits,
		NodeName:       pod.Spec.NodeName,
		ControllerKind: controllerKind,
		ControllerName: controllerName,
		OwnerUID:       ownerUID,
		CustomMetrics:  usage.Custom,
		NamespaceSLO:   sloCopy,
		LastEvicted:    lastEviction,
	}
}

func controllerRef(ownerRefs []metav1.OwnerReference) (string, string, types.UID) {
	for _, owner := range ownerRefs {
		if owner.Controller != nil && *owner.Controller {
			return owner.Kind, owner.Name, owner.UID
		}
	}
	return "", "", ""
}

func addResourceLists(dst, src corev1.ResourceList) corev1.ResourceList {
	if dst == nil {
		dst = corev1.ResourceList{}
	}
	for k, v := range src {
		q := dst[k]
		q.Add(v)
		dst[k] = q
	}
	return dst
}

func (p *Planner) filterCandidates(snapshot *Snapshot, policy *rbv1.RebalancePolicy, hotNodes []*NodeState) []*PodState {
	var filtered []*PodState
	namespaceRules := compileNamespaceRules(policy.Spec.Namespaces)
	labelSelectors := buildSelectorSet(policy.Spec.WorkloadSelectors)

	backoffDuration := defaultBackoff
	if policy.Spec.BackoffDuration != nil {
		backoffDuration = policy.Spec.BackoffDuration.Duration
	}

	for _, pod := range snapshot.Pods {
		if pod.Object.Spec.NodeName == "" {
			continue
		}
		if !namespaceRules.Allow(pod.Object.Namespace) {
			continue
		}
		if len(labelSelectors) > 0 && !matchesAnySelector(labelSelectors, pod.Object) {
			continue
		}
		if isPinned(pod.Object) {
			continue
		}
		if policy.Spec.RespectPodPriority && isHighPriority(pod, snapshot.PriorityClasses) {
			continue
		}
		if pod.ControllerKind == "DaemonSet" {
			continue
		}
		if policy.Spec.RespectPDB && !pdbAllows(snapshot.PDBs, pod.Object) {
			continue
		}
		if pod.ControllerKind == "StatefulSet" {
			if pod.NamespaceSLO == nil || !pod.NamespaceSLO.Spec.AllowStatefulSetEviction {
				continue
			}
		}
		if pod.NamespaceSLO != nil && pod.NamespaceSLO.Spec.MinReadySeconds > 0 {
			if !meetsMinReady(p.clock.Now(), pod.Object, pod.NamespaceSLO.Spec.MinReadySeconds) {
				continue
			}
		}
		if skipLocalStorage(p.options.EmptyDirLimitGi, pod.Object) {
			continue
		}
		if skipHostPath(pod.Object) {
			continue
		}
		if !isHotNode(hotNodes, pod.NodeName) {
			continue
		}
		if hasBackoff(p.clock, pod, backoffDuration) {
			continue
		}
		pod.MatchesPolicy = true
		filtered = append(filtered, pod)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Priority < filtered[j].Priority
	})

	return filtered
}

func isHotNode(nodes []*NodeState, name string) bool {
	for _, n := range nodes {
		if n.Object.Name == name {
			return true
		}
	}
	return false
}

func skipLocalStorage(thresholdGi int64, pod *corev1.Pod) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.EmptyDir != nil {
			if volume.EmptyDir.SizeLimit != nil {
				limitGi := volume.EmptyDir.SizeLimit.Value() / (1 << 30)
				if limitGi > thresholdGi {
					return true
				}
			} else {
				return true
			}
		}
		if volume.HostPath != nil {
			return true
		}
	}
	return false
}

func skipHostPath(pod *corev1.Pod) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.HostPath != nil {
			return true
		}
	}
	return false
}

func hasBackoff(clk clock.Clock, pod *PodState, duration time.Duration) bool {
	if pod.LastEvicted == nil {
		return false
	}
	return clk.Now().Sub(*pod.LastEvicted) < duration
}

func compileNamespaceRules(selector rbv1.NamespaceSelector) namespaceRules {
	include := sets.NewString(selector.Include...)
	exclude := sets.NewString(selector.Exclude...)
	return namespaceRules{
		include: include,
		exclude: exclude,
	}
}

type namespaceRules struct {
	include sets.String
	exclude sets.String
}

func (n namespaceRules) Allow(ns string) bool {
	if n.exclude.Has(ns) {
		return false
	}
	if n.include.Len() == 0 {
		return true
	}
	if n.include.Has("*") {
		return !n.exclude.Has(ns)
	}
	return n.include.Has(ns)
}

func buildSelectorSet(selectors []metav1.LabelSelector) []labels.Selector {
	var compiled []labels.Selector
	for _, sel := range selectors {
		selector, err := metav1.LabelSelectorAsSelector(&sel)
		if err != nil {
			continue
		}
		compiled = append(compiled, selector)
	}
	return compiled
}

func matchesAnySelector(selectors []labels.Selector, pod *corev1.Pod) bool {
	if len(selectors) == 0 {
		return true
	}
	ls := labels.Set(pod.GetLabels())
	for _, selector := range selectors {
		if selector.Matches(ls) {
			return true
		}
	}
	return false
}

func isPinned(pod *corev1.Pod) bool {
	ann := pod.Annotations
	if ann == nil {
		return false
	}
	if ann["rebalancer.dev/pinned"] == "true" {
		return true
	}
	if ann["rebalancer.dev/allow-move"] == "false" {
		return true
	}
	return false
}

func meetsMinReady(now time.Time, pod *corev1.Pod, minReadySeconds int32) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			readyFor := now.Sub(condition.LastTransitionTime.Time)
			return readyFor >= time.Duration(minReadySeconds)*time.Second
		}
	}
	return false
}

func isHighPriority(pod *PodState, classes map[string]*schedulingv1.PriorityClass) bool {
	if pod.Priority >= systemCriticalPriority {
		return true
	}
	if pod.Object.Spec.PriorityClassName != "" {
		if pc, ok := classes[pod.Object.Spec.PriorityClassName]; ok {
			return pc.Value >= systemCriticalPriority
		}
	}
	return false
}

func pdbAllows(pdbs []*policyv1.PodDisruptionBudget, pod *corev1.Pod) bool {
	if len(pdbs) == 0 {
		return true
	}
	labelsSet := labels.Set(pod.Labels)
	for _, pdb := range pdbs {
		if pdb.Namespace != pod.Namespace {
			continue
		}
		if pdb.Spec.Selector == nil {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
		if err != nil {
			continue
		}
		if selector.Matches(labelsSet) && pdb.Status.DisruptionsAllowed <= 0 {
			return false
		}
	}
	return true
}

func (p *Planner) greedyPlan(snapshot *Snapshot, candidates []*PodState, policy *rbv1.RebalancePolicy, hot []*NodeState, cold []*NodeState, util map[string]float64) []PlanEviction {
	var plan []PlanEviction
	targetVariance := policy.Spec.TargetVariance
	currentVariance := computeVariance(util)
	visitedPods := sets.NewString()

	for _, candidate := range candidates {
		if visitedPods.Has(candidate.Object.Namespace + "/" + candidate.Object.Name) {
			continue
		}
		bestEviction, updatedUtil, varianceAfter := evaluateCandidate(snapshot, candidate, policy, util, currentVariance, cold)
		if bestEviction == nil {
			continue
		}
		if varianceAfter >= currentVariance {
			continue
		}
		plan = append(plan, *bestEviction)
		currentVariance = varianceAfter
		if updatedUtil != nil {
			util = updatedUtil
			applyMove(snapshot, candidate, bestEviction.TargetCandidates[0])
		}
		visitedPods.Insert(candidate.Object.Namespace + "/" + candidate.Object.Name)
		if currentVariance <= targetVariance {
			break
		}
	}

	sort.Slice(plan, func(i, j int) bool {
		return plan[i].Score < plan[j].Score
	})
	return plan
}

func evaluateCandidate(snapshot *Snapshot, pod *PodState, policy *rbv1.RebalancePolicy, util map[string]float64, currentVariance float64, cold []*NodeState) (*PlanEviction, map[string]float64, float64) {
	var best *PlanEviction
	bestVariance := math.MaxFloat64
	var bestUtil map[string]float64
	requestCPU := float64(pod.Requests.Cpu().MilliValue()) / 1000.0
	requestMem := float64(pod.Requests.Memory().Value())
	if requestCPU == 0 {
		requestCPU = 0.05
	}
	if requestMem == 0 {
		requestMem = 50 * 1024 * 1024
	}

	preferredClasses := sets.NewString(policy.Spec.NodeClassPreference...)
	for _, node := range cold {
		if !canSchedule(snapshot, pod.Object, node.Object) {
			continue
		}
		newUtil, variance := simulateVariance(util, pod.NodeName, node.Object.Name, requestCPU, requestMem, policy.Spec.ResourceWeights, snapshot.Nodes)
		delta := variance - currentVariance
		if preferredClasses.Len() > 0 {
			if hasPreferredClass(node.Classes, preferredClasses) {
				delta -= 0.01
			} else {
				delta += 0.01
			}
		}
		if delta >= 0 {
			continue
		}
		eviction := PlanEviction{
			Pod:              *pod.Object.DeepCopy(),
			FromNode:         pod.NodeName,
			TargetCandidates: []string{node.Object.Name},
			Score:            delta,
			Reason:           fmt.Sprintf("Reduce variance by %.4f", math.Abs(delta)),
			Namespace:        pod.Object.Namespace,
		}

		if variance < bestVariance {
			best = &eviction
			bestVariance = variance
			bestUtil = newUtil
		}
	}

	return best, bestUtil, bestVariance
}

func simulateVariance(util map[string]float64, from, to string, cpu float64, mem float64, weights rbv1.ResourceWeights, nodes map[string]*NodeState) (map[string]float64, float64) {
	newUtil := make(map[string]float64, len(util))
	for k, v := range util {
		newUtil[k] = v
	}

	fromNode := nodes[from]
	toNode := nodes[to]
	if fromNode == nil || toNode == nil {
		return util, computeVariance(util)
	}

	cpuAllocFrom := float64(fromNode.Object.Status.Allocatable.Cpu().MilliValue()) / 1000.0
	cpuAllocTo := float64(toNode.Object.Status.Allocatable.Cpu().MilliValue()) / 1000.0
	memAllocFrom := float64(fromNode.Object.Status.Allocatable.Memory().Value())
	memAllocTo := float64(toNode.Object.Status.Allocatable.Memory().Value())

	if cpuAllocFrom > 0 {
		fromCPU := fromNode.Usage.CPUCores - cpu
		if fromCPU < 0 {
			fromCPU = 0
		}
		fromMem := float64(fromNode.Usage.MemoryBytes) - mem
		if fromMem < 0 {
			fromMem = 0
		}
		newUtil[from] = (fromCPU/cpuAllocFrom)*weights.CPU + (fromMem/memAllocFrom)*weights.Memory
	}
	if cpuAllocTo > 0 {
		toCPU := toNode.Usage.CPUCores + cpu
		toMem := float64(toNode.Usage.MemoryBytes) + mem
		newUtil[to] = (toCPU/cpuAllocTo)*weights.CPU + (toMem/memAllocTo)*weights.Memory
	}

	return newUtil, computeVariance(newUtil)
}

func applyMove(snapshot *Snapshot, pod *PodState, targetNode string) {
	fromNode := snapshot.Nodes[pod.NodeName]
	toNode := snapshot.Nodes[targetNode]
	if fromNode == nil || toNode == nil {
		return
	}
	cpuDelta := float64(pod.Requests.Cpu().MilliValue()) / 1000.0
	memDelta := float64(pod.Requests.Memory().Value())
	if cpuDelta == 0 {
		cpuDelta = 0.05
	}
	if memDelta == 0 {
		memDelta = 50 * 1024 * 1024
	}
	fromNode.Usage.CPUCores -= cpuDelta
	if fromNode.Usage.CPUCores < 0 {
		fromNode.Usage.CPUCores = 0
	}
	fromNode.Usage.MemoryBytes -= int64(memDelta)
	toNode.Usage.CPUCores += cpuDelta
	toNode.Usage.MemoryBytes += int64(memDelta)
}

func nodeMatchesClass(node *corev1.Node, class *rbv1.NodeClass) bool {
	selector := metav1.LabelSelector{
		MatchLabels:      class.Spec.MatchLabels,
		MatchExpressions: class.Spec.MatchExpressions,
	}
	compiled, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return false
	}
	return compiled.Matches(labels.Set(node.Labels))
}

func hasPreferredClass(classes []string, preferred sets.String) bool {
	for _, class := range classes {
		if preferred.Has(class) {
			return true
		}
	}
	return false
}

func canSchedule(snapshot *Snapshot, pod *corev1.Pod, node *corev1.Node) bool {
	if !toleratesTaints(pod, node) {
		return false
	}
	if !matchesNodeSelector(pod, node) {
		return false
	}
	if !matchesNodeAffinity(pod, node) {
		return false
	}
	if !passesPodAffinity(snapshot, pod, node) {
		return false
	}
	return true
}

func (p *Planner) lastEvicted(name types.NamespacedName) *time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ts, ok := p.history[name]; ok {
		t := ts
		return &t
	}
	return nil
}

// RecordEviction marks a pod as recently evicted to enforce backoff windows.
func (p *Planner) RecordEviction(pod types.NamespacedName) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock.Now()
	p.history[pod] = now
	history := p.nsHistory[pod.Namespace]
	cutoff := now.Add(-1 * time.Hour)
	filtered := history[:0]
	for _, ts := range history {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	filtered = append(filtered, now)
	p.nsHistory[pod.Namespace] = append([]time.Time{}, filtered...)
}

func (p *Planner) canDisruptNamespace(namespace string, policy *rbv1.RebalancePolicy, slo *rbv1.WorkloadSLO, currentPlanCount int) bool {
	if policy.Spec.MaxEvictionsPerNamespace > 0 && currentPlanCount >= policy.Spec.MaxEvictionsPerNamespace {
		return false
	}
	if slo == nil || slo.Spec.MaxDisruptionsPerHour == 0 {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock.Now()
	cutoff := now.Add(-1 * time.Hour)
	history := p.nsHistory[namespace]
	filtered := history[:0]
	for _, ts := range history {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	if len(filtered) >= int(slo.Spec.MaxDisruptionsPerHour) {
		p.nsHistory[namespace] = filtered
		return false
	}
	p.nsHistory[namespace] = filtered
	return true
}

func (p *Planner) remainingNamespaceBudget(namespace string, policy *rbv1.RebalancePolicy, slo *rbv1.WorkloadSLO) int {
	limit := -1
	if policy.Spec.MaxEvictionsPerNamespace > 0 {
		limit = policy.Spec.MaxEvictionsPerNamespace
	}
	if slo != nil && slo.Spec.MaxDisruptionsPerHour > 0 {
		p.mu.Lock()
		now := p.clock.Now()
		cutoff := now.Add(-1 * time.Hour)
		history := p.nsHistory[namespace]
		filtered := history[:0]
		for _, ts := range history {
			if ts.After(cutoff) {
				filtered = append(filtered, ts)
			}
		}
		p.nsHistory[namespace] = filtered
		remaining := int(slo.Spec.MaxDisruptionsPerHour) - len(filtered)
		if remaining < 0 {
			remaining = 0
		}
		p.mu.Unlock()
		if limit == -1 || remaining < limit {
			limit = remaining
		}
	}
	return limit
}
