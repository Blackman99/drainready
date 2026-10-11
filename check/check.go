// Package check predicts whether `kubectl drain` will finish on a node,
// before you cordon it, by reading pods and PodDisruptionBudgets.
package check

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Verdict for one pod (and, aggregated, for a node).
type Verdict string

const (
	OK      Verdict = "OK"      // eviction will be admitted now
	Skip    Verdict = "SKIP"    // drain ignores it (DaemonSet, mirror, finished)
	Wait    Verdict = "WAIT"    // will evict, but only after other replicas reschedule
	Flag    Verdict = "FLAG"    // drain refuses without an extra flag (--force, --delete-emptydir-data)
	Blocked Verdict = "BLOCKED" // drain will retry forever
)

var rank = map[Verdict]int{Skip: 0, OK: 1, Wait: 2, Flag: 3, Blocked: 4}

// Worse returns the more severe verdict.
func Worse(a, b Verdict) Verdict {
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// Options mirror the kubectl drain flags that change what is allowed.
type Options struct {
	Force              bool // --force: allow unmanaged pods
	DeleteEmptyDirData bool // --delete-emptydir-data
}

// PodResult explains the verdict for one pod.
type PodResult struct {
	Namespace string   `json:"namespace"`
	Pod       string   `json:"pod"`
	Verdict   Verdict  `json:"verdict"`
	PDBs      []string `json:"pdbs,omitempty"`
	Reason    string   `json:"reason"`
	Fix       string   `json:"fix,omitempty"`
}

// NodeResult aggregates a node.
type NodeResult struct {
	Node    string      `json:"node"`
	Verdict Verdict     `json:"verdict"`
	Pods    []PodResult `json:"pods"`
}

// Node evaluates every pod on node against the PDBs.
func Node(node string, pods []corev1.Pod, pdbs []policyv1.PodDisruptionBudget, opt Options) NodeResult {
	res := NodeResult{Node: node, Verdict: OK}
	// how many evictable pods on this node each PDB covers
	perPDB := map[string]int{}
	matches := make([][]*policyv1.PodDisruptionBudget, len(pods))
	for i := range pods {
		if skipReason(&pods[i]) != "" {
			continue
		}
		for j := range pdbs {
			if covers(&pdbs[j], &pods[i]) {
				matches[i] = append(matches[i], &pdbs[j])
				perPDB[key(&pdbs[j])]++
			}
		}
	}
	for i := range pods {
		r := pod(&pods[i], matches[i], perPDB, opt)
		res.Verdict = Worse(res.Verdict, r.Verdict)
		res.Pods = append(res.Pods, r)
	}
	sort.SliceStable(res.Pods, func(a, b int) bool { return rank[res.Pods[a].Verdict] > rank[res.Pods[b].Verdict] })
	return res
}

func key(p *policyv1.PodDisruptionBudget) string { return p.Namespace + "/" + p.Name }

func covers(p *policyv1.PodDisruptionBudget, pod *corev1.Pod) bool {
	if p.Namespace != pod.Namespace || p.Spec.Selector == nil {
		return false
	}
	sel, err := metav1.LabelSelectorAsSelector(p.Spec.Selector)
	if err != nil || sel.Empty() {
		// An empty selector matches every pod in the namespace for policy/v1.
		return err == nil
	}
	return sel.Matches(labels.Set(pod.Labels))
}

func skipReason(p *corev1.Pod) string {
	if _, ok := p.Annotations[corev1.MirrorPodAnnotationKey]; ok {
		return "static/mirror pod, drain ignores it"
	}
	if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
		return "finished pod, deleted without eviction"
	}
	if p.DeletionTimestamp != nil {
		return "already terminating"
	}
	if c := metav1.GetControllerOf(p); c != nil && c.Kind == "DaemonSet" {
		return "DaemonSet pod (needs --ignore-daemonsets)"
	}
	return ""
}

func ready(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func pod(p *corev1.Pod, m []*policyv1.PodDisruptionBudget, perPDB map[string]int, opt Options) PodResult {
	r := PodResult{Namespace: p.Namespace, Pod: p.Name, Verdict: OK, Reason: "no PodDisruptionBudget covers it"}
	if s := skipReason(p); s != "" {
		r.Verdict, r.Reason = Skip, s
		return r
	}
	for _, b := range m {
		r.PDBs = append(r.PDBs, key(b))
	}
	if metav1.GetControllerOf(p) == nil && !opt.Force {
		return PodResult{Namespace: p.Namespace, Pod: p.Name, Verdict: Flag, PDBs: r.PDBs,
			Reason: "bare pod with no controller: drain refuses and it will not come back",
			Fix:    "run it under a Deployment/StatefulSet, or drain with --force (the pod is lost)"}
	}
	for _, v := range p.Spec.Volumes {
		if v.EmptyDir != nil && !opt.DeleteEmptyDirData {
			r.Verdict, r.Reason = Flag, fmt.Sprintf("uses emptyDir volume %q: drain refuses", v.Name)
			r.Fix = "drain with --delete-emptydir-data (the data is lost)"
		}
	}
	if len(m) > 1 {
		r.Verdict = Blocked
		r.Reason = "covered by more than one PDB: the API server rejects every eviction (HTTP 500)"
		r.Fix = "make the PDB selectors disjoint so each pod matches exactly one"
		return r
	}
	if len(m) == 0 {
		return r
	}
	b := m[0]
	allowed := b.Status.DisruptionsAllowed
	healthy, desired, expected := b.Status.CurrentHealthy, b.Status.DesiredHealthy, b.Status.ExpectedPods
	if b.Status.ObservedGeneration < b.Generation {
		r.Verdict = Worse(r.Verdict, Wait)
		r.Reason = "PDB status is stale (controller has not observed the latest spec)"
		return r
	}
	// Unhealthy pods may be evicted regardless when AlwaysAllow is set, or
	// (IfHealthyBudget) when the budget is currently met.
	if !ready(p) {
		pol := b.Spec.UnhealthyPodEvictionPolicy
		if pol != nil && *pol == policyv1.AlwaysAllow {
			r.Reason = "not ready, and PDB allows evicting unhealthy pods (AlwaysAllow)"
			return r
		}
		if healthy >= desired {
			r.Reason = "not ready; budget is met so the eviction is admitted"
			return r
		}
	}
	switch {
	case allowed <= 0:
		r.Verdict = Blocked
		r.Reason = fmt.Sprintf("PDB %s allows 0 disruptions (healthy %d, needs %d, expected %d)", key(b), healthy, desired, expected)
		r.Fix = fixFor(b)
	case int32(perPDB[key(b)]) > allowed:
		r.Verdict = Worse(r.Verdict, Wait)
		r.Reason = fmt.Sprintf("%d pods of PDB %s are on this node but only %d may go at once: drain waits for replacements to become ready elsewhere", perPDB[key(b)], key(b), allowed)
		r.Fix = "make sure other nodes have room for the replacements, or raise replicas"
	default:
		if r.Verdict == OK {
			r.Reason = fmt.Sprintf("PDB %s allows %d disruption(s)", key(b), allowed)
		}
	}
	return r
}

func fixFor(b *policyv1.PodDisruptionBudget) string {
	s := b.Status
	switch {
	case s.CurrentHealthy < s.DesiredHealthy:
		return fmt.Sprintf("%d pod(s) already unhealthy: fix them, or set spec.unhealthyPodEvictionPolicy: AlwaysAllow", s.DesiredHealthy-s.CurrentHealthy)
	case b.Spec.MaxUnavailable != nil && b.Spec.MaxUnavailable.IntValue() == 0 && b.Spec.MaxUnavailable.String() != "" && b.Spec.MaxUnavailable.String()[len(b.Spec.MaxUnavailable.String())-1] != '%':
		return "maxUnavailable is 0: no voluntary disruption is ever allowed; use maxUnavailable: 1"
	case b.Spec.MinAvailable != nil && s.DesiredHealthy >= s.ExpectedPods:
		return fmt.Sprintf("minAvailable (%s) equals the replica count (%d): scale up first, or switch to maxUnavailable: 1", b.Spec.MinAvailable.String(), s.ExpectedPods)
	default:
		return "scale the workload up, or loosen the PDB"
	}
}
