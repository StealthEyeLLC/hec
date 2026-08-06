#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
BUILD=$ROOT_DIR/scripts/build.sh
INSTALL=$ROOT_DIR/scripts/install.sh
CUTOVER=$ROOT_DIR/scripts/cutover.sh
COMMON=$ROOT_DIR/scripts/release-common.sh
TMP_ROOT=$(mktemp -d)
LAST_STDOUT=$TMP_ROOT/stdout
LAST_STDERR=$TMP_ROOT/stderr
trap 'rm -rf -- "$TMP_ROOT"' EXIT

fail() {
  echo "maintenance test failed: $*" >&2
  exit 1
}

expect_failure() {
  if "$@" >"$LAST_STDOUT" 2>"$LAST_STDERR"; then
    fail "command unexpectedly succeeded: $*"
  fi
}

assert_eq() {
  [[ $1 == "$2" ]] || fail "expected [$2], got [$1]"
}

assert_file_contains() {
  grep -Fq -- "$2" "$1" || fail "$1 does not contain: $2"
}

# shellcheck disable=SC1090
source "$COMMON"

for valid in 0.0.11 release-1 release_1 release+1 A; do
  hec_release_name_is_valid "$valid" || fail "valid release name rejected: $valid"
done
for invalid in '' '.' '..' '../x' '1..0' '/abs' 'white space' 'one/two' 'one\two'; do
  if hec_release_name_is_valid "$invalid"; then
    fail "invalid release name accepted: $invalid"
  fi
done

expect_failure env -u HEC_VERSION "$BUILD"
for invalid in '.' '..' '../x' '1..0' '/abs' 'white space' 'one/two' 'one\two'; do
  expect_failure env HEC_VERSION="$invalid" HEC_TEST_GOROOT="$TMP_ROOT/missing-go" "$BUILD"
done
expect_failure env HEC_VERSION=0.0.11 HEC_TEST_GOROOT="$TMP_ROOT/missing-go" "$BUILD"
expect_failure env HEC_VERSION=0.0.11 SOURCE_DATE_EPOCH=not-a-number "$BUILD"
expect_failure env HEC_VERSION='../x' "$INSTALL"

HEAD_COMMIT=$(git -C "$ROOT_DIR" rev-parse HEAD)
HEAD_EPOCH=$(git -C "$ROOT_DIR" show -s --format=%ct HEAD)
HEAD_DATE=$(date -u -d "@$HEAD_EPOCH" +%Y-%m-%dT%H:%M:%SZ)
BUILD_OUT=$TMP_ROOT/build/deep/hec
mkdir -p "$(dirname -- "$BUILD_OUT")"
printf 'sentinel-before-success\n' > "$BUILD_OUT"
STATUS_BEFORE=$(git -C "$ROOT_DIR" status --porcelain=v1)
env HEC_VERSION=0.0.11-test HEC_OUTPUT="$BUILD_OUT" "$BUILD" >"$LAST_STDOUT"
STATUS_AFTER=$(git -C "$ROOT_DIR" status --porcelain=v1)
assert_eq "$STATUS_AFTER" "$STATUS_BEFORE"
[[ -x $BUILD_OUT ]] || fail "build output is not executable"
VERSION_TEXT=$($BUILD_OUT version)
assert_file_contains "$LAST_STDOUT" "output=$BUILD_OUT"
assert_file_contains "$LAST_STDOUT" "version=0.0.11-test"
assert_file_contains "$LAST_STDOUT" "commit=$HEAD_COMMIT"
assert_file_contains "$LAST_STDOUT" "date=$HEAD_DATE"
assert_eq "$(awk '$1 == "HEC" {print $2}' <<<"$VERSION_TEXT")" 0.0.11-test
assert_eq "$(awk '$1 == "Protocol" {print $2}' <<<"$VERSION_TEXT")" HEC1/1.0.0
assert_eq "$(awk '$1 == "Commit" {print $2}' <<<"$VERSION_TEXT")" "$HEAD_COMMIT"
assert_eq "$(awk '$1 == "Built" {print $2}' <<<"$VERSION_TEXT")" "$HEAD_DATE"
if grep -Fq sentinel-before-success "$BUILD_OUT"; then
  fail "successful build did not atomically replace the prior output"
fi

FAKE_GOROOT=$TMP_ROOT/fake-go
mkdir -p "$FAKE_GOROOT/bin"
cat > "$FAKE_GOROOT/bin/go" <<'FAKEGO'
#!/usr/bin/env bash
set -euo pipefail
out=
while (($#)); do
  if [[ $1 == -o ]]; then
    shift
    out=$1
  fi
  shift || true
done
[[ -n $out ]]
printf 'partial-build\n' > "$out"
exit 23
FAKEGO
chmod 0755 "$FAKE_GOROOT/bin/go"
FAILED_OUT=$TMP_ROOT/failed/hec
mkdir -p "$(dirname -- "$FAILED_OUT")"
printf 'keep-existing\n' > "$FAILED_OUT"
expect_failure env HEC_VERSION=0.0.11 HEC_OUTPUT="$FAILED_OUT" HEC_TEST_GOROOT="$FAKE_GOROOT" "$BUILD"
assert_eq "$(cat "$FAILED_OUT")" keep-existing
if find "$(dirname -- "$FAILED_OUT")" -maxdepth 1 -name '.hec.tmp.*' -print -quit | grep -q .; then
  fail "failed build left a temporary sibling"
fi

RELEASE_TEST=$TMP_ROOT/release-helper
mkdir -p "$RELEASE_TEST/staging/bin" "$RELEASE_TEST/staging/skills" "$RELEASE_TEST/staging/capabilities" "$RELEASE_TEST/staging/forge"
printf 'binary\n' > "$RELEASE_TEST/staging/bin/hec"
chmod 0755 "$RELEASE_TEST/staging/bin/hec"
hec_install_staged_release "$RELEASE_TEST/staging" "$RELEASE_TEST/final"
[[ -d $RELEASE_TEST/final && ! -e $RELEASE_TEST/staging ]] || fail "new release was not atomically installed"
cp -a "$RELEASE_TEST/final" "$RELEASE_TEST/identical"
hec_install_staged_release "$RELEASE_TEST/identical" "$RELEASE_TEST/final"
[[ -d $RELEASE_TEST/identical ]] || fail "idempotent comparison unexpectedly moved staging"
chmod 0644 "$RELEASE_TEST/identical/bin/hec"
expect_failure hec_install_staged_release "$RELEASE_TEST/identical" "$RELEASE_TEST/final"
assert_eq "$(stat -c %a "$RELEASE_TEST/final/bin/hec")" 755

FAKE_SYSTEMCTL=$TMP_ROOT/fake-systemctl
cat > "$FAKE_SYSTEMCTL" <<'FAKESYSTEMCTL'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${FAKE_SYSTEMCTL_LOG:?}"
case ${1:-} in
  restart)
    [[ ${FAKE_RESTART_FAIL:-0} != 1 ]]
    ;;
  is-active)
    [[ ${FAKE_INACTIVE:-0} != 1 ]]
    ;;
  *)
    exit 0
    ;;
esac
FAKESYSTEMCTL
chmod 0755 "$FAKE_SYSTEMCTL"
FULL_COMMIT=0123456789abcdef0123456789abcdef01234567

new_cutover_root() {
  local root
  root=$(mktemp -d "$TMP_ROOT/cutover.XXXXXX")
  mkdir -p "$root/releases"
  printf '%s\n' "$root"
}

make_release() {
  local root=$1
  local directory_version=$2
  local reported_version=${3:-$directory_version}
  local protocol=${4:-HEC1/1.0.0}
  local commit=${5:-$FULL_COMMIT}
  local release=$root/releases/$directory_version
  mkdir -p "$release/bin" "$release/skills" "$release/capabilities" "$release/forge"
  cat > "$release/bin/hec" <<FAKEHEC
#!/usr/bin/env bash
set -euo pipefail
[[ \${1:-} == version ]]
printf 'HEC %s\nProtocol %s\nCommit %s\nBuilt 2026-01-01T00:00:00Z\n' \
  '$reported_version' '$protocol' '$commit'
FAKEHEC
  chmod 0755 "$release/bin/hec"
}

run_cutover() {
  local root=$1
  shift
  : > "$TMP_ROOT/systemctl.log"
  env HEC_ROOT="$root" HEC_SYSTEMCTL="$FAKE_SYSTEMCTL" \
    FAKE_SYSTEMCTL_LOG="$TMP_ROOT/systemctl.log" "$CUTOVER" "$@"
}

expect_cutover_failure() {
  local root=$1
  shift
  : > "$TMP_ROOT/systemctl.log"
  expect_failure env HEC_ROOT="$root" HEC_SYSTEMCTL="$FAKE_SYSTEMCTL" \
    FAKE_SYSTEMCTL_LOG="$TMP_ROOT/systemctl.log" "$CUTOVER" "$@"
}

expect_failure "$CUTOVER"
expect_failure "$CUTOVER" 1.0.0 extra
HELP_OUTPUT=$($CUTOVER --help)
assert_eq "$HELP_OUTPUT" 'usage: scripts/cutover.sh <version>'
ROOT=$(new_cutover_root)
expect_cutover_failure "$ROOT" '../x'
expect_cutover_failure "$ROOT" '1..0'
expect_cutover_failure "$ROOT" 1.2.3

ROOT=$(new_cutover_root)
mkdir -p "$ROOT/real-release"
ln -s ../real-release "$ROOT/releases/1.2.3"
expect_cutover_failure "$ROOT" 1.2.3

ROOT=$(new_cutover_root)
mkdir -p "$ROOT/releases/1.2.3/skills" "$ROOT/releases/1.2.3/capabilities" "$ROOT/releases/1.2.3/forge"
expect_cutover_failure "$ROOT" 1.2.3

ROOT=$(new_cutover_root)
make_release "$ROOT" 1.2.3
chmod 0644 "$ROOT/releases/1.2.3/bin/hec"
expect_cutover_failure "$ROOT" 1.2.3

ROOT=$(new_cutover_root)
make_release "$ROOT" 1.2.3
rm -rf "$ROOT/releases/1.2.3/forge"
expect_cutover_failure "$ROOT" 1.2.3

ROOT=$(new_cutover_root)
make_release "$ROOT" 1.2.3 9.9.9
expect_cutover_failure "$ROOT" 1.2.3

ROOT=$(new_cutover_root)
make_release "$ROOT" 1.2.3 1.2.3 HEC1/9.9.9
expect_cutover_failure "$ROOT" 1.2.3

ROOT=$(new_cutover_root)
make_release "$ROOT" 1.2.3 1.2.3 HEC1/1.0.0 short
expect_cutover_failure "$ROOT" 1.2.3

ROOT=$(new_cutover_root)
make_release "$ROOT" 1.2.3
printf 'not-a-link\n' > "$ROOT/current"
expect_cutover_failure "$ROOT" 1.2.3
assert_eq "$(cat "$ROOT/current")" not-a-link

ROOT=$(new_cutover_root)
make_release "$ROOT" 1.2.3
ln -s releases/0.0.1 "$ROOT/current"
SUCCESS_OUTPUT=$(run_cutover "$ROOT" 1.2.3)
assert_eq "$(readlink "$ROOT/current")" releases/1.2.3
assert_file_contains "$TMP_ROOT/systemctl.log" 'restart hec.service'
assert_file_contains "$TMP_ROOT/systemctl.log" 'is-active --quiet hec.service'
assert_file_contains <(printf '%s\n' "$SUCCESS_OUTPUT") 'previous=releases/0.0.1'
assert_file_contains <(printf '%s\n' "$SUCCESS_OUTPUT") "selected=$ROOT/releases/1.2.3"
run_cutover "$ROOT" 1.2.3 >/dev/null
assert_eq "$(readlink "$ROOT/current")" releases/1.2.3

ROOT=$(new_cutover_root)
make_release "$ROOT" 1.2.3
ln -s releases/0.0.1 "$ROOT/current"
: > "$TMP_ROOT/systemctl.log"
expect_failure env HEC_ROOT="$ROOT" HEC_SYSTEMCTL="$FAKE_SYSTEMCTL" \
  FAKE_SYSTEMCTL_LOG="$TMP_ROOT/systemctl.log" FAKE_RESTART_FAIL=1 "$CUTOVER" 1.2.3
assert_eq "$(readlink "$ROOT/current")" releases/1.2.3
assert_file_contains "$LAST_STDERR" 'current remains selected at releases/1.2.3'

ROOT=$(new_cutover_root)
make_release "$ROOT" 1.2.3
ln -s releases/0.0.1 "$ROOT/current"
: > "$TMP_ROOT/systemctl.log"
expect_failure env HEC_ROOT="$ROOT" HEC_SYSTEMCTL="$FAKE_SYSTEMCTL" \
  FAKE_SYSTEMCTL_LOG="$TMP_ROOT/systemctl.log" FAKE_INACTIVE=1 "$CUTOVER" 1.2.3
assert_eq "$(readlink "$ROOT/current")" releases/1.2.3
assert_file_contains "$LAST_STDERR" 'current remains selected at releases/1.2.3'

if grep -Ev '^(restart|is-active --quiet) hec\.service$' "$TMP_ROOT/systemctl.log" | grep -q .; then
  fail "cutover invoked an unexpected service command"
fi

printf 'maintenance script tests passed\n'
