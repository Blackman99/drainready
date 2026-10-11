# Goal

**Tell you, before you cordon anything, whether `kubectl drain` will finish on a node, and name the PodDisruptionBudget (or flag) that would stall it.**

In scope: read-only prediction of drain/eviction outcomes for nodes (PDBs, unmanaged pods, emptyDir, DaemonSets, unhealthy-pod policy, server dry-run confirmation), clear fixes, machine-readable output for upgrade scripts.

Out of scope: actually draining/cordoning/editing PDBs, general cluster linting, autoscaler simulation, scheduling capacity planning beyond what blocks a drain.
