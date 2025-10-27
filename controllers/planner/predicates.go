package planner

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func toleratesTaints(pod *corev1.Pod, node *corev1.Node) bool {
	if len(node.Spec.Taints) == 0 {
		return true
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectPreferNoSchedule {
			continue
		}
		if taint.Effect != corev1.TaintEffectNoSchedule && taint.Effect != corev1.TaintEffectNoExecute {
			continue
		}
		if !tolerationMatches(taint, pod.Spec.Tolerations) {
			return false
		}
	}
	return true
}

func tolerationMatches(taint corev1.Taint, tolerations []corev1.Toleration) bool {
	for _, tol := range tolerations {
		if tol.Key != taint.Key {
			continue
		}
		if tol.Effect != "" && tol.Effect != taint.Effect {
			continue
		}
		if tol.Operator == corev1.TolerationOpExists {
			return true
		}
		if tol.Value == taint.Value {
			return true
		}
	}
	return false
}

func matchesNodeSelector(pod *corev1.Pod, node *corev1.Node) bool {
	if len(pod.Spec.NodeSelector) == 0 {
		return true
	}
	labels := node.GetLabels()
	for key, val := range pod.Spec.NodeSelector {
		if labels[key] != val {
			return false
		}
	}
	return true
}

func matchesNodeAffinity(pod *corev1.Pod, node *corev1.Node) bool {
	affinity := pod.Spec.Affinity
	if affinity == nil || affinity.NodeAffinity == nil {
		return true
	}
	required := affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if required == nil {
		return true
	}
	for _, term := range required.NodeSelectorTerms {
		if matchNodeSelectorTerm(node, term) {
			return true
		}
	}
	return false
}

func matchNodeSelectorTerm(node *corev1.Node, term corev1.NodeSelectorTerm) bool {
	for _, expr := range term.MatchExpressions {
		if !matchNodeSelectorRequirement(node, expr) {
			return false
		}
	}
	for _, field := range term.MatchFields {
		if !matchFieldSelectorRequirement(node, field) {
			return false
		}
	}
	return true
}

func matchNodeSelectorRequirement(node *corev1.Node, req corev1.NodeSelectorRequirement) bool {
	nodeLabels := node.GetLabels()
	value := nodeLabels[req.Key]
	switch req.Operator {
	case corev1.NodeSelectorOpIn:
		return contains(req.Values, value)
	case corev1.NodeSelectorOpNotIn:
		return !contains(req.Values, value)
	case corev1.NodeSelectorOpExists:
		return value != ""
	case corev1.NodeSelectorOpDoesNotExist:
		return value == ""
	case corev1.NodeSelectorOpGt:
		if len(req.Values) == 0 {
			return false
		}
		val, err1 := strconv.ParseInt(value, 10, 64)
		target, err2 := strconv.ParseInt(req.Values[0], 10, 64)
		return err1 == nil && err2 == nil && val > target
	case corev1.NodeSelectorOpLt:
		if len(req.Values) == 0 {
			return false
		}
		val, err1 := strconv.ParseInt(value, 10, 64)
		target, err2 := strconv.ParseInt(req.Values[0], 10, 64)
		return err1 == nil && err2 == nil && val < target
	default:
		return false
	}
}

func matchFieldSelectorRequirement(node *corev1.Node, req corev1.NodeSelectorRequirement) bool {
	var value string
	switch req.Key {
	case "metadata.name":
		value = node.Name
	default:
		return true
	}
	switch req.Operator {
	case corev1.NodeSelectorOpIn:
		return contains(req.Values, value)
	case corev1.NodeSelectorOpNotIn:
		return !contains(req.Values, value)
	case corev1.NodeSelectorOpExists:
		return value != ""
	case corev1.NodeSelectorOpDoesNotExist:
		return value == ""
	default:
		return true
	}
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func passesPodAffinity(snapshot *Snapshot, pod *corev1.Pod, node *corev1.Node) bool {
	affinity := pod.Spec.Affinity
	if affinity == nil {
		return true
	}
	if affinity.PodAffinity != nil {
		for _, term := range affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
			if !checkPodAffinityTerm(snapshot, pod, node, term, true) {
				return false
			}
		}
	}
	if affinity.PodAntiAffinity != nil {
		for _, term := range affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
			if !checkPodAffinityTerm(snapshot, pod, node, term, false) {
				return false
			}
		}
	}
	return true
}

func checkPodAffinityTerm(snapshot *Snapshot, pod *corev1.Pod, node *corev1.Node, term corev1.PodAffinityTerm, positive bool) bool {
	selector := labels.Everything()
	if term.LabelSelector != nil {
		compiled, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
		if err != nil {
			return !positive
		}
		selector = compiled
	}
	namespaces := term.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{pod.Namespace}
	}
	topologyKey := term.TopologyKey
	if topologyKey == "" {
		topologyKey = corev1.LabelHostname
	}
	targetValue := node.Labels[topologyKey]
	if targetValue == "" {
		return !positive
	}

	for _, podState := range snapshot.Pods {
		p := podState.Object
		if p.UID == pod.UID {
			continue
		}
		if !contains(namespaces, p.Namespace) {
			continue
		}
		if !selector.Matches(labels.Set(p.Labels)) {
			continue
		}
		nodeState := snapshot.Nodes[p.Spec.NodeName]
		if nodeState == nil {
			continue
		}
		value := nodeState.Object.Labels[topologyKey]
		if value == "" {
			continue
		}
		if positive {
			if value == targetValue {
				return true
			}
		} else {
			if value == targetValue {
				return false
			}
		}
	}

	return !positive
}
