#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-data-tools.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/data-tools.env"

for name in DUCKDB_VERSION CSVKIT_VERSION; do
  [[ -n ${!name:-} ]] || { echo "missing $name" >&2; exit 1; }
done
export PATH=/root/.local/bin:/root/.local/share/mise/shims:/root/.cargo/bin:$PATH
export MISE_TRUSTED_CONFIG_PATHS=/ MISE_YES=1 MISE_PIN=1
command -v uv >/dev/null
CURRENT_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
RESERVE_BYTES=4294967296
ESTIMATE_BYTES=600000000
((CURRENT_BYTES - ESTIMATE_BYTES >= RESERVE_BYTES)) || { echo "data tool estimate would violate reserve" >&2; exit 1; }
mise use --global --pin --yes "duckdb@$DUCKDB_VERSION"
mise reshim
uv tool install --force "csvkit==$CSVKIT_VERSION"
duckdb --version
csvcut --version
AFTER_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
((AFTER_BYTES >= RESERVE_BYTES))
printf 'duckdb=%s csvkit=%s available_bytes=%s\n' "$(duckdb --version)" "$(csvcut --version)" "$AFTER_BYTES"
