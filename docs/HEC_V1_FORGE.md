# HEC v1 Capability Forge

**Status:** v1 forge plan frozen; build-ready.

HEC is intended to make ChatGPT exceptionally capable across software engineering, DevOps, systems work, browsers, data, documents, media, networks, containers, virtual machines, clouds, and debugging.

The forge is broad. The forge manager is not.

> Install and use normal tools. Do not build a second package manager around them.

## 1. Installation model

HEC uses:

- `apt` for Ubuntu packages and native libraries;
- official upstream repositories where the Ubuntu package is absent or materially unsuitable;
- `mise` for project-specific runtimes and many standalone developer tools;
- `uv` and `uvx` for Python versions and isolated Python tools;
- `rustup` for Rust toolchains;
- Corepack and normal JavaScript package managers for Node projects;
- official CLIs and install archives for cloud and infrastructure tools;
- containers for disposable or conflicting services;
- plain shell recipes for everything else.

HEC does not implement dependency solving, package transactions, package locks, tool shims, or installation state. Existing package and runtime managers do that.

## 2. Disk-aware installation

The initial VPS has roughly 17 GiB free while SEZU, Baby, earlier implementation state, and the existing Incus pool remain present.

Installation therefore happens in three ordinary stages.

### Stage A — before cutover

Install only what HEC itself needs:

- Go 1.26.2;
- HEC source and binary;
- minimal build packages;
- the existing Node.js 24 toolchain;
- the existing tmux, Incus, Podman, QEMU, Python, and Git installations;
- Playwright CLI and one Chromium browser only after core HEC is connected.

Target additional disk use: less than 2 GiB.

### Stage B — after HEC works

After HEC is independently reachable from ChatGPT, StealthEye may explicitly remove obsolete SEZU, Baby, and earlier construction data. That should reclaim enough space for the main professional forge.

HEC never removes those systems automatically.

### Stage C — heavy and unusual tools

Large packages are installed when a real task needs them. Recipes remain checked into the HEC repository so installation is one command rather than rediscovery.

Examples:

- Blender;
- LibreOffice;
- full TeX Live;
- Android SDK and emulator images;
- Ghidra;
- FreeCAD;
- KiCad;
- large database servers;
- multiple Playwright browser engines;
- large language and science runtimes;
- Kubernetes clusters and cached images.

## 3. Forge repository structure

```text
forge/
  apt/
    base.txt
    extended.txt
    documents.txt
    media.txt
    reverse-engineering.txt
  recipes/
    install-go.sh
    install-mise.sh
    install-uv.sh
    install-rustup.sh
    install-docker.sh
    install-playwright.sh
    install-kubernetes-tools.sh
    install-cloud-aws.sh
    install-cloud-google.sh
    install-cloud-azure.sh
    install-documents.sh
    install-media.sh
    install-android.sh
    install-reverse-engineering.sh
    install-cad.sh
    install-data-science.sh
  capabilities/
    core.toml
    languages.toml
    devops.toml
    browser.toml
    documents.toml
    media.toml
    data.toml
    systems.toml
```

Recipes are readable shell scripts. ChatGPT runs them through `run`. There is no recipe daemon or installation controller.

## 4. Always-ready base tools

The base layer should be available after Stage B unless a package conflict or disk measurement proves otherwise.

### Shell and navigation

```text
bash
zsh
fish
coreutils
findutils
grep
sed
gawk
less
which
file
tree
jq
yq
ripgrep
fd
fzf
bat
eza
parallel
watch
pv
entr
direnv
```

### File transfer, archives, and encoding

```text
curl
wget
rsync
rclone
aria2
openssh-client
openssh-server
sftp
scp
zip
unzip
p7zip-full
tar
gzip
bzip2
xz-utils
zstd
lz4
cabextract
unrar-free
cpio
base64
xxd
```

### Source control and collaboration

```text
git
git-lfs
gh
git-filter-repo
subversion
mercurial
```

HEC favors Git worktrees for parallel branches because they share the repository object database while keeping separate working trees.

### Editors and terminal work

```text
vim
neovim
nano
emacs-nox
tmux
screen
```

ChatGPT normally edits through HEC file operations, patches, scripts, or noninteractive editor commands. Interactive editors remain available when useful.

### Build foundations

```text
build-essential
gcc
g++
clang
llvm
lld
clangd
make
cmake
ninja-build
meson
pkg-config
autoconf
automake
libtool
m4
ccache
sccache
bear
nasm
yasm
```

### Debugging and profiling

```text
gdb
lldb
strace
ltrace
valgrind
linux-tools
perf
bpftrace
systemtap-sdt-dev
pahole
elfutils
lsof
procps
psmisc
sysstat
iotop
htop
btop
```

### Binary inspection

```text
binutils
readelf
objdump
nm
strings
patchelf
chrpath
elfutils
hexdump
xxd
radare2
```

Ghidra, Cutter, pwndbg, gef, apktool, jadx, and additional reversing suites are on-demand recipes.

### System administration

```text
systemd tools
util-linux
acl
attr
sudo
cron
logrotate
rsyslog tools
apparmor tools
udev tools
cloud-init tools
needrestart
```

HEC does not create a separate administration abstraction. It uses the host directly.

### Storage and filesystems

```text
e2fsprogs
btrfs-progs
xfsprogs
dosfstools
ntfs-3g
exfatprogs
squashfs-tools
erofs-utils
lvm2
mdadm
cryptsetup
parted
gdisk
fdisk
smartmontools
nvme-cli
hdparm
qemu-utils
cloud-guest-utils
fuse3
sshfs
```

### Networking and protocols

```text
iproute2
iputils-ping
ethtool
bridge-utils
nftables
iptables
conntrack
wireguard-tools
openvpn
network-manager tools
dnsutils
bind9-utils
whois
mtr-tiny
traceroute
nmap
masscan
tcpdump
tshark
socat
netcat-openbsd
iperf3
ngrep
arping
httpie
websocat
grpcurl
mosquitto-clients
snmp
ldap-utils
krb5-user
```

### Certificates, keys, and signing

```text
openssl
gnupg
age
sops
step-cli
cosign
ssh-keygen
keytool
```

These are ordinary capabilities. HEC adds no credential broker or signing policy.

## 5. Language and runtime strategy

The host system Python remains untouched because Ubuntu uses it internally.

Project runtimes are supplied by their normal managers.

### Go

HEC itself uses Go 1.26.2 under:

```text
/opt/hec/toolchains/go/1.26.2
```

Additional Go versions may be installed using Go's official multi-version mechanism or mise.

### Python

Install `uv` globally for root.

Use:

```text
uv python install <version>
uv sync
uv run
uv tool install <tool>
uvx <tool>
```

`uvx` is particularly useful for ChatGPT because it runs a Python tool in an isolated temporary environment without permanently polluting the host.

Common globally installed or frequently invoked tools:

```text
ruff
black
mypy
pytest
pre-commit
poetry
pip-tools
nox
tox
httpie
ansible
mkdocs
jupyter
csvkit
```

Project dependencies stay in project virtual environments.

### Rust

Install rustup under `/root/.cargo`.

Use project `rust-toolchain.toml` files and ordinary Cargo commands.

Useful installed tools may include:

```text
cargo-edit
cargo-audit
cargo-deny
cargo-nextest
cargo-binstall
cargo-expand
cargo-watch
cargo-flamegraph
```

### JavaScript and TypeScript

Keep a modern Node.js 24 installation for HEC tooling.

Install or enable:

```text
corepack
npm
pnpm
yarn
bun
deno
```

Projects remain authoritative through `package.json`, lock files, `.node-version`, and `mise.toml`.

Frequently useful global tools:

```text
typescript
tsx
eslint
prettier
@playwright/cli
@mermaid-js/mermaid-cli
markdownlint-cli2
```

### JVM

Install a current OpenJDK LTS and use mise or SDKMAN only when projects require additional versions.

Tools:

```text
java
javac
jdb
jcmd
jstack
jmap
jfr
maven
gradle
```

### .NET

Install the current supported .NET SDK through Microsoft's official Ubuntu repository when first needed.

### Additional runtimes through mise or recipes

```text
Ruby
PHP
Elixir
Erlang
Zig
Nim
Crystal
Lua
LuaJIT
Perl
R
Julia
Dart
Flutter
Haskell
OCaml
Clojure
Scala
Groovy
Swift for Linux
```

These do not all need to occupy disk on day one. `mise exec <tool>@<version> -- <command>` gives ChatGPT direct access on demand.

HEC configures mise with:

```text
MISE_TRUSTED_CONFIG_PATHS=/
```

This removes interactive trust prompts because HEC intentionally gives ChatGPT unrestricted execution already.

## 6. Structured code search and quality tools

Install or make one-command available:

```text
tree-sitter
ast-grep
semgrep
comby
ctags
cscope
universal-ctags
shellcheck
shfmt
actionlint
hadolint
yamllint
markdownlint
editorconfig-checker
codespell
cloc
tokei
hyperfine
```

These are tools ChatGPT may choose. They are never mandatory gates.

## 7. Containers and virtual machines

### Incus

Preserve the existing Incus installation and KVM capability.

Incus supplies:

- full Linux system containers;
- OCI application containers;
- virtual machines with their own kernels;
- images, snapshots, storage, networking, direct exec, and file transfer.

HEC does not wrap Incus. It ships an Incus skill and uses the `incus` CLI.

### Podman

Preserve Podman as a daemonless OCI engine.

Add when useful:

```text
buildah
skopeo
crun
podman-compose
```

### Docker

Install official Docker Engine, Docker CLI, containerd, Buildx, and the Compose plugin after checking the existing container package set for conflicts.

Docker is included because many repositories, examples, CI systems, and third-party tools assume the Docker API and CLI even when Podman is also present.

### QEMU and KVM

Preserve QEMU and `/dev/kvm`.

Install on demand:

```text
libvirt-daemon-system
libvirt-clients
virt-install
virt-manager dependencies without desktop UI where possible
virtiofsd
ovmf
```

HEC may use raw QEMU, libvirt, or Incus VMs according to the task. There is no HEC VM abstraction.

### Lightweight isolation

Install `bubblewrap` and namespace utilities as optional tools, not as mandatory execution boundaries.

## 8. Kubernetes and infrastructure

Base or early extended layer:

```text
kubectl
helm
kustomize
k9s
stern
kind
k3d
minikube client tooling
crictl
oras
regctl
```

Infrastructure tools:

```text
opentofu
terraform when licensing or project compatibility requires it
terragrunt
packer
ansible
ansible-lint
vagrant when needed
pulumi when needed
nomad CLI
consul CLI
vault CLI
```

No cluster, registry, or infrastructure service runs permanently unless a real task starts it.

## 9. Cloud and hosting CLIs

Install through plain recipes as accounts or tasks require them:

```text
aws
sam
cdk
session-manager-plugin
gcloud
gsutil
bq
azure-cli
azcopy
doctl
hcloud
linode-cli
oci
cloudflared
wrangler
flyctl
vercel
netlify
railway
render
supabase
firebase
heroku
```

HEC uses the CLIs' native credential stores and environment variables.

## 10. Databases, queues, and object storage

Install clients broadly:

```text
sqlite3
duckdb
psql and PostgreSQL client tools
MariaDB/MySQL client tools
redis-cli
mongosh
clickhouse-client
influx CLI
cqlsh
etcdctl
nats
rabbitmqadmin
mc for MinIO
rclone
```

Database servers normally run:

- as a native package when the task genuinely requires host integration;
- as a Podman or Docker container;
- in an Incus system container;
- inside a temporary VM.

HEC has no service-template subsystem.

## 11. Web and API development

Useful tools:

```text
Playwright CLI
Chromium
curl
HTTPie
websocat
grpcurl
mitmproxy
ngrok or equivalent when explicitly installed
Caddy
nginx
Apache utilities
mkcert
vegeta
wrk
hey
k6
ab
```

Caddy is already present on the host. HEC may operate or replace its configuration directly.

## 12. Security and supply-chain tools

Available as ordinary tools:

```text
trivy
syft
gryphe
cosign
osv-scanner
gitleaks
semgrep
bandit
checkov
kube-bench
kube-linter
nikto
sqlmap
john
hashcat
hydra
nuclei
```

Spelling correction: the intended image scanner is `grype`.

These tools are not compulsory scans or policy gates. They are capabilities ChatGPT can use when the task calls for them.

## 13. Data engineering and science

Base lightweight tools:

```text
sqlite3
duckdb
jq
yq
miller
csvkit
xsv
visidata
gnuplot
graphviz
```

On demand:

```text
JupyterLab
R
Julia
Polars
PyArrow
Pandas
NumPy
SciPy
Matplotlib
Dask
Spark clients
DataFusion tools
PostgreSQL extensions and spatial clients
GDAL
PROJ
```

Project Python environments hold Python libraries rather than a huge global site-packages directory.

## 14. Documents and publishing

Extended layer after disk reclamation:

```text
pandoc
typst
poppler-utils
qpdf
ghostscript
ocrmypdf
tesseract-ocr
unpaper
imagemagick
libreoffice
wkhtmltopdf or modern browser-print alternatives
weasyprint
calibre tools
exiftool
```

On demand:

```text
TeX Live collections
Inkscape
Scribus
additional OCR language packs
font packages
```

ChatGPT can create, render, inspect, convert, OCR, repair, and package documents through ordinary commands and returned artifacts.

## 15. Images, audio, and video

Extended layer:

```text
ffmpeg
ffprobe
imagemagick
graphicsmagick
exiftool
sox
flac
lame
opus-tools
webp
libvips tools
```

On demand:

```text
Blender
Kdenlive command dependencies
OpenToonz
Synfig
GIMP batch tools
Krita batch tools
waifu2x and other selected local utilities when hardware permits
```

No local generative model service is required. Cloud generators are callable through their ordinary APIs and CLIs if credentials are present.

## 16. Diagrams, CAD, electronics, and 3D

Useful lightweight tools:

```text
graphviz
plantuml
mermaid-cli
openscad
assimp tools
meshlab command tools where available
```

On demand:

```text
FreeCAD
KiCad
Blender
BRL-CAD
OpenSCAD libraries
Gerber and PCB conversion tools
```

## 17. Mobile and cross-platform development

On demand:

```text
Android command-line tools
Android SDK platform tools
adb
fastboot
Gradle
selected Android platforms and build tools
Android emulator images only when disk permits
Flutter
Dart
React Native dependencies
```

Linux cannot provide native Apple platform builds. HEC can still edit and test portable portions, operate remote macOS builders over SSH, or use a future owner-controlled Mac target through ordinary SSH tooling.

## 18. Browser forge

Install:

```text
npm install -g @playwright/cli@<resolved-version>
playwright-cli install --skills
playwright-cli install-browser --with-deps
```

Commit the resolved CLI version to the HEC forge configuration after the first successful install.

Start with Chromium only. Install Firefox and WebKit when needed.

Use named persistent sessions and custom profile directories under `/var/lib/hec/browser/`.

## 19. Capability manifests

Each capability group gets a small TOML description. Example:

```toml
id = "reverse.ghidra"
description = "Interactive and headless Java reverse-engineering suite"
tags = ["binary", "reverse-engineering", "decompile"]
commands = ["ghidraRun", "analyzeHeadless"]
installed_by_default = false
recipe = "install-reverse-engineering.sh"
approximate_disk_class = "large"
```

These files exist only so ChatGPT can discover what is installed and how to get what is missing. They are not a package database and HEC never uses them to deny execution.

## 20. Cache and space management

HEC does not run automatic cleanup.

ChatGPT may use ordinary commands such as:

```text
apt clean
uv cache prune
cargo cache tools
go clean -cache
npm cache clean
pnpm store prune
docker system df
docker builder prune
podman system df
podman system prune
incus storage volume list
playwright-cli close-all
```

The owner decides when deletion occurs.

## 21. Research basis

Primary references:

- mise: https://mise.jdx.dev/
- uv: https://docs.astral.sh/uv/
- rustup: https://rust-lang.github.io/rustup/
- Go multi-version installs: https://go.dev/doc/manage-install
- Playwright CLI: https://playwright.dev/agent-cli/introduction
- Incus: https://linuxcontainers.org/incus/docs/main/
- Podman: https://docs.podman.io/en/latest/
- Docker Engine: https://docs.docker.com/engine/install/ubuntu/
- Docker Buildx: https://docs.docker.com/reference/cli/docker/buildx/
- Docker Compose: https://docs.docker.com/compose/install/linux/
- Git worktrees: https://git-scm.com/docs/git-worktree
