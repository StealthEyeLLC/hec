#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-apt-base.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
PACKAGE_FILE="$ROOT_DIR/forge/apt/base.txt"
[[ -s $PACKAGE_FILE ]]
mapfile -t PACKAGES < <(sed -e 's/[[:space:]]*#.*$//' -e '/^[[:space:]]*$/d' "$PACKAGE_FILE")
((${#PACKAGES[@]} > 0))

RESERVE_BYTES=4294967296
CURRENT_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
SIM_FILE=$(mktemp /tmp/hec-apt-base-sim.XXXXXX)
EST_FILE=$(mktemp /tmp/hec-apt-base-est.XXXXXX)
cleanup() { rm -f -- "$SIM_FILE" "$EST_FILE"; }
trap cleanup EXIT

export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l LC_ALL=C
apt-get update
apt-get -s --no-install-recommends install "${PACKAGES[@]}" > "$SIM_FILE" 2>&1
if grep -qE '^(Remv |The following packages will be REMOVED:)' "$SIM_FILE"; then
  cat "$SIM_FILE" >&2
  echo "base apt simulation proposed removals" >&2
  exit 1
fi
apt-get --print-uris -y --no-install-recommends install "${PACKAGES[@]}" > "$EST_FILE" 2>&1

size_bytes() {
  local label=$1 line amount unit suffix
  line=$(grep -m1 "$label" "$EST_FILE" || true)
  [[ -n $line ]] || { echo 0; return; }
  if [[ $label == 'Need to get' ]]; then
    read -r amount unit < <(sed -E 's/^Need to get ([0-9.]+) ([kMGT]?B).*/\1 \2/' <<<"$line")
  else
    read -r amount unit < <(sed -E 's/^After this operation, ([0-9.]+) ([kMGT]?B).*/\1 \2/' <<<"$line")
  fi
  suffix=${unit%B}
  [[ -n $suffix ]] || suffix=1
  numfmt --from=si "${amount}${suffix}"
}
DOWNLOAD_BYTES=$(size_bytes 'Need to get')
INSTALLED_BYTES=$(size_bytes 'After this operation')
PROJECTED_BYTES=$((CURRENT_BYTES - DOWNLOAD_BYTES - INSTALLED_BYTES))
printf 'stage=apt-base current_available_bytes=%s projected_download_bytes=%s projected_installed_bytes=%s projected_available_bytes=%s reserve_bytes=%s\n' "$CURRENT_BYTES" "$DOWNLOAD_BYTES" "$INSTALLED_BYTES" "$PROJECTED_BYTES" "$RESERVE_BYTES"
if ((PROJECTED_BYTES < RESERVE_BYTES)); then
  echo "base apt layer cannot fit without violating the disk reserve; no installation performed" >&2
  exit 1
fi
apt-get install -y --no-install-recommends "${PACKAGES[@]}"
for command in zsh fish tree jq rg fzf parallel git nvim tmux clang cmake gdb strace htop nmap tcpdump openssl age; do
  command -v "$command" >/dev/null
 done
printf 'apt-base packages=%s available_bytes=%s\n' "${#PACKAGES[@]}" "$(df -B1 --output=avail / | tail -n1 | tr -d ' ')"
