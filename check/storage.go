package check

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// PinnedVolumes finds pods on node whose PersistentVolume can only be used on
// nodes that will not take the replacement: the node being drained itself, or
// nodes that are cordoned. A local volume (kubernetes.io/hostname affinity) or
// a zonal disk whose zone has no other schedulable node is the usual cause.
// The result maps "namespace/pod" to a human explanation and feeds
// Options.Pinned.
func PinnedVolumes(node string, pods []corev1.Pod, pvcs []corev1.PersistentVolumeClaim, pvs []corev1.PersistentVolume, nodes []corev1.Node) map[string]string {
	claims := map[string]*corev1.PersistentVolumeClaim{}
	for i := range pvcs {
		claims[pvcs[i].Namespace+"/"+pvcs[i].Name] = &pvcs[i]
	}
	vols := map[string]*corev1.PersistentVolume{}
	for i := range pvs {
		vols[pvs[i].Name] = &pvs[i]
	}
	var others []*corev1.Node
	for i := range nodes {
		n := &nodes[i]
		if n.Name != node && !n.Spec.Unschedulable {
			others = append(others, n)
		}
	}
	out := map[string]string{}
	for i := range pods {
		p := &pods[i]
		if skipReason(p) != "" {
			continue
		}
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim == nil {
				continue
			}
			c := claims[p.Namespace+"/"+v.PersistentVolumeClaim.ClaimName]
			if c == nil || c.Spec.VolumeName == "" {
				continue
			}
			pv := vols[c.Spec.VolumeName]
			if pv == nil || pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
				continue
			}
			req := pv.Spec.NodeAffinity.Required
			fits := false
			for _, n := range others {
				if matchesNode(req, n) {
					fits = true
					break
				}
			}
			if !fits {
				out[p.Namespace+"/"+p.Name] = fmt.Sprintf("PersistentVolume %s (claim %s) only attaches where %s, and no other schedulable node matches",
					pv.Name, c.Name, describe(req))
				break
			}
		}
	}
	return out
}

// matchesNode reports whether a node satisfies any of the selector's terms.
func matchesNode(sel *corev1.NodeSelector, n *corev1.Node) bool {
	for _, t := range sel.NodeSelectorTerms {
		if len(t.MatchExpressions) == 0 && len(t.MatchFields) == 0 {
			continue // an empty term matches nothing
		}
		ok := true
		for _, r := range t.MatchExpressions {
			v, has := n.Labels[r.Key]
			ok = ok && matchReq(r, v, has)
		}
		for _, r := range t.MatchFields {
			ok = ok && r.Key == "metadata.name" && matchReq(r, n.Name, true)
		}
		if ok {
			return true
		}
	}
	return false
}

func matchReq(r corev1.NodeSelectorRequirement, v string, has bool) bool {
	in := func() bool {
		for _, x := range r.Values {
			if x == v {
				return true
			}
		}
		return false
	}
	switch r.Operator {
	case corev1.NodeSelectorOpIn:
		return has && in()
	case corev1.NodeSelectorOpNotIn:
		return !has || !in()
	case corev1.NodeSelectorOpExists:
		return has
	case corev1.NodeSelectorOpDoesNotExist:
		return !has
	case corev1.NodeSelectorOpGt, corev1.NodeSelectorOpLt:
		if !has || len(r.Values) != 1 {
			return false
		}
		a, err1 := strconv.ParseInt(v, 10, 64)
		b, err2 := strconv.ParseInt(r.Values[0], 10, 64)
		if err1 != nil || err2 != nil {
			return false
		}
		if r.Operator == corev1.NodeSelectorOpGt {
			return a > b
		}
		return a < b
	}
	return false
}

// describe renders the first term of a node selector, e.g.
// "kubernetes.io/hostname in (worker-2)".
func describe(sel *corev1.NodeSelector) string {
	if len(sel.NodeSelectorTerms) == 0 {
		return "nothing"
	}
	var parts []string
	t := sel.NodeSelectorTerms[0]
	for _, r := range append(append([]corev1.NodeSelectorRequirement{}, t.MatchExpressions...), t.MatchFields...) {
		s := r.Key + " " + strings.ToLower(string(r.Operator))
		if len(r.Values) > 0 {
			s += " (" + strings.Join(r.Values, ", ") + ")"
		}
		parts = append(parts, s)
	}
	d := strings.Join(parts, " and ")
	if len(sel.NodeSelectorTerms) > 1 {
		d += fmt.Sprintf(" (or %d other term(s))", len(sel.NodeSelectorTerms)-1)
	}
	return d
}
