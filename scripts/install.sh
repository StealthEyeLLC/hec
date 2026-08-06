#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install.sh: must run as root" >&2
  exit 1
fi

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/release-common.sh
source "$ROOT_DIR/scripts/release-common.sh"

[[ -n ${HEC_VERSION:-} ]] || { echo "install.sh: HEC_VERSION is required" >&2; exit 1; }
VERSION=$HEC_VERSION
hec_require_release_name "$VERSION" || exit 1

HEC_ROOT=/opt/hec
RELEASES_DIR=$HEC_ROOT/releases
RELEASE_DIR=$RELEASES_DIR/$VERSION
CURRENT_LINK=$HEC_ROOT/current
STAGING_DIR=
CURRENT_TEMP=

cleanup() {
  if [[ -n ${STAGING_DIR:-} ]]; then
    rm -rf -- "$STAGING_DIR"
  fi
  if [[ -n ${CURRENT_TEMP:-} ]]; then
    rm -f -- "$CURRENT_TEMP"
  fi
}
trap cleanup EXIT

if [[ -e $CURRENT_LINK || -L $CURRENT_LINK ]]; then
  [[ -L $CURRENT_LINK ]] || { echo "install.sh: current exists but is not a symlink: $CURRENT_LINK" >&2; exit 1; }
  CURRENT_EXISTED=1
else
  CURRENT_EXISTED=0
fi

SOURCE_COMMIT=$(git -C "$ROOT_DIR" rev-parse --verify HEAD 2>/dev/null) || { echo "install.sh: cannot resolve source commit" >&2; exit 1; }
hec_full_commit_is_valid "$SOURCE_COMMIT" || { echo "install.sh: source commit is not a full Git SHA" >&2; exit 1; }
if [[ -n ${SOURCE_DATE_EPOCH+x} ]]; then
  [[ $SOURCE_DATE_EPOCH =~ ^[0-9]+$ ]] || { echo "install.sh: SOURCE_DATE_EPOCH must be a nonnegative integer" >&2; exit 1; }
else
  SOURCE_DATE_EPOCH=$(git -C "$ROOT_DIR" show -s --format=%ct "$SOURCE_COMMIT" 2>/dev/null) || { echo "install.sh: cannot resolve commit timestamp" >&2; exit 1; }
fi
export SOURCE_DATE_EPOCH

date -u -d "@$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ >/dev/null 2>&1 || { echo "install.sh: SOURCE_DATE_EPOCH is outside the supported date range" >&2; exit 1; }

install -d -m 0755 \
  "$HEC_ROOT" \
  "$RELEASES_DIR" \
  /etc/hec \
  /etc/hec/skills \
  /root/.local/bin \
  /root/.local/share/mise/shims \
  /root/.cargo/bin \
  /srv/hec \
  /srv/hec/workspaces \
  /srv/hec/repositories \
  /srv/hec/deliveries \
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
chmod 0755 "$STAGING_DIR"
install -d -m 0755 \
  "$STAGING_DIR/bin" \
  "$STAGING_DIR/skills" \
  "$STAGING_DIR/capabilities" \
  "$STAGING_DIR/forge"

HEC_VERSION=$VERSION HEC_OUTPUT="$STAGING_DIR/bin/hec" "$ROOT_DIR/scripts/build.sh"

copy_release_tree() {
  local source=$1
  local destination=$2
  rsync -a \
    --exclude='.git/' \
    --exclude='dist/' \
    --exclude='bin/' \
    --exclude='.cache/' \
    --exclude='skill.zip' \
    --exclude='*.zip' \
    --exclude='*.tar' \
    --exclude='*.tar.gz' \
    --exclude='*.tgz' \
    --exclude='*.tmp' \
    "$source/" "$destination/"
}

copy_release_tree "$ROOT_DIR/skills" "$STAGING_DIR/skills"
copy_release_tree "$ROOT_DIR/capabilities" "$STAGING_DIR/capabilities"
copy_release_tree "$ROOT_DIR/forge" "$STAGING_DIR/forge"

[[ -f $STAGING_DIR/bin/hec && -x $STAGING_DIR/bin/hec && ! -L $STAGING_DIR/bin/hec ]] || { echo "install.sh: staged HEC binary is not a regular executable" >&2; exit 1; }
VERSION_TEXT=$("$STAGING_DIR/bin/hec" version) || { echo "install.sh: staged HEC binary version command failed" >&2; exit 1; }
STAGED_VERSION=$(awk '$1 == "HEC" {print $2; exit}' <<<"$VERSION_TEXT")
STAGED_PROTOCOL=$(awk '$1 == "Protocol" {print $2; exit}' <<<"$VERSION_TEXT")
STAGED_COMMIT=$(awk '$1 == "Commit" {print $2; exit}' <<<"$VERSION_TEXT")
[[ $STAGED_VERSION == "$VERSION" ]] || { echo "install.sh: staged binary version mismatch: $STAGED_VERSION" >&2; exit 1; }
[[ $STAGED_PROTOCOL == HEC1/1.0.0 ]] || { echo "install.sh: staged binary protocol mismatch: $STAGED_PROTOCOL" >&2; exit 1; }
[[ $STAGED_COMMIT == "$SOURCE_COMMIT" ]] || { echo "install.sh: staged binary commit mismatch: $STAGED_COMMIT" >&2; exit 1; }

find "$STAGING_DIR" ! -type d -exec touch -h -d "@$SOURCE_DATE_EPOCH" {} +
find "$STAGING_DIR" -depth -type d -exec touch -d "@$SOURCE_DATE_EPOCH" {} +

hec_install_staged_release "$STAGING_DIR" "$RELEASE_DIR"
if [[ ! -e $STAGING_DIR && ! -L $STAGING_DIR ]]; then
  STAGING_DIR=
fi

install -m 0644 "$ROOT_DIR/systemd/hec.service" /etc/systemd/system/hec.service
for env_file in /etc/hec/hec.env /etc/hec/tunnel.env; do
  if [[ ! -e $env_file ]]; then
    install -o root -g root -m 0600 /dev/null "$env_file"
  else
    chown root:root "$env_file"
    chmod 0600 "$env_file"
  fi
done

systemctl daemon-reload

CURRENT_STATE=preserved
SERVICE_STATE=not-restarted
if [[ $CURRENT_EXISTED -eq 0 ]]; then
  CURRENT_TEMP=$HEC_ROOT/.current.$$.${RANDOM}
  while [[ -e $CURRENT_TEMP || -L $CURRENT_TEMP ]]; do
    CURRENT_TEMP=$HEC_ROOT/.current.$$.${RANDOM}
  done
  ln -s "releases/$VERSION" "$CURRENT_TEMP"
  mv -Tf -- "$CURRENT_TEMP" "$CURRENT_LINK"
  CURRENT_TEMP=
  CURRENT_STATE=created

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
      SERVICE_STATE=restart-deferred
    else
      systemctl restart hec.service
      SERVICE_STATE=started
    fi
  else
    SERVICE_STATE=credentials-unavailable
  fi
fi

printf 'installed release: %s\n' "$RELEASE_DIR"
printf 'current link: %s\n' "$CURRENT_STATE"
printf 'service: %s\n' "$SERVICE_STATE"
if [[ $CURRENT_STATE == preserved ]]; then
  printf 'activate explicitly with: %s/scripts/cutover.sh %s\n' "$ROOT_DIR" "$VERSION"
fi
