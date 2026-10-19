package check

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func mkPod(name, app, kind string, ready bool) corev1.Pod {
	p := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "shop", Labels: map[string]string{"app": app}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	if kind != "" {
		t := true
		p.OwnerReferences = []metav1.OwnerReference{{Kind: kind, Name: app, Controller: &t}}
	}
	st := corev1.ConditionFalse
	if ready {
		st = corev1.ConditionTrue
	}
	p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: st}}
	return p
}

func mkPDB(name, app string, allowed, healthy, desired, expected int32) policyv1.PodDisruptionBudget {
	min := intstr.FromInt32(desired)
	return policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "shop"},
		Spec:   policyv1.PodDisruptionBudgetSpec{MinAvailable: &min, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": app}}},
		Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: allowed, CurrentHealthy: healthy, DesiredHealthy: desired, ExpectedPods: expected}}
}

func find(r NodeResult, pod string) PodResult {
	for _, p := range r.Pods {
		if p.Pod == pod {
			return p
		}
	}
	return PodResult{}
}

func TestBlockedByMinAvailableEqualsReplicas(t *testing.T) {
	r := Node("n1", []corev1.Pod{mkPod("db-0", "db", "StatefulSet", true)},
		[]policyv1.PodDisruptionBudget{mkPDB("db-pdb", "db", 0, 2, 2, 2)}, Options{})
	if r.Verdict != Blocked {
		t.Fatalf("want BLOCKED got %s", r.Verdict)
	}
	p := find(r, "db-0")
	if p.PDBs[0] != "shop/db-pdb" || p.Fix == "" {
		t.Fatalf("bad result %+v", p)
	}
}

func TestOKAndWait(t *testing.T) {
	pods := []corev1.Pod{mkPod("web-1", "web", "ReplicaSet", true), mkPod("web-2", "web", "ReplicaSet", true)}
	r := Node("n1", pods, []policyv1.PodDisruptionBudget{mkPDB("web", "web", 2, 4, 2, 4)}, Options{})
	if r.Verdict != OK {
		t.Fatalf("want OK got %s", r.Verdict)
	}
	r = Node("n1", pods, []policyv1.PodDisruptionBudget{mkPDB("web", "web", 1, 3, 2, 3)}, Options{})
	if r.Verdict != Wait {
		t.Fatalf("want WAIT got %s", r.Verdict)
	}
}

func TestOverlappingPDBs(t *testing.T) {
	r := Node("n1", []corev1.Pod{mkPod("api-1", "api", "ReplicaSet", true)},
		[]policyv1.PodDisruptionBudget{mkPDB("a", "api", 1, 3, 2, 3), mkPDB("b", "api", 1, 3, 2, 3)}, Options{})
	if r.Verdict != Blocked {
		t.Fatalf("want BLOCKED got %s", r.Verdict)
	}
}

func TestSkipFlagsAndUnhealthy(t *testing.T) {
	ds := mkPod("fluent", "log", "DaemonSet", true)
	bare := mkPod("debug", "x", "", true)
	ed := mkPod("cache", "c", "ReplicaSet", true)
	ed.Spec.Volumes = []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
	r := Node("n1", []corev1.Pod{ds, bare, ed}, nil, Options{})
	if find(r, "fluent").Verdict != Skip || find(r, "debug").Verdict != Flag || find(r, "cache").Verdict != Flag {
		t.Fatalf("%+v", r.Pods)
	}
	if r2 := Node("n1", []corev1.Pod{ds, bare, ed}, nil, Options{Force: true, DeleteEmptyDirData: true}); r2.Verdict != OK {
		t.Fatalf("want OK with flags, got %s", r2.Verdict)
	}
	// unhealthy pod, budget not met, AlwaysAllow lets it go
	sick := mkPod("w-3", "w", "ReplicaSet", false)
	b := mkPDB("w", "w", 0, 1, 2, 3)
	if Node("n1", []corev1.Pod{sick}, []policyv1.PodDisruptionBudget{b}, Options{}).Verdict != Blocked {
		t.Fatal("want blocked")
	}
	pol := policyv1.AlwaysAllow
	b.Spec.UnhealthyPodEvictionPolicy = &pol
	if v := Node("n1", []corev1.Pod{sick}, []policyv1.PodDisruptionBudget{b}, Options{}).Verdict; v != OK {
		t.Fatalf("want OK got %s", v)
	}
}

func TestMaxUnavailableZeroFix(t *testing.T) {
	b := mkPDB("z", "z", 0, 3, 3, 3)
	zero := intstr.FromInt32(0)
	b.Spec.MinAvailable, b.Spec.MaxUnavailable = nil, &zero
	p := find(Node("n", []corev1.Pod{mkPod("z1", "z", "ReplicaSet", true)}, []policyv1.PodDisruptionBudget{b}, Options{}), "z1")
	if p.Verdict != Blocked || p.Fix[:16] != "maxUnavailable i" {
		t.Fatalf("%+v", p)
	}
}

func TestPercentZeroBudgetFixes(t *testing.T) {
	// maxUnavailable: 0% should get the clear structural fix, not a generic one
	b := mkPDB("z", "z", 0, 3, 3, 3)
	zeroPct := intstr.FromString("0%")
	b.Spec.MinAvailable, b.Spec.MaxUnavailable = nil, &zeroPct
	p := find(Node("n", []corev1.Pod{mkPod("z1", "z", "ReplicaSet", true)}, []policyv1.PodDisruptionBudget{b}, Options{}), "z1")
	if p.Verdict != Blocked || p.Fix[:16] != "maxUnavailable i" {
		t.Fatalf("0%% fix: %+v", p)
	}
	// minAvailable: 100% likewise
	b2 := mkPDB("full", "full", 0, 2, 2, 2)
	full := intstr.FromString("100%")
	b2.Spec.MinAvailable = &full
	p2 := find(Node("n", []corev1.Pod{mkPod("f1", "full", "ReplicaSet", true)}, []policyv1.PodDisruptionBudget{b2}, Options{}), "f1")
	if p2.Verdict != Blocked || p2.Fix[:12] != "minAvailable" {
		t.Fatalf("100%% fix: %+v", p2)
	}
}

func syncFailedPDB(stale bool) policyv1.PodDisruptionBudget {
	b := mkPDB("op", "op", 0, 0, 0, 0)
	one := intstr.FromInt32(1)
	b.Spec.MinAvailable, b.Spec.MaxUnavailable = nil, &one
	b.Generation = 1
	if !stale {
		b.Status.ObservedGeneration = 1
	}
	b.Status.Conditions = []metav1.Condition{{Type: policyv1.DisruptionAllowedCondition, Status: metav1.ConditionFalse,
		Reason: policyv1.SyncFailedReason, Message: "configmaps does not implement the scale subresource"}}
	return b
}

func TestSyncFailedIsBlockedEvenWhenStale(t *testing.T) {
	// failSafe never bumps observedGeneration, so a brand-new SyncFailed PDB
	// looks "stale"; the Eviction API still answers 429 forever.
	for _, stale := range []bool{true, false} {
		p := find(Node("n", []corev1.Pod{mkPod("op-0", "op", "Widget", true)},
			[]policyv1.PodDisruptionBudget{syncFailedPDB(stale)}, Options{}), "op-0")
		if p.Verdict != Blocked || !strings.Contains(p.Reason, "SyncFailed: configmaps does not implement") || !strings.Contains(p.Fix, "integer minAvailable") {
			t.Fatalf("stale=%v: %+v", stale, p)
		}
	}
	// AlwaysAllow still lets a not-ready pod go before the budget is consulted.
	b := syncFailedPDB(true)
	pol := policyv1.AlwaysAllow
	b.Spec.UnhealthyPodEvictionPolicy = &pol
	if v := Node("n", []corev1.Pod{mkPod("op-0", "op", "Widget", false)}, []policyv1.PodDisruptionBudget{b}, Options{}).Verdict; v != OK {
		t.Fatalf("AlwaysAllow not-ready: want OK got %s", v)
	}
}

func TestUnhealthyNeedsPositiveDesired(t *testing.T) {
	// Eviction API admits a not-ready pod under IfHealthyBudget only when
	// desiredHealthy > 0; 0 >= 0 must not count as "budget met".
	b := mkPDB("e", "e", 0, 0, 0, 1)
	p := find(Node("n", []corev1.Pod{mkPod("e-0", "e", "ReplicaSet", false)}, []policyv1.PodDisruptionBudget{b}, Options{}), "e-0")
	if p.Verdict != Blocked {
		t.Fatalf("want BLOCKED got %+v", p)
	}
}

func TestNegativeAllowed(t *testing.T) {
	b := mkPDB("n", "n", -1, 1, 2, 2)
	p := find(Node("n", []corev1.Pod{mkPod("n-0", "n", "ReplicaSet", true)}, []policyv1.PodDisruptionBudget{b}, Options{}), "n-0")
	if p.Verdict != Blocked || !strings.Contains(p.Reason, "403") {
		t.Fatalf("%+v", p)
	}
}
