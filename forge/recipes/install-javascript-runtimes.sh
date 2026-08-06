#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-javascript-runtimes.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/javascript.env"

for name in BUN_VERSION DENO_VERSION; do
  [[ ${!name:-} =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid $name" >&2; exit 1; }
done
export PATH=/root/.local/bin:/root/.local/share/mise/shims:/root/.cargo/bin:$PATH
export MISE_TRUSTED_CONFIG_PATHS=/ MISE_YES=1 MISE_PIN=1
command -v mise >/dev/null
CURRENT_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
RESERVE_BYTES=4294967296
ESTIMATE_BYTES=768000000
((CURRENT_BYTES - ESTIMATE_BYTES >= RESERVE_BYTES)) || { echo "javascript runtime estimate would violate reserve" >&2; exit 1; }
mise use --global --pin --yes "bun@$BUN_VERSION" "deno@$DENO_VERSION"
mise reshim
[[ $(bun --version) == "$BUN_VERSION" ]]
[[ $(deno --version | awk 'NR==1 {print $2}') == "$DENO_VERSION" ]]
bun -e 'console.log("bun-ok")' | grep -qx bun-ok
deno eval 'console.log("deno-ok")' | grep -qx deno-ok
AFTER_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
((AFTER_BYTES >= RESERVE_BYTES))
printf 'bun=%s deno=%s available_bytes=%s\n' "$(bun --version)" "$(deno --version | awk 'NR==1 {print $2}')" "$AFTER_BYTES"
