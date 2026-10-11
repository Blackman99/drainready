#!/usr/bin/env bash
# End-to-end check against a real cluster (kind in CI).
set -euo pipefail
BIN=${BIN:-./drainready}
NODE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
kubectl create namespace shop
kubectl -n shop create deployment web --image=registry.k8s.io/pause:3.9 --replicas=2
kubectl -n shop rollout status deploy/web --timeout=120s
cat <<YAML | kubectl apply -f -
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata: {name: web, namespace: shop}
spec:
  minAvailable: 2
  selector: {matchLabels: {app: web}}
YAML
for i in $(seq 30); do
  [ "$(kubectl -n shop get pdb web -o jsonpath='{.status.expectedPods}')" = "2" ] && break; sleep 1; done

echo "--- case 1: minAvailable == replicas must be BLOCKED (exit 1)"
set +e; $BIN "$NODE" | tee out1.txt; rc=$?; set -e
[ $rc -eq 1 ] && grep -q "BLOCKED" out1.txt && grep -q "shop/web" out1.txt
echo "--- cross-check: real server dry-run drain agrees"
set +e; timeout 20 kubectl drain "$NODE" --dry-run=server --ignore-daemonsets --delete-emptydir-data --timeout=10s >drain.txt 2>&1; set -e
cat drain.txt | tail -5
grep -qi "disruption budget" drain.txt

echo "--- case 2: maxUnavailable 1 -> not blocked"
kubectl -n shop patch pdb web --type=json -p='[{"op":"remove","path":"/spec/minAvailable"},{"op":"add","path":"/spec/maxUnavailable","value":1}]'
for i in $(seq 30); do
  [ "$(kubectl -n shop get pdb web -o jsonpath='{.status.disruptionsAllowed}')" = "1" ] && break; sleep 1; done
$BIN --delete-emptydir-data --server-dry-run -v "$NODE" | tee out2.txt
grep -q "shop/web" out2.txt && ! grep -q "^BLOCKED" out2.txt
$BIN --json --delete-emptydir-data "$NODE" > out.json && grep -q '"verdict"' out.json
echo "e2e OK"
