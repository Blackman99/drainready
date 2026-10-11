# Roadmap

## v0.1 (done)
- Per-node verdicts: OK / WAIT / FLAG / BLOCKED / SKIP with reason and fix
- Overlapping PDBs, minAvailable == replicas, maxUnavailable 0, unhealthy pod policy
- `--server-dry-run` confirmation, `--json`, label selector / `--all`
- Single static binaries via GoReleaser; e2e tested on kind in CI

## v0.2
- Krew plugin (`kubectl drainready`) and Homebrew tap
- Capacity hint: will WAIT pods fit on the remaining nodes (requests vs allocatable, taints)
- Rolling "node group" mode: simulate draining nodes one by one

## v0.3
- Detect PVC zone pinning (pods that cannot reschedule anywhere)
- Markdown report for change tickets
