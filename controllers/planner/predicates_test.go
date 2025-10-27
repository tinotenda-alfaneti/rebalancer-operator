package planner

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestToleratesTaints(t *testing.T) {
	node := &corev1.Node{Spec: corev1.NodeSpec{Taints: []corev1.Taint{{
		Key:    "node-role.kubernetes.io/infra",
		Effect: corev1.TaintEffectNoSchedule,
	}}}}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Tolerations: []corev1.Toleration{{
		Key:      "node-role.kubernetes.io/infra",
		Operator: corev1.TolerationOpEqual,
		Value:    "",
	}}}}
	if !toleratesTaints(pod, node) {
		t.Fatalf("expected toleration to satisfy taint")
	}
	pod.Spec.Tolerations = nil
	if toleratesTaints(pod, node) {
		t.Fatalf("expected pod without toleration to fail taint")
	}
}

func TestMatchesNodeSelector(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{NodeSelector: map[string]string{"kubernetes.io/arch": "arm64"}}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"kubernetes.io/arch": "arm64"}}}
	if !matchesNodeSelector(pod, node) {
		t.Fatalf("expected node selector to match labels")
	}
	node.Labels["kubernetes.io/arch"] = "amd64"
	if matchesNodeSelector(pod, node) {
		t.Fatalf("expected mismatch when label differs")
	}
}
