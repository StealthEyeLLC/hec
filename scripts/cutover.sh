#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: scripts/cutover.sh <version>"
}

if [[ ${1:-} == --help && $# -eq 1 ]]; then
  usage
  exit 0
fi
if [[ $# -ne 1 ]]; then
  usage >&2
  exit 2
fi
if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "cutover.sh: must run as root" >&2
  exit 1
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/release-common.sh
source "$SCRIPT_DIR/release-common.sh"

VERSION=$1
hec_require_release_name "$VERSION" || exit 1

HEC_ROOT=${HEC_ROOT:-/opt/hec}
HEC_SERVICE=${HEC_SERVICE:-hec.service}
HEC_SYSTEMCTL=${HEC_SYSTEMCTL:-/usr/bin/systemctl}
[[ $HEC_ROOT == /* ]] || { echo "cutover.sh: HEC_ROOT must be absolute" >&2; exit 1; }
[[ -x $HEC_SYSTEMCTL ]] || { echo "cutover.sh: systemctl executable is missing: $HEC_SYSTEMCTL" >&2; exit 1; }

RELEASES_DIR=$HEC_ROOT/releases
CURRENT_LINK=$HEC_ROOT/current
RELEASE_DIR=$RELEASES_DIR/$VERSION
BINARY=$RELEASE_DIR/bin/hec

[[ -d $RELEASE_DIR && ! -L $RELEASE_DIR ]] || { echo "cutover.sh: release is missing or not a real directory: $RELEASE_DIR" >&2; exit 1; }
[[ -f $BINARY && -x $BINARY && ! -L $BINARY ]] || { echo "cutover.sh: release binary is missing, linked, or not executable: $BINARY" >&2; exit 1; }
for required_dir in skills capabilities forge; do
  path=$RELEASE_DIR/$required_dir
  [[ -d $path && ! -L $path ]] || { echo "cutover.sh: required release directory is missing or linked: $path" >&2; exit 1; }
done

VERSION_TEXT=$("$BINARY" version) || { echo "cutover.sh: target binary version command failed" >&2; exit 1; }
REPORTED_VERSION=$(awk '$1 == "HEC" {print $2; exit}' <<<"$VERSION_TEXT")
REPORTED_PROTOCOL=$(awk '$1 == "Protocol" {print $2; exit}' <<<"$VERSION_TEXT")
REPORTED_COMMIT=$(awk '$1 == "Commit" {print $2; exit}' <<<"$VERSION_TEXT")
[[ $REPORTED_VERSION == "$VERSION" ]] || { echo "cutover.sh: target reports version $REPORTED_VERSION, expected $VERSION" >&2; exit 1; }
[[ $REPORTED_PROTOCOL == HEC1/1.0.0 ]] || { echo "cutover.sh: target reports protocol $REPORTED_PROTOCOL, expected HEC1/1.0.0" >&2; exit 1; }
hec_full_commit_is_valid "$REPORTED_COMMIT" || { echo "cutover.sh: target build commit is not a full Git SHA" >&2; exit 1; }

if [[ -e $CURRENT_LINK || -L $CURRENT_LINK ]]; then
  [[ -L $CURRENT_LINK ]] || { echo "cutover.sh: current exists but is not a symlink: $CURRENT_LINK" >&2; exit 1; }
  PREVIOUS_TARGET=$(readlink -- "$CURRENT_LINK")
else
  PREVIOUS_TARGET='(absent)'
fi

TEMP_LINK=$HEC_ROOT/.current.$$.${RANDOM}
while [[ -e $TEMP_LINK || -L $TEMP_LINK ]]; do
  TEMP_LINK=$HEC_ROOT/.current.$$.${RANDOM}
done
cleanup() {
  if [[ -n ${TEMP_LINK:-} ]]; then
    rm -f -- "$TEMP_LINK"
  fi
}
trap cleanup EXIT

ln -s "releases/$VERSION" "$TEMP_LINK"
mv -Tf -- "$TEMP_LINK" "$CURRENT_LINK"
TEMP_LINK=

if ! "$HEC_SYSTEMCTL" restart "$HEC_SERVICE"; then
  echo "cutover.sh: restart failed; current remains selected at releases/$VERSION" >&2
  exit 1
fi
if ! "$HEC_SYSTEMCTL" is-active --quiet "$HEC_SERVICE"; then
  echo "cutover.sh: service is not active after restart; current remains selected at releases/$VERSION" >&2
  exit 1
fi

printf 'cutover complete: previous=%s selected=%s commit=%s service=active\n' \
  "$PREVIOUS_TARGET" "$RELEASE_DIR" "$REPORTED_COMMIT"
