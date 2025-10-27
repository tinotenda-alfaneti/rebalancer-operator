package planner

import (
	"testing"
	"time"

	rbv1 "github.com/you/rebalancer/api/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
)

func TestNamespaceRules(t *testing.T) {
	rules := compileNamespaceRules(rbv1.NamespaceSelector{Include: []string{"*"}, Exclude: []string{"kube-system"}})
	if !rules.Allow("default") {
		t.Fatalf("expected default namespace to be allowed")
	}
	if rules.Allow("kube-system") {
		t.Fatalf("expected kube-system to be excluded")
	}
}

func TestPDBAllows(t *testing.T) {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
		},
		Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Labels: map[string]string{"app": "web"}}}
	if pdbAllows([]*policyv1.PodDisruptionBudget{pdb}, pod) {
		t.Fatalf("expected pdb to block eviction when disruptionsAllowed=0")
	}
	pdb.Status.DisruptionsAllowed = 1
	if !pdbAllows([]*policyv1.PodDisruptionBudget{pdb}, pod) {
		t.Fatalf("expected pdb to allow eviction when disruptionsAllowed>0")
	}
}

func TestIsHighPriority(t *testing.T) {
	pc := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "system-critical"}, Value: int32(systemCriticalPriority)}
	classes := map[string]*schedulingv1.PriorityClass{"system-critical": pc}
	pod := &PodState{
		Priority: systemCriticalPriority,
		Object:   &corev1.Pod{Spec: corev1.PodSpec{PriorityClassName: "system-critical"}},
	}
	if !isHighPriority(pod, classes) {
		t.Fatalf("expected pod with critical priority class to be treated as high priority")
	}
	pod.Priority = 0
	pod.Object.Spec.PriorityClassName = ""
	if isHighPriority(pod, classes) {
		t.Fatalf("expected pod without priority class to be considered movable")
	}
}

func TestMeetsMinReady(t *testing.T) {
	now := time.Now()
	pod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
		Type:               corev1.PodReady,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Minute)),
	}}}}
	if !meetsMinReady(now, pod, 30) {
		t.Fatalf("expected pod ready for >30s to satisfy requirement")
	}
	if meetsMinReady(now, pod, 600) {
		t.Fatalf("expected pod to fail 10 minute readiness requirement")
	}
}

func TestRemainingNamespaceBudget(t *testing.T) {
	fakeClock := clocktesting.NewFakeClock(time.Now())
	pl := &Planner{
		clock:     fakeClock,
		nsHistory: map[string][]time.Time{"prod": {fakeClock.Now().Add(-10 * time.Minute)}},
		history:   map[types.NamespacedName]time.Time{},
	}
	policy := &rbv1.RebalancePolicy{Spec: rbv1.RebalancePolicySpec{MaxEvictionsPerNamespace: 3}}
	slo := &rbv1.WorkloadSLO{Spec: rbv1.WorkloadSLOSpec{MaxDisruptionsPerHour: 2}}
	remaining := pl.remainingNamespaceBudget("prod", policy, slo)
	if remaining != 1 {
		t.Fatalf("expected remaining budget of 1, got %d", remaining)
	}
	pl.nsHistory["prod"] = []time.Time{fakeClock.Now().Add(-30 * time.Minute), fakeClock.Now().Add(-20 * time.Minute)}
	remaining = pl.remainingNamespaceBudget("prod", policy, slo)
	if remaining != 0 {
		t.Fatalf("expected budget to be exhausted, got %d", remaining)
	}
}
