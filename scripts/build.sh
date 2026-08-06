#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# HEC_TEST_GOROOT is an isolated test seam; production uses the pinned path.
GOROOT=${HEC_TEST_GOROOT:-/opt/hec/toolchains/go/1.26.2}
HOME=${HOME:-/root}
GOPATH=${GOPATH:-/root/go}
GOMODCACHE=${GOMODCACHE:-$GOPATH/pkg/mod}
GOCACHE=${GOCACHE:-/root/.cache/go-build}
PATH="$GOROOT/bin:${PATH:-/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin}"
export GOROOT HOME GOPATH GOMODCACHE GOCACHE PATH

# shellcheck source=scripts/release-common.sh
source "$ROOT_DIR/scripts/release-common.sh"

fail() {
  echo "build.sh: $*" >&2
  exit 1
}

[[ -n ${HEC_VERSION:-} ]] || fail "HEC_VERSION is required"
VERSION=$HEC_VERSION
hec_require_release_name "$VERSION" || exit 1

GO=$GOROOT/bin/go
[[ -x $GO ]] || fail "pinned Go executable is missing: $GO"

COMMIT=$(git -C "$ROOT_DIR" rev-parse --verify HEAD 2>/dev/null) || fail "cannot resolve source commit"
hec_full_commit_is_valid "$COMMIT" || fail "source commit is not a full Git SHA: $COMMIT"

if [[ -n ${SOURCE_DATE_EPOCH+x} ]]; then
  [[ $SOURCE_DATE_EPOCH =~ ^[0-9]+$ ]] || fail "SOURCE_DATE_EPOCH must be a nonnegative integer"
  BUILD_EPOCH=$SOURCE_DATE_EPOCH
else
  BUILD_EPOCH=$(git -C "$ROOT_DIR" show -s --format=%ct "$COMMIT" 2>/dev/null) || fail "cannot resolve commit timestamp"
  [[ $BUILD_EPOCH =~ ^[0-9]+$ ]] || fail "commit timestamp is invalid: $BUILD_EPOCH"
fi
BUILD_DATE=$(date -u -d "@$BUILD_EPOCH" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || fail "SOURCE_DATE_EPOCH is outside the supported date range"

OUT=${HEC_OUTPUT:-$ROOT_DIR/dist/hec}
[[ -n $OUT ]] || fail "HEC_OUTPUT must not be empty"
OUT_PARENT=$(dirname -- "$OUT")
OUT_NAME=$(basename -- "$OUT")
mkdir -p -- "$OUT_PARENT"

TMP_OUT=$(mktemp "$OUT_PARENT/.${OUT_NAME}.tmp.XXXXXX")
cleanup() {
  if [[ -n ${TMP_OUT:-} ]]; then
    rm -f -- "$TMP_OUT"
  fi
}
trap cleanup EXIT

LDFLAGS="-s -w -X github.com/StealthEyeLLC/hec/internal/hec.Version=$VERSION -X github.com/StealthEyeLLC/hec/internal/hec.BuildCommit=$COMMIT -X github.com/StealthEyeLLC/hec/internal/hec.BuildDate=$BUILD_DATE"
(
  cd "$ROOT_DIR"
  "$GO" build \
    -trimpath \
    -ldflags "$LDFLAGS" \
    -o "$TMP_OUT" \
    ./cmd/hec
)
chmod 0755 "$TMP_OUT"
mv -f -- "$TMP_OUT" "$OUT"
TMP_OUT=

printf 'built HEC binary: output=%s version=%s commit=%s date=%s\n' \
  "$OUT" "$VERSION" "$COMMIT" "$BUILD_DATE"
