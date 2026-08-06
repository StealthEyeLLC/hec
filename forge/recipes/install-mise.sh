#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-mise.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/mise.env"

: "${MISE_VERSION:?missing MISE_VERSION}"
: "${MISE_INSTALLER_URL:?missing MISE_INSTALLER_URL}"
: "${MISE_INSTALLER_SHA256:?missing MISE_INSTALLER_SHA256}"
: "${MISE_INSTALL_PATH:?missing MISE_INSTALL_PATH}"
[[ $MISE_VERSION =~ ^[0-9]{4}\.[0-9]+\.[0-9]+$ ]]
[[ $MISE_INSTALL_PATH == /root/.local/bin/mise ]]

TMP_DIR=$(mktemp -d /tmp/hec-mise.XXXXXX)
cleanup() { rm -rf -- "$TMP_DIR"; }
trap cleanup EXIT

curl -fsSL "$MISE_INSTALLER_URL" -o "$TMP_DIR/install.sh"
printf '%s  %s\n' "$MISE_INSTALLER_SHA256" "$TMP_DIR/install.sh" | sha256sum -c -
grep -q 'MISE_VERSION' "$TMP_DIR/install.sh"
grep -q 'MISE_INSTALL_PATH' "$TMP_DIR/install.sh"

MISE_VERSION="$MISE_VERSION" \
MISE_INSTALL_PATH="$MISE_INSTALL_PATH" \
MISE_INSTALL_SKIP_IF_EXISTS=1 \
MISE_INSTALL_HELP=0 \
MISE_QUIET=1 \
  sh "$TMP_DIR/install.sh"

install -d -m 0755 /root/.config/hec
cat > /root/.config/hec/forge-env.sh <<'ENVEOF'
# Managed by HEC Slice 7.
case ":${PATH}:" in
  *:/root/.local/bin:*) ;;
  *) PATH="/root/.local/bin:${PATH}" ;;
esac
case ":${PATH}:" in
  *:/root/.cargo/bin:*) ;;
  *) PATH="/root/.cargo/bin:${PATH}" ;;
esac
case ":${PATH}:" in
  *:/root/.local/share/mise/shims:*) ;;
  *) PATH="/root/.local/share/mise/shims:${PATH}" ;;
esac
export PATH
export MISE_TRUSTED_CONFIG_PATHS=/
ENVEOF
chmod 0644 /root/.config/hec/forge-env.sh

manage_shell_file() {
  local path=$1 tmp
  touch "$path"
  tmp=$(mktemp "${path}.XXXXXX")
  awk '
    BEGIN {skip=0}
    /^# BEGIN HEC FORGE ENV$/ {skip=1; next}
    /^# END HEC FORGE ENV$/ {skip=0; next}
    !skip {print}
  ' "$path" > "$tmp"
  while [[ -s $tmp ]] && [[ $(tail -c 1 "$tmp" | wc -l) -eq 0 ]]; do printf '\n' >> "$tmp"; done
  cat >> "$tmp" <<'BLOCK'
# BEGIN HEC FORGE ENV
if [ -r /root/.config/hec/forge-env.sh ]; then
  . /root/.config/hec/forge-env.sh
fi
# END HEC FORGE ENV
BLOCK
  cat "$tmp" > "$path"
  rm -f -- "$tmp"
}
manage_shell_file /root/.profile
manage_shell_file /root/.bashrc

install -d -m 0755 /etc/hec
HEC_ENV_TMP=$(mktemp /etc/hec/.hec.env.XXXXXX)
if [[ -e /etc/hec/hec.env ]]; then
  grep -v '^MISE_TRUSTED_CONFIG_PATHS=' /etc/hec/hec.env > "$HEC_ENV_TMP" || true
fi
printf 'MISE_TRUSTED_CONFIG_PATHS=/\n' >> "$HEC_ENV_TMP"
install -o root -g root -m 0600 "$HEC_ENV_TMP" /etc/hec/hec.env
rm -f -- "$HEC_ENV_TMP"

export PATH="/root/.local/share/mise/shims:/root/.local/bin:/root/.cargo/bin:$PATH"
export MISE_TRUSTED_CONFIG_PATHS=/
"$MISE_INSTALL_PATH" --version
"$MISE_INSTALL_PATH" doctor
"$MISE_INSTALL_PATH" settings
printf 'mise=%s trusted_config_paths=%s\n' "$("$MISE_INSTALL_PATH" --version | head -n1)" "$MISE_TRUSTED_CONFIG_PATHS"
