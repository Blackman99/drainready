# Roadmap

## v0.1 (done)
- Per-node verdicts: OK / WAIT / FLAG / BLOCKED / SKIP with reason and fix
- Overlapping PDBs, minAvailable == replicas, maxUnavailable 0, unhealthy pod policy
- `--server-dry-run` confirmation, `--json`, label selector / `--all`
- Single static binaries via GoReleaser; e2e tested on kind in CI

## v0.2 (next — week of 2026-10-12)
Priority order from weekly review (0 traffic/stars after day 1; pain still live in ops posts):
1. **Krew plugin** (`kubectl drainready`) + Homebrew tap — primary discovery for kubectl users. Krew packaging merged 2026-10-14 (#1: `kubectl-drainready` archives, `.krew.yaml`, docs); remaining: tag a release with the krew archives, submit to krew-index (needs user approval), Homebrew tap
2. **Structural zero-budget fixes** — clear messages for `maxUnavailable: 0%` and `minAvailable: 100%` (shipped 2026-10-12); keep tightening fix copy vs undrained-style audits without becoming a cluster-wide linter
3. **Capacity hint** — will WAIT pods fit on remaining nodes (requests vs allocatable, taints)
4. Rolling "node group" mode: simulate draining nodes one by one

## v0.3
- **SyncFailed budgets** (shipped 2026-10-19): PDBs whose controller cannot compute them (e.g. maxUnavailable over operator pods without `/scale`) are BLOCKED with the controller's message, even though they look "stale"; e2e reproduces it on kind. Eviction-order parity: unhealthy IfHealthyBudget needs desiredHealthy > 0; negative disruptionsAllowed is a 403, not a hang.
- **Markdown report** `--markdown` for change tickets / PR comments (shipped 2026-10-19)
- **Node-pinned volumes** (shipped 2026-10-26): PVs whose node affinity no other schedulable node satisfies (local volumes, zonal disks with no free node in the zone). More pinned pods than the PDB allows is BLOCKED; within budget it warns that the next drain of that workload may stall. e2e reproduces it with a local PV on kind.

## Growth notes (2026-10-26 review)
- Week 2 after v0.2.0: no issues, PRs or stars; 8 views / 5 unique, 198 clones / 89 unique (mostly CI and mirrors), no referrers. awesome-list PRs still unreviewed. Discovery is still the bottleneck; v0.3.0 release and krew-index submission are waiting for approval.
- Picked node-pinned volumes this week: it is the one storage case that turns a slow drain into a permanent one, and it stays within "what blocks a drain" (GOAL.md).
- Next: capacity hint for WAIT pods (requests vs allocatable, taints), limited to replacements a drain is actually waiting on.

## Growth notes (2026-10-19 review)
- Week 1 after v0.2.0: 0 stars, ~4 views, 127 clones (mostly CI/bots), first kubectl-drainready archive downloads. Discovery is the bottleneck, so krew-index submission (awaiting approval) stays the top outreach item.
- Ecosystem: drain-stuck-on-PDB posts keep appearing (X, blogs on minAvailable == replicas with co-located replicas); kubernetes/kubernetes#141607 (drain retry loop on recreated pods) still open. A Sep 2026 thread pointed at SyncFailed PDBs as an overlooked cause, which led to this week's fix.
- Next: release v0.3.0 with SyncFailed + markdown (needs approval), then capacity hint for WAIT pods.

## Growth notes (2026-10-12 review)
- Niche vs competitors: undrained audits PDB objects cluster-wide; kubepreflight/upgrade are broad upgrade suites. drainready stays **per-node drain prediction that names the blocking PDB** (GOAL.md).
- Ecosystem: ops still hit PDB=0 drain hangs (e.g. X post 2026-10-10); kubectl PR #138555 fixed a related StatefulSet retry loop (merged earlier in 2026).
- Outreach (drafts need user approval): reply to live drain-stuck posts; awesome-kubectl-plugins + awesome-kubernetes PRs once Krew lands or as plain-binary entries.
