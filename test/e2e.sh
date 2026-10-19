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
echo "--- case 3: maxUnavailable over pods owned by a kind without /scale -> SyncFailed, BLOCKED"
kubectl create namespace ops
kubectl -n ops create configmap widget
CM_UID=$(kubectl -n ops get configmap widget -o jsonpath='{.metadata.uid}')
cat <<YAML | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: widget-0
  namespace: ops
  labels: {app: widget}
  ownerReferences: [{apiVersion: v1, kind: ConfigMap, name: widget, uid: "$CM_UID", controller: true}]
spec:
  nodeName: $NODE
  containers: [{name: c, image: registry.k8s.io/pause:3.9}]
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata: {name: widget, namespace: ops}
spec:
  maxUnavailable: 1
  selector: {matchLabels: {app: widget}}
YAML
kubectl -n ops wait pod/widget-0 --for=condition=Ready --timeout=120s
for i in $(seq 60); do
  [ "$(kubectl -n ops get pdb widget -o jsonpath='{.status.conditions[?(@.type=="DisruptionAllowed")].reason}')" = "SyncFailed" ] && break; sleep 1; done
kubectl -n ops get pdb widget -o jsonpath='{.status}'; echo
set +e; $BIN --delete-emptydir-data "$NODE" | tee out3.txt; rc=$?; set -e
[ $rc -eq 1 ] && grep -q "ops/widget-0" out3.txt && grep -q "SyncFailed" out3.txt
echo "--- cross-check: real server dry-run drain of that pod is refused"
# A never-synced SyncFailed PDB answers 429 with Retry-After, which client-go
# retries silently: drain prints "evicting pod" and then nothing, ignoring
# --timeout. So assert that it did not finish rather than grep a message.
set +e; timeout 20 kubectl drain "$NODE" --dry-run=server --pod-selector=app=widget --ignore-daemonsets --delete-emptydir-data --timeout=10s >drain3.txt 2>&1; drc=$?; set -e
tail -5 drain3.txt; echo "drain exit: $drc"
[ $drc -ne 0 ] && ! grep -q "widget-0 evicted" drain3.txt
echo "--- markdown report names the blocked pod"
set +e; $BIN --markdown --delete-emptydir-data "$NODE" > out.md; set -e
cat out.md
grep -q "^## Drain readiness: BLOCKED" out.md && grep -q '`ops/widget-0`' out.md
echo "e2e OK"
