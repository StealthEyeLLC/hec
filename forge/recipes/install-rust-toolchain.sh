#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-rust-toolchain.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/rust.env"

for name in RUSTUP_VERSION RUST_TOOLCHAIN RUSTC_VERSION RUSTC_COMMIT CARGO_VERSION CARGO_COMMIT RUST_HOST RUSTFMT_VERSION CLIPPY_VERSION; do
  [[ -n ${!name:-} ]] || { echo "missing $name" >&2; exit 1; }
done
[[ $RUST_TOOLCHAIN == "$RUSTC_VERSION-$RUST_HOST" ]]
[[ $(rustup --version | awk 'NR==1 {print $2}') == "$RUSTUP_VERSION" ]]

export RUSTUP_HOME=/root/.rustup CARGO_HOME=/root/.cargo
rustup toolchain install "$RUST_TOOLCHAIN" --profile default --component rustfmt clippy --no-self-update
rustup default "$RUST_TOOLCHAIN"
rustup show
[[ $(rustc --version | awk '{print $2}') == "$RUSTC_VERSION" ]]
[[ $(rustc --version --verbose | awk '/^commit-hash:/ {print $2}') == "$RUSTC_COMMIT" ]]
[[ $(cargo --version | awk '{print $2}') == "$CARGO_VERSION" ]]
[[ $(cargo --version --verbose | awk '/^commit-hash:/ {print $2}') == "$CARGO_COMMIT" ]]
rustfmt --version
cargo clippy --version
printf 'rustup=%s toolchain=%s rustc=%s cargo=%s rustfmt=%s clippy=%s\n' "$RUSTUP_VERSION" "$RUST_TOOLCHAIN" "$(rustc --version)" "$(cargo --version)" "$(rustfmt --version)" "$(cargo clippy --version)"
