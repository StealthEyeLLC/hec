#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
export PATH="/root/.local/bin:/root/.local/share/mise/shims:/root/.cargo/bin:$PATH"

fail() {
  echo "forge validation: $*" >&2
  exit 1
}

version_files=(
  mise.env uv.env rust.env corepack.env javascript.env docker.env
  quality-tools.env kubernetes-tools.env infrastructure-tools.env
  security-tools.env data-tools.env
)
for name in "${version_files[@]}"; do
  path="$ROOT_DIR/forge/versions/$name"
  [[ -s $path ]] || fail "missing or empty version file: $name"
  while IFS= read -r line; do
    [[ -z $line || $line == \#* || $line =~ ^[A-Z][A-Z0-9_]*=[^[:space:]]+$ ]] || fail "invalid assignment in $name: $line"
  done < "$path"
  if grep -Eiq '=(latest|stable)$|/(latest|stable)(/|$)' "$path"; then
    fail "unpinned value in $name"
  fi
done

for recipe in "$ROOT_DIR"/forge/recipes/*.sh; do
  [[ $(head -n1 "$recipe") == '#!/usr/bin/env bash' ]] || fail "wrong shebang: $recipe"
  grep -qx 'set -euo pipefail' "$recipe" || fail "missing strict mode: $recipe"
  bash -n "$recipe"
done

for list in "$ROOT_DIR/forge/apt/base.txt" "$ROOT_DIR/forge/apt/extended.txt"; do
  mapfile -t packages < <(sed -e 's/[[:space:]]*#.*$//' -e '/^[[:space:]]*$/d' "$list")
  ((${#packages[@]} > 0)) || fail "empty apt list: $list"
  duplicates=$(printf '%s\n' "${packages[@]}" | sort | uniq -d)
  [[ -z $duplicates ]] || fail "duplicate apt packages in $list: $duplicates"
  for package in "${packages[@]}"; do
    [[ $package =~ ^[a-z0-9][a-z0-9+.-]*(:[a-z0-9]+)?$ ]] || fail "invalid apt package line in $list: $package"
    apt-cache show --no-all-versions "$package" >/dev/null 2>&1 || fail "unavailable apt package in $list: $package"
  done
done

python3 - "$ROOT_DIR" <<'PY'
import pathlib
import sys
import tomllib

root = pathlib.Path(sys.argv[1])
recipes = root / "forge" / "recipes"
ids = set()
for path in sorted((root / "capabilities").glob("*.toml")):
    with path.open("rb") as handle:
        card = tomllib.load(handle)
    for field in ("id", "description", "tags", "commands", "approximate_disk_class"):
        if field not in card:
            raise SystemExit(f"{path}: missing {field}")
    if card["id"] in ids:
        raise SystemExit(f"{path}: duplicate id {card['id']}")
    ids.add(card["id"])
    if not isinstance(card["tags"], list) or not isinstance(card["commands"], list) or not isinstance(card.get("skills", []), list):
        raise SystemExit(f"{path}: tags, commands, and skills must be arrays")
    recipe = card.get("recipe")
    if recipe is not None and not (recipes / recipe).is_file():
        raise SystemExit(f"{path}: missing recipe {recipe}")
PY

required_commands=(
  mise uv uvx rustup rustc cargo rustfmt corepack pnpm yarn bun deno
  docker buildah shellcheck shfmt actionlint hadolint ec ast-grep tree-sitter
  tokei semgrep kubectl helm kustomize k9s stern kind k3d crictl oras regctl
  tofu terragrunt packer trivy syft grype cosign osv-scanner gitleaks
  duckdb csvcut
)
for command in "${required_commands[@]}"; do
  command -v "$command" >/dev/null || fail "installed command missing from forge PATH: $command"
done

schema_count=$(jq '.oneOf | length' "$ROOT_DIR/schemas/call-hec.input.json")
dispatcher_count=$(grep -E '^\s*case "[^"]+":' "$ROOT_DIR/internal/hec/dispatcher.go" | wc -l)
[[ $schema_count -eq 38 ]] || fail "schema operation count is $schema_count, want 38"
[[ $dispatcher_count -eq 38 ]] || fail "dispatcher operation count is $dispatcher_count, want 38"
forbidden_pattern='^(package\.install|forge\.install|docker\.|kubernetes\.|cloud\.|workspace\.|repository\.)'
if jq -r '.oneOf[].properties.operation.const' "$ROOT_DIR/schemas/call-hec.input.json" | grep -Eq "$forbidden_pattern"; then
  fail "forbidden Slice 8 or forge-specific schema operation found"
fi
if grep -E '^\s*case "[^"]+":' "$ROOT_DIR/internal/hec/dispatcher.go" | sed -E 's/.*case "([^"]+)":.*/\1/' | grep -Eq "$forbidden_pattern"; then
  fail "forbidden Slice 8 or forge-specific dispatcher operation found"
fi

printf 'forge validation passed: versions=%s recipes=%s manifests=%s schema=%s dispatcher=%s\n' \
  "${#version_files[@]}" \
  "$(find "$ROOT_DIR/forge/recipes" -maxdepth 1 -type f -name '*.sh' | wc -l)" \
  "$(find "$ROOT_DIR/capabilities" -maxdepth 1 -type f -name '*.toml' | wc -l)" \
  "$schema_count" "$dispatcher_count"
