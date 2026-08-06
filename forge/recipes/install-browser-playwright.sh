#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-browser-playwright.sh must run as root" >&2
  exit 1
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
FORGE_DIR=$(cd -- "$SCRIPT_DIR/.." && pwd)
VERSION_FILE="$FORGE_DIR/playwright/version.env"
CONFIG_FILE="$FORGE_DIR/playwright/cli.config.json"

if [[ ! -r $VERSION_FILE ]]; then
  echo "missing Playwright version file: $VERSION_FILE" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$VERSION_FILE"
: "${PLAYWRIGHT_CLI_VERSION:?PLAYWRIGHT_CLI_VERSION must be set}"

for command_name in node npm; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required command is missing: $command_name" >&2
    exit 1
  fi
done

node_major=$(node -p 'Number(process.versions.node.split(".")[0])')
if [[ ! $node_major =~ ^[0-9]+$ ]] || (( node_major < 18 )); then
  echo "Node.js 18 or newer is required; found $(node --version)" >&2
  exit 1
fi

npm install -g --prefix /usr/local "@playwright/cli@${PLAYWRIGHT_CLI_VERSION}"

if [[ $(command -v playwright-cli) != /usr/local/bin/playwright-cli ]]; then
  echo "playwright-cli is not available at /usr/local/bin/playwright-cli" >&2
  exit 1
fi

installed_version=$(node -p 'require("/usr/local/lib/node_modules/@playwright/cli/package.json").version')
installed_name=$(node -p 'require("/usr/local/lib/node_modules/@playwright/cli/package.json").name')
if [[ $installed_name != @playwright/cli || $installed_version != "$PLAYWRIGHT_CLI_VERSION" ]]; then
  echo "unexpected installed package: ${installed_name}@${installed_version}" >&2
  exit 1
fi

npm list -g --prefix /usr/local "@playwright/cli@${PLAYWRIGHT_CLI_VERSION}" --depth=0 >/dev/null

# Name Chromium explicitly. Omitting the browser on CLI 0.1.17 installs all engines.
playwright-cli install-browser chromium --with-deps

install -d -o root -g root -m 0700 \
  /var/lib/hec/browser \
  /var/lib/hec/browser/profiles \
  /var/lib/hec/browser/output

browser_list=$(playwright-cli install-browser --list)
if ! grep -q '/chromium-' <<<"$browser_list"; then
  echo "Playwright-managed Chromium was not found" >&2
  exit 1
fi
if grep -Eq '/(firefox|webkit)-' <<<"$browser_list"; then
  echo "unexpected Firefox or WebKit installation detected" >&2
  exit 1
fi

if [[ ! -r $CONFIG_FILE ]]; then
  echo "missing Playwright CLI config: $CONFIG_FILE" >&2
  exit 1
fi

tmp_dir=$(mktemp -d /var/tmp/hec-playwright-install.XXXXXX)
session="hec-install-verify-$$"
cleanup() {
  playwright-cli -s="$session" close >/dev/null 2>&1 || true
  rm -rf -- "$tmp_dir"
}
trap cleanup EXIT

install -d -m 0700 "$tmp_dir/profile" "$tmp_dir/output"
PLAYWRIGHT_CLI_SESSION="$session" \
PLAYWRIGHT_MCP_OUTPUT_DIR="$tmp_dir/output" \
  playwright-cli -s="$session" open about:blank \
    --config="$CONFIG_FILE" \
    --persistent \
    --profile="$tmp_dir/profile" >/dev/null
PLAYWRIGHT_CLI_SESSION="$session" \
PLAYWRIGHT_MCP_OUTPUT_DIR="$tmp_dir/output" \
  playwright-cli -s="$session" eval 'navigator.userAgent' >/dev/null
playwright-cli -s="$session" close >/dev/null

printf 'installed @playwright/cli %s with Playwright-managed Chromium\n' "$PLAYWRIGHT_CLI_VERSION"
