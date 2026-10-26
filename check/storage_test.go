package check

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func withClaim(p corev1.Pod, claim string) corev1.Pod {
	p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{Name: "data",
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}}})
	return p
}

func mkPV(name, key string, values ...string) corev1.PersistentVolume {
	return corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{NodeAffinity: &corev1.VolumeNodeAffinity{Required: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{
				{Key: key, Operator: corev1.NodeSelectorOpIn, Values: values}}}}}}}}
}

func mkPVC(name, pv string) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "shop"},
		Spec: corev1.PersistentVolumeClaimSpec{VolumeName: pv}}
}

func mkNode(name, zone string, cordoned bool) corev1.Node {
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name,
		Labels: map[string]string{"kubernetes.io/hostname": name, "topology.kubernetes.io/zone": zone}},
		Spec: corev1.NodeSpec{Unschedulable: cordoned}}
}

func TestPinnedVolumes(t *testing.T) {
	nodes := []corev1.Node{mkNode("n1", "a", false), mkNode("n2", "a", true), mkNode("n3", "b", false)}
	pods := []corev1.Pod{
		withClaim(mkPod("local-0", "db", "StatefulSet", true), "local"),
		withClaim(mkPod("zonal-0", "db", "StatefulSet", true), "zonal"),
		withClaim(mkPod("roam-0", "db", "StatefulSet", true), "roam"),
	}
	pvcs := []corev1.PersistentVolumeClaim{mkPVC("local", "pv-local"), mkPVC("zonal", "pv-zonal"), mkPVC("roam", "pv-roam")}
	pvs := []corev1.PersistentVolume{
		mkPV("pv-local", "kubernetes.io/hostname", "n1"),
		mkPV("pv-zonal", "topology.kubernetes.io/zone", "a"), // n2 is in zone a but cordoned
		mkPV("pv-roam", "topology.kubernetes.io/zone", "a", "b"),
	}
	got := PinnedVolumes("n1", pods, pvcs, pvs, nodes)
	if len(got) != 2 || got["shop/local-0"] == "" || got["shop/zonal-0"] == "" {
		t.Fatalf("want local-0 and zonal-0 pinned, got %v", got)
	}
	if !strings.Contains(got["shop/local-0"], "kubernetes.io/hostname in (n1)") {
		t.Errorf("explanation should name the affinity: %s", got["shop/local-0"])
	}
	nodes[1].Spec.Unschedulable = false
	if got := PinnedVolumes("n1", pods, pvcs, pvs, nodes); got["shop/zonal-0"] != "" {
		t.Errorf("zonal volume can follow to uncordoned n2: %v", got)
	}
}

func TestPinnedPodsBeyondBudgetBlock(t *testing.T) {
	pods := []corev1.Pod{mkPod("db-0", "db", "StatefulSet", true), mkPod("db-1", "db", "StatefulSet", true)}
	pdb := []policyv1.PodDisruptionBudget{mkPDB("db", "db", 1, 3, 2, 3)}
	pin := map[string]string{"shop/db-0": "PersistentVolume x", "shop/db-1": "PersistentVolume y"}
	r := Node("n1", pods, pdb, Options{Pinned: pin})
	if r.Verdict != Blocked || !strings.Contains(find(r, "db-0").Reason, "never become ready") {
		t.Fatalf("two pinned pods, budget 1: want BLOCKED, got %s %+v", r.Verdict, r.Pods)
	}
	// Without the pin it is merely slow.
	if r := Node("n1", pods, pdb, Options{}); r.Verdict != Wait {
		t.Fatalf("want WAIT without pins, got %s", r.Verdict)
	}
}

func TestPinnedPodWithinBudgetWarns(t *testing.T) {
	pods := []corev1.Pod{mkPod("db-0", "db", "StatefulSet", true)}
	pdb := []policyv1.PodDisruptionBudget{mkPDB("db", "db", 1, 3, 2, 3)}
	r := Node("n1", pods, pdb, Options{Pinned: map[string]string{"shop/db-0": "PersistentVolume x"}})
	p := find(r, "db-0")
	if r.Verdict != OK || p.Fix == "" || !strings.Contains(p.Reason, "stays Pending") {
		t.Fatalf("want OK with a heads-up, got %s %+v", r.Verdict, p)
	}
}

func TestPinnedPodWithoutPDB(t *testing.T) {
	pods := []corev1.Pod{mkPod("cache-0", "cache", "StatefulSet", true)}
	r := Node("n1", pods, nil, Options{Pinned: map[string]string{"shop/cache-0": "PersistentVolume x"}})
	if p := find(r, "cache-0"); r.Verdict != OK || p.Fix == "" {
		t.Fatalf("want OK with a downtime note, got %s %+v", r.Verdict, p)
	}
}
