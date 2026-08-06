#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-corepack.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/corepack.env"

: "${COREPACK_VERSION:?missing COREPACK_VERSION}"
: "${COREPACK_PACKAGE_INTEGRITY:?missing COREPACK_PACKAGE_INTEGRITY}"
: "${COREPACK_PREFIX:?missing COREPACK_PREFIX}"
: "${PNPM_VERSION:?missing PNPM_VERSION}"
: "${YARN_VERSION:?missing YARN_VERSION}"
[[ $COREPACK_VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
[[ $PNPM_VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
[[ $YARN_VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
[[ $COREPACK_PREFIX == /usr/local ]]

NODE_BEFORE=$(node --version)
NPM_BEFORE=$(npm --version)
PNPM_PATH_BEFORE=$(command -v pnpm || true)
PNPM_VERSION_BEFORE=$(pnpm --version 2>/dev/null || true)
printf 'before node=%s npm=%s pnpm_path=%s pnpm=%s\n' "$NODE_BEFORE" "$NPM_BEFORE" "$PNPM_PATH_BEFORE" "$PNPM_VERSION_BEFORE"

npm install --global --prefix "$COREPACK_PREFIX" --ignore-scripts --force "corepack@$COREPACK_VERSION"
[[ $(node -p 'require("/usr/local/lib/node_modules/corepack/package.json").version') == "$COREPACK_VERSION" ]]
corepack enable --install-directory /usr/local/bin pnpm yarn
corepack install --global "pnpm@$PNPM_VERSION" "yarn@$YARN_VERSION"

[[ $(node --version) == "$NODE_BEFORE" ]]
[[ $(npm --version) == "$NPM_BEFORE" ]]
corepack --version
pnpm --version
yarn --version
[[ $(corepack --version) == "$COREPACK_VERSION" ]]
[[ $(pnpm --version) == "$PNPM_VERSION" ]]
[[ $(yarn --version) == "$YARN_VERSION" ]]
printf 'corepack=%s pnpm=%s yarn=%s node=%s npm=%s\n' "$(corepack --version)" "$(pnpm --version)" "$(yarn --version)" "$(node --version)" "$(npm --version)"
