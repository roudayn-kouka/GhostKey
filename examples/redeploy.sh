#!/usr/bin/env bash
# Quick redeploy: rebuild ghostkey image and restart controller only.
# Usage: make generate-ebpf && ./examples/redeploy.sh
#
# Skips: Lima VM setup, K3s install, demo image builds, demo redeployment.
# Only rebuilds the ghostkey controller image and restarts it.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
KUBECONFIG_PATH="/tmp/ghostkey-k3s.yaml"
LIMA_INSTANCE="ghostkey"

export KUBECONFIG="$KUBECONFIG_PATH"

echo "==> Building ghostkey image..."
docker build -t ghostkey:latest "$ROOT_DIR"

echo "==> Importing into K3s..."
docker save ghostkey:latest | limactl shell "${LIMA_INSTANCE}" -- sudo k3s ctr images import -

echo "==> Restarting controller..."
kubectl rollout restart daemonset/ghostkey-controller -n ghostkey-system

echo "==> Waiting for rollout..."
kubectl rollout status daemonset/ghostkey-controller -n ghostkey-system --timeout=60s

echo "==> Controller logs:"
sleep 3
kubectl logs -n ghostkey-system -l app.kubernetes.io/component=controller --tail=20

echo ""
echo "Done. Monitor with:"
echo "  kubectl logs -f -n ghostkey-system -l app.kubernetes.io/component=controller"
