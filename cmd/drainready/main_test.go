package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Blackman99/drainready/check"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEvaluateFake(t *testing.T) {
	tr := true
	min := intstr.FromInt32(1)
	cs := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "db-0", Namespace: "d", Labels: map[string]string{"app": "db"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db", Controller: &tr}}},
			Spec: corev1.PodSpec{NodeName: "n1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}},
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "d"},
			Spec:   policyv1.PodDisruptionBudgetSpec{MinAvailable: &min, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}}},
			Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0, CurrentHealthy: 1, DesiredHealthy: 1, ExpectedPods: 1}},
	)
	res, err := Evaluate(context.Background(), cs, nil, "", check.Options{}, false)
	if err != nil || len(res) != 1 || res[0].Verdict != check.Blocked {
		t.Fatalf("%v %+v", err, res)
	}
	var b bytes.Buffer
	Print(&b, res, false)
	if !strings.Contains(b.String(), "BLOCKED  n1") || !strings.Contains(b.String(), "d/db-0") {
		t.Fatal(b.String())
	}
}

func TestUsageExit(t *testing.T) {
	var o, e bytes.Buffer
	if run(nil, &o, &e) != 2 || !strings.Contains(e.String(), "Usage") {
		t.Fatal("expected usage")
	}
	if run([]string{"--version"}, &o, &e) != 0 {
		t.Fatal("version")
	}
}
