#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-infrastructure-tools.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/infrastructure-tools.env"

for name in OPENTOFU_VERSION TERRAGRUNT_VERSION PACKER_VERSION; do
  [[ -n ${!name:-} ]] || { echo "missing $name" >&2; exit 1; }
done
export PATH=/root/.local/bin:/root/.local/share/mise/shims:/root/.cargo/bin:$PATH
export MISE_TRUSTED_CONFIG_PATHS=/ MISE_YES=1 MISE_PIN=1
CURRENT_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
RESERVE_BYTES=4294967296
ESTIMATE_BYTES=600000000
((CURRENT_BYTES - ESTIMATE_BYTES >= RESERVE_BYTES)) || { echo "infrastructure tool estimate would violate reserve" >&2; exit 1; }
mise use --global --pin --yes "opentofu@$OPENTOFU_VERSION" "terragrunt@$TERRAGRUNT_VERSION" "packer@$PACKER_VERSION"
mise reshim
tofu version
terragrunt --version
packer version
AFTER_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
((AFTER_BYTES >= RESERVE_BYTES))
printf 'opentofu=%s terragrunt=%s packer=%s available_bytes=%s\n' "$(tofu version | head -n1)" "$(terragrunt --version | head -n1)" "$(packer version | head -n1)" "$AFTER_BYTES"
