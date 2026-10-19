package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

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

func TestDisplayName(t *testing.T) {
	for in, want := range map[string]string{
		"drainready":                           "drainready",
		"/usr/local/bin/drainready":            "drainready",
		"/home/u/.krew/bin/kubectl-drainready": "kubectl drainready",
		`C:\krew\bin\kubectl-drainready.exe`:   "kubectl drainready",
	} {
		if got := displayName(strings.ReplaceAll(in, `\`, "/")); got != want {
			t.Errorf("displayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUsageAsPlugin(t *testing.T) {
	old := progName
	progName = "kubectl drainready"
	defer func() { progName = old }()
	var o, e strings.Builder
	run(nil, &o, &e)
	if !strings.Contains(e.String(), "kubectl drainready [flags] NODE") {
		t.Fatalf("usage: %s", e.String())
	}
}

func TestMarkdown(t *testing.T) {
	res := []check.NodeResult{
		{Node: "n1", Verdict: check.Blocked, Pods: []check.PodResult{
			{Namespace: "d", Pod: "db-0", Verdict: check.Blocked, PDBs: []string{"d/db"}, Reason: "PDB d/db allows 0 disruptions", Fix: "a | b\nc"},
			{Namespace: "d", Pod: "web-1", Verdict: check.OK, Reason: "no PodDisruptionBudget covers it"},
		}},
		{Node: "n2", Verdict: check.OK, Pods: []check.PodResult{{Namespace: "d", Pod: "web-2", Verdict: check.OK}}},
	}
	var b strings.Builder
	Markdown(&b, res, false, true, time.Date(2026, 10, 19, 2, 30, 0, 0, time.UTC))
	out := b.String()
	for _, want := range []string{
		"## Drain readiness: BLOCKED",
		"2026-10-19 02:30 UTC",
		"confirmed with server dry-run evictions",
		"### BLOCKED `n1`",
		"| **BLOCKED** | `d/db-0` | `d/db` | PDB d/db allows 0 disruptions | a \\| b c |",
		"### READY `n2`",
		"No pod on this node would stall the drain.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "web-1") {
		t.Errorf("OK pods should be hidden without -v:\n%s", out)
	}
}

func TestJSONAndMarkdownExclusive(t *testing.T) {
	var o, e strings.Builder
	if run([]string{"--json", "--markdown", "n1"}, &o, &e) != 2 || !strings.Contains(e.String(), "not both") {
		t.Fatalf("want exit 2, got %q", e.String())
	}
}
