#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-security-tools.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/security-tools.env"

for name in TRIVY_VERSION SYFT_VERSION GRYPE_VERSION COSIGN_VERSION OSV_SCANNER_VERSION GITLEAKS_VERSION; do
  [[ -n ${!name:-} ]] || { echo "missing $name" >&2; exit 1; }
done
export PATH=/root/.local/bin:/root/.local/share/mise/shims:/root/.cargo/bin:$PATH
export MISE_TRUSTED_CONFIG_PATHS=/ MISE_YES=1 MISE_PIN=1
CURRENT_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
RESERVE_BYTES=4294967296
ESTIMATE_BYTES=900000000
((CURRENT_BYTES - ESTIMATE_BYTES >= RESERVE_BYTES)) || { echo "security tool estimate would violate reserve" >&2; exit 1; }
mise use --global --pin --yes \
  "trivy@$TRIVY_VERSION" \
  "syft@$SYFT_VERSION" \
  "grype@$GRYPE_VERSION" \
  "cosign@$COSIGN_VERSION" \
  "osv-scanner@$OSV_SCANNER_VERSION" \
  "gitleaks@$GITLEAKS_VERSION"
mise reshim
trivy --version
syft version
grype version
cosign version
osv-scanner --version
gitleaks version
AFTER_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
((AFTER_BYTES >= RESERVE_BYTES))
printf 'security trivy=%s syft=%s grype=%s cosign=%s osv-scanner=%s gitleaks=%s available_bytes=%s\n' "$(trivy --version | head -n1)" "$(syft version | head -n1)" "$(grype version | head -n1)" "$(cosign version | head -n1)" "$(osv-scanner --version | head -n1)" "$(gitleaks version | head -n1)" "$AFTER_BYTES"
