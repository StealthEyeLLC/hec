#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)

while IFS= read -r -d '' script; do
  bash -n "$script"
done < <(find "$ROOT_DIR" -type f -name '*.sh' -print0)

shellcheck \
  "$ROOT_DIR/scripts/build.sh" \
  "$ROOT_DIR/scripts/install.sh" \
  "$ROOT_DIR/scripts/cutover.sh" \
  "$ROOT_DIR/scripts/release-common.sh" \
  "$ROOT_DIR/scripts/validate-maintenance.sh" \
  "$ROOT_DIR/tests/scripts/test-maintenance.sh"

for script in \
  "$ROOT_DIR/scripts/build.sh" \
  "$ROOT_DIR/scripts/install.sh" \
  "$ROOT_DIR/scripts/cutover.sh" \
  "$ROOT_DIR/scripts/release-common.sh" \
  "$ROOT_DIR/scripts/validate-maintenance.sh" \
  "$ROOT_DIR/tests/scripts/test-maintenance.sh"; do
  [[ -x $script ]] || { echo "maintenance validation: not executable: $script" >&2; exit 1; }
done

"$ROOT_DIR/tests/scripts/test-maintenance.sh"
printf 'maintenance validation passed\n'
