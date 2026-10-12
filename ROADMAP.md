# Roadmap

## v0.1 (done)
- Per-node verdicts: OK / WAIT / FLAG / BLOCKED / SKIP with reason and fix
- Overlapping PDBs, minAvailable == replicas, maxUnavailable 0, unhealthy pod policy
- `--server-dry-run` confirmation, `--json`, label selector / `--all`
- Single static binaries via GoReleaser; e2e tested on kind in CI

## v0.2 (next — week of 2026-10-12)
Priority order from weekly review (0 traffic/stars after day 1; pain still live in ops posts):
1. **Krew plugin** (`kubectl drainready`) + Homebrew tap — primary discovery for kubectl users
2. **Structural zero-budget fixes** — clear messages for `maxUnavailable: 0%` and `minAvailable: 100%` (shipped 2026-10-12); keep tightening fix copy vs undrained-style audits without becoming a cluster-wide linter
3. **Capacity hint** — will WAIT pods fit on remaining nodes (requests vs allocatable, taints)
4. Rolling "node group" mode: simulate draining nodes one by one

## v0.3
- Detect PVC zone pinning (pods that cannot reschedule anywhere)
- Markdown report for change tickets

## Growth notes (2026-10-12 review)
- Niche vs competitors: undrained audits PDB objects cluster-wide; kubepreflight/upgrade are broad upgrade suites. drainready stays **per-node drain prediction that names the blocking PDB** (GOAL.md).
- Ecosystem: ops still hit PDB=0 drain hangs (e.g. X post 2026-10-10); kubectl PR #138555 fixed a related StatefulSet retry loop (merged earlier in 2026).
- Outreach (drafts need user approval): reply to live drain-stuck posts; awesome-kubectl-plugins + awesome-kubernetes PRs once Krew lands or as plain-binary entries.
