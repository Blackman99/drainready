# Contributing

1. Read [GOAL.md](GOAL.md); features outside it will be declined.
2. `go test ./...` must pass; `gofmt` clean. Logic lives in `check/` and is unit-tested without a cluster.
3. `test/e2e.sh` runs against a kind cluster in CI; add a case there for new behaviour that the API server decides.
4. Keep the binary dependency-light (client-go only).
