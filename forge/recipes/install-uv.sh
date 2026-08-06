#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-uv.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/uv.env"

: "${UV_VERSION:?missing UV_VERSION}"
: "${UV_INSTALLER_URL:?missing UV_INSTALLER_URL}"
: "${UV_INSTALLER_SHA256:?missing UV_INSTALLER_SHA256}"
: "${UV_INSTALL_DIR:?missing UV_INSTALL_DIR}"
[[ $UV_VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
[[ $UV_INSTALLER_URL == *"/$UV_VERSION/install.sh" ]]
[[ $UV_INSTALL_DIR == /root/.local/bin ]]

TMP_DIR=$(mktemp -d /tmp/hec-uv.XXXXXX)
cleanup() { rm -rf -- "$TMP_DIR"; }
trap cleanup EXIT

curl -fsSL "$UV_INSTALLER_URL" -o "$TMP_DIR/install.sh"
printf '%s  %s\n' "$UV_INSTALLER_SHA256" "$TMP_DIR/install.sh" | sha256sum -c -
grep -q "APP_VERSION=\"$UV_VERSION\"" "$TMP_DIR/install.sh"
grep -q 'ARTIFACT_DOWNLOAD_URLS' "$TMP_DIR/install.sh"

UV_UNMANAGED_INSTALL="$UV_INSTALL_DIR" \
UV_NO_MODIFY_PATH=1 \
  sh "$TMP_DIR/install.sh"

export PATH="$UV_INSTALL_DIR:$PATH"
uv --version
uvx --version
uv python list
uv tool dir
uv tool dir --bin
printf 'uv=%s uvx=%s\n' "$(uv --version)" "$(uvx --version)"
