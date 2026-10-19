// drainready tells you, before you cordon anything, whether `kubectl drain`
// will finish on a node, and names the PodDisruptionBudget that would stall it.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Blackman99/drainready/check"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var version = "dev"

const usageTmpl = `%[1]s - will kubectl drain finish on this node?

Usage:
  %[1]s [flags] NODE [NODE...]
  %[1]s [flags] -l node-role.kubernetes.io/worker   (all matching nodes)
  %[1]s [flags] --all

Flags:
`

// progName is "kubectl drainready" when installed as a kubectl plugin
// (binary named kubectl-drainready, e.g. via krew), otherwise "drainready".
var progName = "drainready"

// displayName maps an argv[0] to the name users type.
func displayName(argv0 string) string {
	base := strings.TrimSuffix(filepath.Base(argv0), ".exe")
	if strings.HasPrefix(base, "kubectl-") {
		return "kubectl " + strings.ReplaceAll(strings.TrimPrefix(base, "kubectl-"), "-", " ")
	}
	return "drainready"
}

func main() {
	progName = displayName(os.Args[0])
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("drainready", flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.Usage = func() { fmt.Fprintf(errw, usageTmpl, progName); fs.PrintDefaults() }
	kubeconfig := fs.String("kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	kctx := fs.String("context", "", "kubeconfig context to use")
	selector := fs.String("l", "", "label selector for nodes")
	all := fs.Bool("all", false, "check every node")
	force := fs.Bool("force", false, "assume drain runs with --force")
	emptyDir := fs.Bool("delete-emptydir-data", false, "assume drain runs with --delete-emptydir-data")
	asJSON := fs.Bool("json", false, "print JSON")
	asMD := fs.Bool("markdown", false, "print a Markdown report (paste into a change ticket or PR)")
	verbose := fs.Bool("v", false, "also list pods that are OK or skipped")
	serverDry := fs.Bool("server-dry-run", false, "confirm each pod with a server-side dry-run eviction (nothing is evicted)")
	showVer := fs.Bool("version", false, "print version")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVer {
		fmt.Fprintln(out, "drainready", version)
		return 0
	}
	if *asJSON && *asMD {
		fmt.Fprintln(errw, "drainready: use --json or --markdown, not both")
		return 2
	}
	nodes := fs.Args()
	if len(nodes) == 0 && *selector == "" && !*all {
		fs.Usage()
		return 2
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if *kubeconfig != "" {
		rules.ExplicitPath = *kubeconfig
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: *kctx}).ClientConfig()
	if err != nil {
		fmt.Fprintln(errw, "drainready: kubeconfig:", err)
		return 2
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fmt.Fprintln(errw, "drainready:", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	results, err := Evaluate(ctx, cs, nodes, *selector, check.Options{Force: *force, DeleteEmptyDirData: *emptyDir}, *serverDry)
	if err != nil {
		fmt.Fprintln(errw, "drainready:", err)
		return 2
	}
	worst := check.OK
	for _, r := range results {
		worst = check.Worse(worst, r.Verdict)
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		enc.Encode(results)
	} else if *asMD {
		Markdown(out, results, *verbose, *serverDry, time.Now())
	} else {
		Print(out, results, *verbose)
	}
	switch worst {
	case check.Blocked:
		return 1
	case check.Flag:
		return 3
	}
	return 0
}

// Evaluate lists nodes, pods and PDBs and runs the check.
func Evaluate(ctx context.Context, cs kubernetes.Interface, nodes []string, selector string, opt check.Options, serverDry bool) ([]check.NodeResult, error) {
	if len(nodes) == 0 {
		nl, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return nil, err
		}
		for _, n := range nl.Items {
			nodes = append(nodes, n.Name)
		}
		if len(nodes) == 0 {
			return nil, fmt.Errorf("no nodes match %q", selector)
		}
	} else {
		for _, n := range nodes {
			if _, err := cs.CoreV1().Nodes().Get(ctx, n, metav1.GetOptions{}); err != nil {
				return nil, err
			}
		}
	}
	pdbl, err := cs.PolicyV1().PodDisruptionBudgets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list PodDisruptionBudgets: %w", err)
	}
	var out []check.NodeResult
	for _, n := range nodes {
		pl, err := cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + n})
		if err != nil {
			return nil, err
		}
		r := check.Node(n, pl.Items, pdbl.Items, opt)
		if serverDry {
			confirm(ctx, cs, &r)
		}
		out = append(out, r)
	}
	return out, nil
}

// confirm asks the API server to dry-run each eviction the check predicted
// would be admitted, catching races and admission webhooks.
func confirm(ctx context.Context, cs kubernetes.Interface, r *check.NodeResult) {
	r.Verdict = check.OK
	for i := range r.Pods {
		p := &r.Pods[i]
		if p.Verdict == check.OK {
			ev := &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: p.Pod, Namespace: p.Namespace},
				DeleteOptions: &metav1.DeleteOptions{DryRun: []string{metav1.DryRunAll}}}
			if err := cs.CoreV1().Pods(p.Namespace).EvictV1(ctx, ev); err != nil {
				p.Verdict, p.Reason, p.Fix = check.Blocked, "server dry-run eviction refused: "+err.Error(), "see the PDB or admission webhook named above"
			} else {
				p.Reason += " (confirmed by server dry-run)"
			}
		}
		r.Verdict = check.Worse(r.Verdict, p.Verdict)
	}
}

// Print renders a human report.
func Print(w io.Writer, results []check.NodeResult, verbose bool) {
	for _, r := range results {
		counts := map[check.Verdict]int{}
		for _, p := range r.Pods {
			counts[p.Verdict]++
		}
		fmt.Fprintf(w, "%s  %s   %d pods: %d ok, %d wait, %d need flag, %d blocked, %d skipped\n",
			mark(r.Verdict), r.Node, len(r.Pods), counts[check.OK], counts[check.Wait], counts[check.Flag], counts[check.Blocked], counts[check.Skip])
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, p := range r.Pods {
			if !verbose && (p.Verdict == check.OK || p.Verdict == check.Skip) {
				continue
			}
			fmt.Fprintf(tw, "   %s\t%s/%s\t%s\n", p.Verdict, p.Namespace, p.Pod, p.Reason)
			if p.Fix != "" {
				fmt.Fprintf(tw, "   \t\tfix: %s\n", p.Fix)
			}
		}
		tw.Flush()
	}
}

// Markdown renders a report for change tickets and PR comments.
func Markdown(w io.Writer, results []check.NodeResult, verbose, serverDry bool, at time.Time) {
	worst := check.OK
	for _, r := range results {
		worst = check.Worse(worst, r.Verdict)
	}
	fmt.Fprintf(w, "## Drain readiness: %s\n\n", mark(worst))
	how := "pods and PodDisruptionBudgets"
	if serverDry {
		how += ", confirmed with server dry-run evictions"
	}
	fmt.Fprintf(w, "Checked %d node(s) at %s with drainready %s (%s). Nothing was cordoned or evicted.\n",
		len(results), at.UTC().Format("2006-01-02 15:04 UTC"), version, how)
	for _, r := range results {
		counts := map[check.Verdict]int{}
		for _, p := range r.Pods {
			counts[p.Verdict]++
		}
		fmt.Fprintf(w, "\n### %s `%s`\n\n%d pods: %d ok, %d wait, %d need flag, %d blocked, %d skipped\n",
			mark(r.Verdict), r.Node, len(r.Pods), counts[check.OK], counts[check.Wait], counts[check.Flag], counts[check.Blocked], counts[check.Skip])
		var rows []check.PodResult
		for _, p := range r.Pods {
			if verbose || (p.Verdict != check.OK && p.Verdict != check.Skip) {
				rows = append(rows, p)
			}
		}
		if len(rows) == 0 {
			fmt.Fprintln(w, "\nNo pod on this node would stall the drain.")
			continue
		}
		fmt.Fprintln(w, "\n| Verdict | Pod | PDB | Reason | Fix |\n|---|---|---|---|---|")
		for _, p := range rows {
			pdbs := ""
			for i, b := range p.PDBs {
				if i > 0 {
					pdbs += ", "
				}
				pdbs += "`" + b + "`"
			}
			fmt.Fprintf(w, "| **%s** | `%s/%s` | %s | %s | %s |\n", p.Verdict, p.Namespace, p.Pod, pdbs, cell(p.Reason), cell(p.Fix))
		}
	}
}

// cell makes text safe inside a Markdown table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.Join(strings.Fields(s), " ")
}

func mark(v check.Verdict) string {
	switch v {
	case check.Blocked:
		return "BLOCKED"
	case check.Flag:
		return "NEEDS FLAG"
	case check.Wait:
		return "SLOW"
	}
	return strings.ToUpper("ready")
}
