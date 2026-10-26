// Package check predicts whether `kubectl drain` will finish on a node,
// before you cordon it, by reading pods and PodDisruptionBudgets.
package check

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	// Pinned maps "namespace/pod" to why its PersistentVolume cannot follow
	// it to another node (see PinnedVolumes). Nil disables the storage check.
	Pinned map[string]string
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
	// how many of those pods cannot come back elsewhere (node-pinned volumes)
	pinnedPDB := map[string]int{}
	matches := make([][]*policyv1.PodDisruptionBudget, len(pods))
	for i := range pods {
		if skipReason(&pods[i]) != "" {
			continue
		}
		for j := range pdbs {
			if covers(&pdbs[j], &pods[i]) {
				matches[i] = append(matches[i], &pdbs[j])
				perPDB[key(&pdbs[j])]++
				if _, ok := opt.Pinned[pods[i].Namespace+"/"+pods[i].Name]; ok {
					pinnedPDB[key(&pdbs[j])]++
				}
			}
		}
	}
	for i := range pods {
		r := pod(&pods[i], matches[i], perPDB, opt)
		r = pinned(&pods[i], matches[i], r, perPDB, pinnedPDB, opt)
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
	// Same order as the Eviction API (pkg/registry/core/pod/storage/eviction.go):
	// not-ready pods may skip the budget, then the budget itself is checked.
	if !ready(p) {
		pol := b.Spec.UnhealthyPodEvictionPolicy
		if pol != nil && *pol == policyv1.AlwaysAllow {
			r.Reason = "not ready, and PDB allows evicting unhealthy pods (AlwaysAllow)"
			return r
		}
		if healthy >= desired && desired > 0 {
			r.Reason = "not ready; budget is met so the eviction is admitted"
			return r
		}
	}
	if msg, failed := syncFailed(b); failed {
		// The controller pins disruptionsAllowed at 0 and never catches up
		// until the PDB or its pods change: every eviction gets 429 forever.
		r.Verdict = Blocked
		r.Reason = fmt.Sprintf("PDB %s cannot be computed by the disruption controller (SyncFailed: %s), so it allows 0 disruptions", key(b), msg)
		r.Fix = syncFailedFix
		return r
	}
	if b.Status.ObservedGeneration < b.Generation {
		r.Verdict = Worse(r.Verdict, Wait)
		r.Reason = "PDB status is stale (controller has not observed the latest spec)"
		return r
	}
	switch {
	case allowed < 0:
		r.Verdict = Blocked
		r.Reason = fmt.Sprintf("PDB %s reports %d allowed disruptions: the API server refuses the eviction (403) and drain exits with an error", key(b), allowed)
		r.Fix = "check the PDB status and the disruption controller (kube-controller-manager) logs"
	case allowed == 0:
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

// pinned adjusts a result for pods whose volume cannot follow them. Every
// such eviction spends one disruption of its PDB until the node comes back,
// because the replacement stays Pending. If the node holds more pinned pods
// of a budget than it allows, drain can never finish.
func pinned(p *corev1.Pod, m []*policyv1.PodDisruptionBudget, r PodResult, perPDB, pinnedPDB map[string]int, opt Options) PodResult {
	why, ok := opt.Pinned[p.Namespace+"/"+p.Name]
	if !ok || r.Verdict == Skip || r.Verdict == Blocked || len(m) > 1 {
		return r
	}
	if len(m) == 0 {
		if r.Verdict == OK {
			r.Reason = "evicts, but its replacement stays Pending until this node is back: " + why
			r.Fix = "expect downtime for this pod, or move its data to storage other nodes can attach"
		}
		return r
	}
	b := m[0]
	pc, allowed := int32(pinnedPDB[key(b)]), b.Status.DisruptionsAllowed
	switch {
	case pc > allowed:
		r.Verdict = Blocked
		r.Reason = fmt.Sprintf("%d pod(s) of PDB %s here have node-pinned volumes but it allows only %d disruption(s); their replacements can never become ready, so drain retries forever: %s",
			pc, key(b), allowed, why)
		r.Fix = "drain with the volume's node in mind: scale the workload so the budget covers the pinned pods, or move the data to storage other nodes can attach"
	case r.Verdict == OK || r.Verdict == Wait:
		r.Reason += "; its replacement stays Pending until this node is back (" + why + ")"
		r.Fix = fmt.Sprintf("after this drain PDB %s has %d fewer disruption(s) until the node returns, so draining another node of this workload may stall", key(b), pc)
		if int32(perPDB[key(b)]) > pc && pc == allowed {
			r.Verdict = Worse(r.Verdict, Wait)
			r.Reason = fmt.Sprintf("PDB %s allows %d disruption(s) and %d pinned pod(s) here use them all if evicted first, so the other replicas here may wait forever; ", key(b), allowed, pc) + r.Reason
		}
	}
	return r
}

const syncFailedFix = "maxUnavailable and percentage minAvailable need every selected pod to belong to a controller with a scale subresource " +
	"(Deployment, ReplicaSet, StatefulSet, or a CRD with /scale); use an integer minAvailable, add /scale to the CRD, or narrow the selector"

// syncFailed reports a DisruptionAllowed=False condition with reason
// SyncFailed, which the disruption controller sets when it cannot compute
// the budget (for example, pods owned by a CRD without a scale subresource).
func syncFailed(b *policyv1.PodDisruptionBudget) (string, bool) {
	c := meta.FindStatusCondition(b.Status.Conditions, policyv1.DisruptionAllowedCondition)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != policyv1.SyncFailedReason {
		return "", false
	}
	msg := c.Message
	if msg == "" {
		msg = "no message"
	}
	return msg, true
}

func fixFor(b *policyv1.PodDisruptionBudget) string {
	s := b.Status
	switch {
	case s.CurrentHealthy < s.DesiredHealthy:
		return fmt.Sprintf("%d pod(s) already unhealthy: fix them, or set spec.unhealthyPodEvictionPolicy: AlwaysAllow", s.DesiredHealthy-s.CurrentHealthy)
	case isZeroDisruptions(b.Spec.MaxUnavailable):
		return fmt.Sprintf("maxUnavailable is %s: no voluntary disruption is ever allowed; use maxUnavailable: 1", b.Spec.MaxUnavailable.String())
	case isFullAvailability(b.Spec.MinAvailable):
		return fmt.Sprintf("minAvailable is %s: no voluntary disruption is ever allowed; switch to maxUnavailable: 1", b.Spec.MinAvailable.String())
	case b.Spec.MinAvailable != nil && s.ExpectedPods > 0 && s.DesiredHealthy >= s.ExpectedPods:
		return fmt.Sprintf("minAvailable (%s) equals the replica count (%d): scale up first, or switch to maxUnavailable: 1", b.Spec.MinAvailable.String(), s.ExpectedPods)
	default:
		return "scale the workload up, or loosen the PDB"
	}
}

// isZeroDisruptions reports maxUnavailable values that forbid every voluntary eviction (0 or 0%).
func isZeroDisruptions(v *intstr.IntOrString) bool {
	if v == nil {
		return false
	}
	s := v.String()
	return s == "0" || s == "0%"
}

// isFullAvailability reports minAvailable values that forbid every voluntary eviction (100%).
func isFullAvailability(v *intstr.IntOrString) bool {
	return v != nil && v.String() == "100%"
}
