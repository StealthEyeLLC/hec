#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-kubernetes-tools.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/kubernetes-tools.env"

for name in KUBECTL_VERSION HELM_VERSION KUSTOMIZE_VERSION K9S_VERSION STERN_VERSION KIND_VERSION K3D_VERSION CRICTL_VERSION ORAS_VERSION REGCTL_VERSION; do
  [[ -n ${!name:-} ]] || { echo "missing $name" >&2; exit 1; }
done
export PATH=/root/.local/bin:/root/.local/share/mise/shims:/root/.cargo/bin:$PATH
export MISE_TRUSTED_CONFIG_PATHS=/ MISE_YES=1 MISE_PIN=1
CURRENT_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
RESERVE_BYTES=4294967296
ESTIMATE_BYTES=900000000
((CURRENT_BYTES - ESTIMATE_BYTES >= RESERVE_BYTES)) || { echo "kubernetes client estimate would violate reserve" >&2; exit 1; }
mise use --global --pin --yes \
  "kubectl@$KUBECTL_VERSION" \
  "helm@$HELM_VERSION" \
  "kustomize@$KUSTOMIZE_VERSION" \
  "k9s@$K9S_VERSION" \
  "stern@$STERN_VERSION" \
  "kind@$KIND_VERSION" \
  "k3d@$K3D_VERSION" \
  "crictl@$CRICTL_VERSION" \
  "oras@$ORAS_VERSION" \
  "regctl@$REGCTL_VERSION"
mise reshim
kubectl version --client=true --output=yaml >/dev/null
helm version --short
kustomize version
k9s version --short 2>/dev/null || k9s version >/dev/null
stern --version
kind version
k3d version
crictl --version
oras version
regctl version
AFTER_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
((AFTER_BYTES >= RESERVE_BYTES))
printf 'kubernetes kubectl=%s helm=%s kustomize=%s available_bytes=%s\n' "$(kubectl version --client=true -o json | jq -r .clientVersion.gitVersion)" "$(helm version --short)" "$(kustomize version)" "$AFTER_BYTES"
