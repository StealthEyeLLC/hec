#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "scripts/install.sh must run as root" >&2
  exit 1
fi

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
VERSION=${HEC_VERSION:-0.0.1}
RELEASES_DIR=/opt/hec/releases
RELEASE_DIR=$RELEASES_DIR/$VERSION
STAGING_DIR=
CURRENT_LINK=

cleanup() {
  if [[ -n $STAGING_DIR ]]; then
    rm -rf -- "$STAGING_DIR"
  fi
  if [[ -n $CURRENT_LINK ]]; then
    rm -f -- "$CURRENT_LINK"
  fi
}
trap cleanup EXIT

install -d -m 0755 \
  /opt/hec \
  "$RELEASES_DIR" \
  /etc/hec \
  /etc/hec/skills \
  /var/lib/hec \
  /var/cache/hec \
  /run/hec
install -d -m 0700 \
  /var/lib/hec/jobs \
  /var/lib/hec/job-keys \
  /var/lib/hec/terminals \
  /var/lib/hec/browser \
  /var/lib/hec/browser/profiles \
  /var/lib/hec/browser/output

STAGING_DIR=$(mktemp -d "$RELEASES_DIR/.${VERSION}.staging.XXXXXX")
install -d -m 0755 \
  "$STAGING_DIR/bin" \
  "$STAGING_DIR/skills" \
  "$STAGING_DIR/capabilities" \
  "$STAGING_DIR/forge"

SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(git -C "$ROOT_DIR" show -s --format=%ct HEAD)}
export SOURCE_DATE_EPOCH
HEC_VERSION=$VERSION HEC_OUTPUT="$STAGING_DIR/bin/hec" "$ROOT_DIR/scripts/build.sh"
cp -a "$ROOT_DIR/skills/." "$STAGING_DIR/skills/"
cp -a "$ROOT_DIR/capabilities/." "$STAGING_DIR/capabilities/"
cp -a "$ROOT_DIR/forge/." "$STAGING_DIR/forge/"

if [[ -e "$RELEASE_DIR" ]]; then
  if ! diff -qr "$STAGING_DIR" "$RELEASE_DIR" >/dev/null; then
    echo "release $RELEASE_DIR already exists with different content; select a new HEC_VERSION" >&2
    exit 1
  fi
else
  mv -- "$STAGING_DIR" "$RELEASE_DIR"
  STAGING_DIR=
fi

CURRENT_LINK=/opt/hec/.current.$$
ln -s "releases/$VERSION" "$CURRENT_LINK"
mv -Tf "$CURRENT_LINK" /opt/hec/current
CURRENT_LINK=

install -m 0644 "$ROOT_DIR/systemd/hec.service" /etc/systemd/system/hec.service
if [[ ! -e /etc/hec/tunnel.env ]]; then
  install -m 0600 /dev/null /etc/hec/tunnel.env
else
  chown root:root /etc/hec/tunnel.env
  chmod 0600 /etc/hec/tunnel.env
fi

systemctl daemon-reload

credentials_available() (
  set +u
  set -a
  # shellcheck disable=SC1091
  source /etc/hec/tunnel.env || return 1
  set +a
  [[ -n ${CONTROL_PLANE_TUNNEL_ID:-} ]] && \
    [[ -n ${CONTROL_PLANE_API_KEY:-${OPENAI_API_KEY:-}} ]]
)

if credentials_available; then
  systemctl enable hec.service
  if [[ ${HEC_SKIP_RESTART:-0} == 1 ]]; then
    echo "installed HEC $VERSION; hec.service restart was intentionally deferred"
  else
    systemctl restart hec.service
    echo "installed HEC $VERSION and started hec.service"
  fi
else
  echo "installed HEC $VERSION; hec.service was not started because tunnel credentials are not present"
fi
