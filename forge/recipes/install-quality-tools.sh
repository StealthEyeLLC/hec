#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-quality-tools.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/quality-tools.env"

for name in ACTIONLINT_VERSION HADOLINT_VERSION EDITORCONFIG_CHECKER_VERSION AST_GREP_VERSION TREE_SITTER_VERSION WEBSOCAT_VERSION GRPCURL_VERSION TOKEI_VERSION SEMGREP_VERSION; do
  [[ -n ${!name:-} ]] || { echo "missing $name" >&2; exit 1; }
done
export PATH=/root/.local/bin:/root/.local/share/mise/shims:/root/.cargo/bin:$PATH
export MISE_TRUSTED_CONFIG_PATHS=/ MISE_YES=1 MISE_PIN=1
command -v mise >/dev/null
command -v uv >/dev/null
CURRENT_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
RESERVE_BYTES=4294967296
ESTIMATE_BYTES=900000000
((CURRENT_BYTES - ESTIMATE_BYTES >= RESERVE_BYTES)) || { echo "quality tool estimate would violate reserve" >&2; exit 1; }
# websocat, grpcurl, and tokei are installed here because Ubuntu noble does not provide their requested package names.
mise use --global --pin --yes \
  "actionlint@$ACTIONLINT_VERSION" \
  "hadolint@$HADOLINT_VERSION" \
  "editorconfig-checker@$EDITORCONFIG_CHECKER_VERSION" \
  "ast-grep@$AST_GREP_VERSION" \
  "tree-sitter@$TREE_SITTER_VERSION" \
  "websocat@$WEBSOCAT_VERSION" \
  "grpcurl@$GRPCURL_VERSION" \
  "tokei@$TOKEI_VERSION"
mise reshim
uv tool install --force "semgrep==$SEMGREP_VERSION"
actionlint -version
hadolint --version
ec --version
sg --version
tree-sitter --version
websocat --version
grpcurl -version
tokei --version
semgrep --version
AFTER_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
((AFTER_BYTES >= RESERVE_BYTES))
printf 'quality actionlint=%s hadolint=%s ast-grep=%s semgrep=%s available_bytes=%s\n' "$(actionlint -version 2>&1 | head -n1)" "$(hadolint --version | head -n1)" "$(sg --version | head -n1)" "$(semgrep --version | head -n1)" "$AFTER_BYTES"
