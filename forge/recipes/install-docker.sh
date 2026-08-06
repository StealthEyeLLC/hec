#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "install-docker.sh must run as root" >&2
  exit 1
fi

export HOME=/root

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT_DIR/forge/versions/docker.env"

for name in DOCKER_CE_VERSION DOCKER_CE_CLI_VERSION CONTAINERD_IO_VERSION DOCKER_BUILDX_PLUGIN_VERSION DOCKER_COMPOSE_PLUGIN_VERSION; do
  [[ -n ${!name:-} ]] || { echo "missing $name" >&2; exit 1; }
done
[[ $(dpkg --print-architecture) == amd64 ]]
. /etc/os-release
[[ $VERSION_CODENAME == noble ]]

TMP_DIR=$(mktemp -d /tmp/hec-docker.XXXXXX)
cleanup() { rm -rf -- "$TMP_DIR"; }
trap cleanup EXIT
RESERVE_BYTES=4294967296
CURRENT_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
CADDY_BEFORE=$(systemctl is-active caddy.service 2>/dev/null || true)
ufw status verbose > "$TMP_DIR/ufw-before.txt" 2>&1 || true
nft list ruleset > "$TMP_DIR/nft-before.txt" 2>&1 || true

for conflict in docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc; do
  if dpkg-query -W -f='${db:Status-Status}' "$conflict" 2>/dev/null | grep -qx installed; then
    echo "conflicting package is installed: $conflict" >&2
    exit 1
  fi
done
for preserved in crun podman skopeo incus; do
  dpkg-query -W -f='${db:Status-Status}' "$preserved" 2>/dev/null | grep -qx installed
 done

install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o "$TMP_DIR/docker.asc"
gpg --show-keys --with-colons "$TMP_DIR/docker.asc" | grep -q 'fpr:::::::::9DC858229FC7DD38854AE2D88D81803C0EBFCD88:'
install -o root -g root -m 0644 "$TMP_DIR/docker.asc" /etc/apt/keyrings/docker.asc
cat > "$TMP_DIR/docker.sources" <<'SOURCEEOF'
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: noble
Components: stable
Architectures: amd64
Signed-By: /etc/apt/keyrings/docker.asc
SOURCEEOF
install -o root -g root -m 0644 "$TMP_DIR/docker.sources" /etc/apt/sources.list.d/docker.sources

export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l LC_ALL=C
apt-get update
PACKAGES=(
  "docker-ce=$DOCKER_CE_VERSION"
  "docker-ce-cli=$DOCKER_CE_CLI_VERSION"
  "containerd.io=$CONTAINERD_IO_VERSION"
  "docker-buildx-plugin=$DOCKER_BUILDX_PLUGIN_VERSION"
  "docker-compose-plugin=$DOCKER_COMPOSE_PLUGIN_VERSION"
)
apt-get -s --no-install-recommends install "${PACKAGES[@]}" > "$TMP_DIR/simulation.txt" 2>&1
if grep -qE '^(Remv |The following packages will be REMOVED:)' "$TMP_DIR/simulation.txt"; then
  cat "$TMP_DIR/simulation.txt" >&2
  echo "Docker apt simulation proposed removals" >&2
  exit 1
fi
apt-get --print-uris -y --no-install-recommends install "${PACKAGES[@]}" > "$TMP_DIR/estimate.txt" 2>&1
size_bytes() {
  local label=$1 line amount unit suffix
  line=$(grep -m1 "$label" "$TMP_DIR/estimate.txt" || true)
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
printf 'stage=docker current_available_bytes=%s projected_download_bytes=%s projected_installed_bytes=%s projected_available_bytes=%s reserve_bytes=%s\n' "$CURRENT_BYTES" "$DOWNLOAD_BYTES" "$INSTALLED_BYTES" "$PROJECTED_BYTES" "$RESERVE_BYTES"
((PROJECTED_BYTES >= RESERVE_BYTES)) || { echo "Docker installation would violate reserve; no packages installed" >&2; exit 1; }

HELLO_EXISTED=0
TEST_IMAGE_EXISTED=0
if command -v docker >/dev/null 2>&1; then
  docker image inspect hello-world:latest >/dev/null 2>&1 && HELLO_EXISTED=1 || true
  docker image inspect hec-slice7-test:0.0.9 >/dev/null 2>&1 && TEST_IMAGE_EXISTED=1 || true
fi
apt-get install -y --no-install-recommends "${PACKAGES[@]}"
systemctl is-active --quiet docker.service
systemctl is-active --quiet containerd.service
systemctl is-enabled --quiet docker.service
systemctl is-enabled --quiet docker.socket
docker version
docker info >/dev/null
docker buildx version
docker compose version
docker run --rm hello-world
if ((HELLO_EXISTED == 0)); then
  docker image rm hello-world:latest >/dev/null
fi
mkdir "$TMP_DIR/build"
printf 'slice7\n' > "$TMP_DIR/build/hello"
cat > "$TMP_DIR/build/Dockerfile" <<'DOCKEREOF'
FROM scratch
COPY hello /hello
DOCKEREOF
docker build --tag hec-slice7-test:0.0.9 "$TMP_DIR/build" >/dev/null
if ((TEST_IMAGE_EXISTED == 0)); then
  docker image rm hec-slice7-test:0.0.9 >/dev/null
fi

podman info >/dev/null
skopeo --version
incus list >/dev/null
qemu-system-x86_64 --version | head -n1
test -c /dev/kvm
systemctl is-active --quiet ssh.service
systemctl is-active --quiet hec.service
[[ $(systemctl is-active caddy.service 2>/dev/null || true) == "$CADDY_BEFORE" ]]
ufw status verbose > "$TMP_DIR/ufw-after.txt" 2>&1 || true
diff -u "$TMP_DIR/ufw-before.txt" "$TMP_DIR/ufw-after.txt"
nft list ruleset > "$TMP_DIR/nft-after.txt" 2>&1 || true
printf 'nft_before=%s nft_after=%s\n' "$(sha256sum "$TMP_DIR/nft-before.txt" | awk '{print $1}')" "$(sha256sum "$TMP_DIR/nft-after.txt" | awk '{print $1}')"
AFTER_BYTES=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
((AFTER_BYTES >= RESERVE_BYTES))
printf 'docker=%s compose=%s containerd=%s podman=%s incus=%s available_bytes=%s\n' "$(docker version --format '{{.Server.Version}}')" "$(docker compose version --short)" "$(containerd --version | head -n1)" "$(podman --version)" "$(incus version)" "$AFTER_BYTES"
