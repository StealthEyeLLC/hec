# HEC — ChatGPT Project Source

**Repository:** `StealthEyeLLC/hec`  
**Project:** HEC  
**Status:** Canonical v1 source frozen; prebuild complete; implementation may begin only after an explicit StealthEye instruction.

---

## 1. Purpose of this document

This is the canonical source document for the HEC ChatGPT Project.

Use it to preserve HEC’s identity, constraints, architecture, operating model, construction order, and product boundaries across chats and failed ChatGPT turns.

The detailed repository documents remain authoritative for implementation specifics:

- `README.md`
- `docs/HEC_V1_DESIGN.md`
- `docs/HEC_V1_CHATGPT_INTERFACE.md`
- `docs/HEC_V1_FORGE.md`
- `docs/HEC_V1_BUILD_PLAN.md`

This source document resolves the project at the conceptual and operational level. When a later discussion conflicts with this document, distinguish informal exploration from an explicitly approved change to the canonical design.

---

## 2. Product identity

HEC is a standalone, ChatGPT-native, unrestricted root engineering environment designed by and for ChatGPT and StealthEye.

Its purpose is to give ChatGPT the broadest practical ability to perform:

- software engineering;
- DevOps;
- systems administration;
- debugging and profiling;
- builds and releases;
- deployments;
- browser automation;
- cloud and infrastructure work;
- networking and storage work;
- containers and virtual machines;
- database and data work;
- document, PDF, image, audio, video, CAD, diagram, and 3D work;
- research and artifact production;

on an owner-controlled Linux host with the least possible friction and internal machinery.

HEC’s governing principle is:

> **Maximum capability. Minimum bullshit.**

HEC is intentionally large in tools and intentionally small in architecture.

---

## 3. Non-negotiable founding contract

### 3.1 Full power

- The real Linux host is the primary execution environment.
- ChatGPT receives unrestricted root execution as a first-class capability.
- A raw unrestricted execution path always exists.
- Absolute paths are accepted.
- Arbitrary commands, packages, services, daemons, source builds, mounts, filesystems, networks, containers, virtual machines, browsers, debuggers, cloud clients, compilers, and third-party tools remain directly usable.
- HEC is designed specifically around ChatGPT’s tool-calling, context, file, image, and artifact behavior.
- HEC remains standalone after construction.
- SEZU and Baby may be used to build HEC but are never HEC runtime dependencies.

### 3.2 No permanent forge container

There is no required `u` container and no permanent forge container.

The host itself is the workstation.

Incus, Docker, Podman, QEMU/KVM, namespaces, and other isolation mechanisms remain available as ordinary tools when a task benefits from them. They are not mandatory execution boundaries or HEC architectural layers.

### 3.3 Native systems remain authoritative

HEC does not recreate native systems under new names.

- Git owns source history, branches, commits, worktrees, and recovery.
- systemd owns services and durable processes.
- tmux owns persistent interactive terminals.
- Linux filesystems own files and directories.
- `apt` and ecosystem-native managers own packages and dependencies.
- Incus owns system containers and compatible VMs.
- Podman and Docker own OCI containers and images.
- QEMU/KVM and optionally libvirt own virtual machines.
- Playwright CLI owns browser automation.
- Official cloud CLIs own cloud-provider operations.
- Project files own project configuration and reproducibility.

HEC connects ChatGPT directly to these systems and gets out of the way.

### 3.4 Direct work stays direct

A simple command is a direct command.

A file operation is a file operation.

A Git operation normally uses Git.

A service operation normally uses `systemctl` or `journalctl`.

A container operation normally uses `incus`, `podman`, or `docker`.

HEC adds a primitive only when MCP transport, detached execution, persistent interaction, binary transfer, returned artifacts, or ChatGPT capability discovery requires it.

---

## 4. Explicit exclusions

HEC does not add any of the following:

- policy engines;
- approval systems;
- command filters;
- safety layers;
- safety theater;
- receipts;
- evidence systems;
- audit systems;
- reporting systems;
- mandatory previews;
- mandatory dry runs;
- mandatory preflight;
- mandatory verification;
- verification frameworks;
- automatic rollback;
- automatic reconciliation frameworks;
- governance systems;
- observability theater;
- task ledgers;
- task capsules;
- model-reasoning storage;
- universal operation history;
- a workflow platform;
- a second package manager;
- a second process manager;
- a second container manager;
- a second filesystem model;
- a local model or planner;
- automatic tool installation based on guesses;
- arbitrary command caching;
- abstractions that merely rename Linux commands.

ChatGPT and StealthEye may inspect, test, verify, commit, branch, snapshot, or back up work whenever a real task calls for those actions. They are ordinary capabilities, not compulsory HEC subsystems.

HEC itself adds no confirmation or restriction layer. ChatGPT or the hosting platform may still impose platform behavior outside HEC.

---

## 5. Final v1 architecture

```text
ChatGPT
   |
   | OpenAI Secure MCP Tunnel
   v
one root hec.service
   |
   | embedded in-memory MCP transport
   v
one stable public tool: call_hec
   |
   +-- unrestricted synchronous execution
   +-- systemd-backed durable jobs
   +-- tmux-backed persistent terminals
   +-- binary-safe file operations
   +-- chunked uploads
   +-- returned artifacts
   +-- skills and capability discovery
   +-- every installed native CLI
```

### 5.1 Runtime decisions

- Implementation language: Go.
- Initial Go version: 1.26.2.
- Tunnel library: `github.com/openai/tunnel-client` v0.0.10.
- MCP SDK: `github.com/modelcontextprotocol/go-sdk` v1.4.1.
- HEC binary: `/opt/hec/current/bin/hec`.
- Permanent HEC services: exactly one, `hec.service`.
- Service identity: root.
- Local HEC database: none.
- Local HEC API port: none.
- Supervisor socket: none.
- Separate gateway daemon: none.
- Node runtime dependency for HEC core: none.
- Embedded Secure MCP Tunnel and MCP server run in one process using in-memory transport.

The same binary supplies:

```text
hec serve
hec call
hec job-run
hec version
```

If embedding the tunnel proves concretely troublesome during implementation, the smallest acceptable fallback is a separate `hec-tunnel.service`. Do not redesign the rest of HEC around that fallback.

---

## 6. Public ChatGPT interface

HEC exposes one stable MCP tool:

```text
call_hec
```

Recommended title:

```text
HEC
```

Recommended description:

> Operate the HEC workstation.

Input:

```json
{
  "operation": "run",
  "args": {},
  "idempotency_key": "optional"
}
```

The public schema remains small and stable. Operation-specific arguments are validated after dispatch.

Do not publish one MCP tool for every native command or domain.

The internal operation catalog may grow without changing the public ChatGPT action.

---

## 7. Public metadata and friction freeze

Initial public metadata is frozen as:

```text
MCP tool name: call_hec
Title: HEC
Description: Operate the HEC workstation.
```

The initial action omits optional risk annotations. Do not add warning prose, split read/write public actions, confirmation-tuning wrappers, platform-specific indirection, or other anticipatory friction measures before real use.

Anything previously discussed as a possible platform-friction measure is deferred. It may be added only after actual operation demonstrates a concrete need and StealthEye confirms that the change adds no meaningful friction.

Internal operations are optimized for ChatGPT clarity:

```text
run
job.*
terminal.*
file.*
upload.*
artifact.*
capabilities
skill.*
health
version
```

## 8. ChatGPT operation-selection model

The interface should make the following choices nearly automatic:

```text
run something now                  -> run
work may outlive this turn         -> job.start
program needs a PTY or later input -> terminal.open
inspect or edit exact bytes        -> file.*
send a large file to HEC           -> upload.*
return a file to ChatGPT           -> artifact.*
discover installed capability      -> capabilities
load operating guidance            -> skill.find / skill.read
```

### 8.1 `run`

Use for noninteractive commands expected to finish during the current tool call.

It supports:

- shell commands through `/bin/bash -lc`;
- direct argv execution;
- absolute or relative working directories;
- environment additions and removals;
- UTF-8 or base64 stdin;
- timeout;
- bounded stdout and stderr;
- UTF-8 or base64 output;
- native exit code and signal.

There is no allowlist, denylist, approval pass, preview pass, or target selection.

### 8.2 Durable jobs

Use `job.start` when a noninteractive command may run longer than the current turn or must survive a broken ChatGPT response.

Jobs run as transient systemd services:

```text
hec-job-<id>.service
```

Job state is stored only as ordinary files needed to operate the process:

```text
/var/lib/hec/jobs/<id>/
  spec.json
  stdout
  stderr
  result.json
```

Core job operations:

```text
job.start
job.status
job.output
job.wait
job.signal
job.list
job.forget
```

Output is read by byte offset.

Only durable job creation uses an optional idempotency key. Repeating the same key returns the existing job instead of starting a duplicate.

No automatic retry occurs.

### 8.3 Persistent terminals

Use terminals for:

- shells;
- debuggers;
- REPLs;
- SSH;
- database consoles;
- interactive installers;
- programs requiring terminal dimensions or continuing input.

Terminals use a dedicated tmux server.

Core operations:

```text
terminal.open
terminal.list
terminal.read
terminal.write
terminal.resize
terminal.signal
terminal.close
```

Terminal reads support:

- current visible screen;
- accumulated output by byte offset.

Terminal writes paste exact bytes through tmux buffers rather than relying on shell-quoted `send-keys`.

### 8.4 Files and uploads

Core file operations:

```text
file.stat
file.list
file.read
file.write
file.append
file.patch
file.remove
```

Core upload operations:

```text
upload.begin
upload.chunk
upload.finish
upload.abort
```

Absolute paths are allowed.

Binary reads and writes use base64 when necessary.

Large files use chunked transfer.

Copy, move, find, permissions, ownership, ACLs, archives, rsync, rclone, mounts, extraction, and similar operations normally use native commands through `run`.

### 8.5 Returned artifacts

Artifacts exist only to make generated files downloadable or reusable through ChatGPT.

Core operations:

```text
artifact.return
artifact.stat
artifact.read
artifact.materialize
artifact.list
artifact.delete
```

Artifacts are:

- an immutable copied file; or
- a `.tar.zst` bundle for a directory.

They live under:

```text
/var/lib/hec/artifacts/
```

HEC returns MCP resource links when the client supports them. `artifact.read` remains the fallback.

There is no content-addressed store, artifact registry, garbage collector, tree database, evidence package, or receipt system.

---

## 9. Result design

Results are optimized for ChatGPT comprehension and context economy.

Stable shape:

```json
{
  "ok": true,
  "protocol": "HEC1/1.0.0",
  "operation": "run",
  "status": "completed",
  "handle": null,
  "exit_code": 0,
  "signal": null,
  "stdout": "",
  "stderr": "",
  "stdout_encoding": "utf8",
  "stderr_encoding": "utf8",
  "truncated": false,
  "result": {},
  "resources": [],
  "error": null
}
```

Return:

- operation;
- status;
- exit code or signal when relevant;
- nonempty stdout and stderr;
- encoding;
- truncation status;
- handle only when future calls need it;
- operation-specific structured result;
- returned resources;
- short exact error.

Do not return by default:

- echoed request arguments;
- receipt IDs;
- evidence;
- request histories;
- audit metadata;
- trace IDs;
- risk labels;
- policy decisions;
- verification fields;
- host snapshots;
- repeated copies of the same information;
- long help text after success.

Default combined inline stdout and stderr limit:

```text
1 MiB
```

Large outputs should use a durable job, byte-offset reads, or a file.

MCP results should contain:

1. structured content with the HEC result;
2. one concise text summary.

---

## 10. Capability discovery and skills

HEC is broad, but ChatGPT should not receive a complete binary inventory in every turn.

### 10.1 Capability discovery

`capabilities` searches:

- built-in HEC operations;
- plain TOML capability manifests;
- installed skill metadata;
- install recipes;
- optionally named commands on `PATH`.

Example query:

```json
{
  "query": "persistent browser automation",
  "limit": 10,
  "include_missing": true
}
```

Example result card:

```json
{
  "id": "browser.playwright",
  "description": "Persistent browser automation designed for coding agents",
  "installed": true,
  "commands": ["playwright-cli"],
  "skills": ["playwright-cli"],
  "recipe": null
}
```

Discovery uses ordinary metadata and text matching.

There is no embedding server, vector database, capability graph, planner, or automatic installer.

### 10.2 Skills

Skill roots:

```text
/opt/hec/current/skills
/etc/hec/skills
/srv/hec/workspaces/<name>/.hec/skills
```

Skill structure:

```text
skill-name/
  SKILL.md
  agents/openai.yaml
  scripts/
  references/
  assets/
```

Core operations:

```text
skill.list
skill.find
skill.read
```

Discovery returns only lightweight metadata until ChatGPT requests the full skill.

Skills provide instructions, scripts, references, and reusable practices. They do not grant permissions, enforce workflows, or restrict raw execution.

Native CLI preference:

```text
raw native CLI
-> established HEC or project skill
-> small HEC primitive only when MCP transport requires it
```

---

## 11. Browser design

HEC does not implement a custom browser action API.

Install and use the official Playwright CLI for coding agents plus its Agent Skills.

ChatGPT operates Playwright through `run` or a persistent terminal.

Default browser:

```text
Chromium
```

Firefox and WebKit are installed when needed.

Persistent profiles:

```text
/var/lib/hec/browser/profiles/<name>/
```

Browser outputs:

```text
/var/lib/hec/browser/output/<name>/
```

Normal loop:

```text
open named session
read accessibility snapshot
interact using current element references
read updated snapshot
capture screenshot/download/trace/video when useful
return files through HEC artifacts
```

HEC adds no origin allowlists, proxy restrictions, browser policy engine, or custom browser state database.

---

## 12. Workspaces and project state

A workspace is a convention, not a controller.

```text
/srv/hec/workspaces/<name>/
  repository/
  .hec/
    workspace.toml
    skills/
    scratch/
```

A workspace may provide:

- default working directory;
- repository path;
- environment additions;
- notes;
- skill hints.

Raw paths always work.

HEC does not reconcile the host to a workspace declaration.

Normal project files remain authoritative:

```text
.git
mise.toml
pyproject.toml
package.json
go.mod
Cargo.toml
Dockerfile
Compose files
Terraform or OpenTofu files
Ansible files
CI definitions
```

Use Git worktrees for parallel branches and experiments.

---

## 13. Credentials

HEC uses native credential locations and mechanisms:

```text
/root/.ssh
/root/.config/gh
/root/.aws
/root/.config/gcloud
/root/.azure
/root/.docker
/root/.config/containers
/root/.kube
```

HEC has no credential broker, secret database, approval system, or credential policy engine.

Secure MCP Tunnel credentials are supplied to `hec.service` through a root-readable environment file under `/etc/hec/`.

---

## 14. Capability forge

HEC should eventually provide a broad professional workstation, but HEC does not build a custom package manager.

Use:

- `apt` for Ubuntu packages and native libraries;
- official upstream repositories where appropriate;
- `mise` for project runtimes and many standalone tools;
- `uv` and `uvx` for Python versions and isolated Python tools;
- `rustup` for Rust toolchains;
- Corepack and native JavaScript package managers;
- Go modules, Cargo, Maven, Gradle, and other ecosystem-native managers;
- official cloud and infrastructure installers;
- Docker, Podman, or Incus for conflicting or disposable servers;
- plain readable shell recipes for the long tail.

### 14.1 Broad capability domains

The forge should cover:

- shell, navigation, text, archive, transfer, and encoding tools;
- Git, GitHub CLI, Git LFS, worktrees, and collaboration tools;
- C, C++, Go, Python, Rust, Node.js, JVM, .NET, and additional runtimes on demand;
- GCC, Clang, LLVM, linkers, Make, CMake, Ninja, Meson, and build caching;
- GDB, LLDB, strace, ltrace, perf, bpftrace, Valgrind, and profiling tools;
- binary inspection and reverse engineering;
- system administration, storage, filesystems, networking, packet capture, DNS, TLS, and key tools;
- Docker, Podman, Buildah, Skopeo, Incus, QEMU/KVM, and optional libvirt;
- Kubernetes, Helm, Kustomize, OpenTofu, Terraform when needed, Ansible, Packer, and related tools;
- AWS, Google Cloud, Azure, hosting, edge, and platform CLIs as needed;
- database, queue, object-storage, and data clients;
- Playwright, Chromium, API clients, load tools, and web utilities;
- PDF, OCR, publishing, office, image, audio, video, and metadata tools;
- Graphviz, PlantUML, Mermaid, OpenSCAD, CAD, electronics, and 3D tools;
- mobile, Android, cross-compilation, firmware, and hardware tools when a real task requires them;
- security and supply-chain tools as optional capabilities, never mandatory gates.

### 14.2 Heavy tools

Large tools are installed after core HEC works and disk space is available.

Examples:

- Blender;
- LibreOffice;
- full TeX Live;
- Android SDK and emulator images;
- Ghidra;
- FreeCAD;
- KiCad;
- large database servers;
- multiple browser engines;
- large scientific runtimes;
- Kubernetes clusters and cached images.

Their installation remains one-command through checked-in shell recipes, not through an HEC package subsystem.

---

## 15. Actual initial host

The initial HEC host was measured as:

- Ubuntu 24.04;
- x86_64;
- Linux 6.8;
- 6 vCPUs;
- 11 GiB RAM plus zram;
- approximately 100 GB root disk on ext4;
- systemd 255;
- KVM available;
- Incus 6.0.6 available;
- Podman available;
- QEMU available;
- tmux available;
- Node.js 24 available;
- Python 3.12 available;
- Git available.

At design time, only about 17 GiB remained free because SEZU, Baby, Incus, and earlier construction state occupied most of the disk.

Therefore:

- build and prove HEC core first;
- do not install the full heavy forge before cutover;
- do not remove SEZU, Baby, SSH, or their data before HEC is independently reachable;
- remove obsolete construction state only after explicit StealthEye authorization.

---

## 16. Construction plan

The build is a sequence of usable vertical slices, not a governance process.

At every step:

- preserve the current working construction path;
- implement the smallest functional slice;
- invoke the real operation through the real ChatGPT connection;
- fix concrete failures;
- commit working code;
- continue.

### Slice 0 — repository foundation

Create:

```text
cmd/hec/main.go
internal/hec/
schemas/
systemd/
scripts/
skills/hec-operator/
capabilities/
forge/apt/
forge/recipes/
docs/
go.mod
.gitignore
Makefile or scripts/build.sh
```

First outcome:

```text
hec version
```

### Slice 1 — direct ChatGPT-to-root connection

- Install Go 1.26.2 under `/opt/hec/toolchains/go/1.26.2`.
- Implement embedded MCP server and Secure MCP Tunnel.
- Register `call_hec`.
- Implement:
  - `health`
  - `version`
  - `run`
- Install release under `/opt/hec/releases/`.
- Point `/opt/hec/current` to the release.
- Install root `hec.service`.
- Create a separate HEC tunnel and ChatGPT app while SEZU remains available.
- Execute real root calls from ChatGPT.

Once this works, HEC is independently alive.

### Slice 2 — durable jobs

Implement systemd-backed job operations and reproduce the failed-ChatGPT-turn case:

- start long job;
- disconnect or restart HEC after handle return;
- confirm job continues;
- reconnect;
- read status and output;
- repeat start with the same idempotency key;
- receive the same job.

This is functional job behavior, not a reliability framework.

### Slice 3 — files, uploads, and artifacts

Implement:

- direct file operations;
- chunked uploads;
- artifact creation;
- MCP resource links;
- artifact reads as fallback.

### Slice 4 — persistent terminals

Implement the tmux-backed terminal operations with exact-byte writes and screen/output reads.

### Slice 5 — capability discovery and skills

- Add HEC operator skill.
- Add skill operations.
- Add plain capability manifests.
- Add ordinary text-based discovery.

### Slice 6 — browser capability

- Install Playwright CLI and Chromium.
- Add Playwright skill.
- Use named sessions and persistent profiles.
- Return screenshots, downloads, traces, and videos as artifacts.

### Slice 7 — core forge

Preserve useful existing tools.

Install runtime managers:

```text
mise
uv
rustup
Corepack
```

Install broad base and extended packages through ordinary files and scripts.

Install Docker carefully alongside Podman and Incus.

Delay the heaviest packages until disk is reclaimed.

### Slice 8 — workspaces and repositories

Create:

```text
/srv/hec/workspaces
/srv/hec/repositories
/srv/hec/deliveries
```

Use normal Git worktrees.

Do not create a workspace controller.

### Slice 9 — HEC maintenance scripts

Provide:

```text
scripts/build.sh
scripts/install.sh
scripts/cutover.sh
```

No update daemon, automatic rollback, or fallback release manager.

### Final cutover

Cut over only after HEC can perform ordinary real use through ChatGPT:

```text
root command
long job and reconnect
persistent terminal
binary upload
artifact returned to ChatGPT
skill lookup
Playwright session
Git repository work
systemd and package operations
Incus or Podman operation
```

Cutover order:

1. ensure HEC has its own tunnel and ChatGPT app;
2. keep SSH available;
3. stop SEZU tunnel and supervisor;
4. confirm HEC remains connected;
5. stop Baby MCP and Baby;
6. leave old disk state untouched initially;
7. use HEC for real work;
8. delete obsolete construction state only after StealthEye explicitly approves;
9. install the broader forge with reclaimed space.

---

## 17. Expected HEC state and processes

HEC itself requires one permanent process:

```text
hec.service
```

Other services such as SSH, Caddy, Incus, Docker, databases, or application services are ordinary host software, not HEC subsystems.

HEC-created persistent state is limited to:

```text
jobs and output
terminal output logs
uploads in progress
returned artifacts
browser profiles and outputs
owner-installed skills
workspaces and repositories
```

HEC stores no:

- universal operation history;
- receipts;
- evidence;
- policy state;
- verification records;
- model reasoning;
- task ledger.

---

## 18. ChatGPT Project versus HEC

The ChatGPT Project holds:

- design discussion;
- project instructions;
- this source document;
- architectural decisions;
- user intent;
- synthesis and planning history.

HEC holds:

- actual files;
- repositories;
- processes;
- services;
- jobs;
- terminals;
- browser profiles;
- artifacts;
- installed tools;
- native machine state.

The Project provides conversational continuity.

The host provides operational truth.

When they disagree, inspect the real host and repository.

---

## 19. Rules for future ChatGPT work

### 19.1 Build now; do not reopen design casually

The broad research and architecture pass is complete.

Do not initiate another broad research pass before building.

Search or research only when:

- a concrete implementation question requires current documentation;
- an API or dependency has changed;
- a real build failure requires investigation;
- StealthEye explicitly asks for more research.

Do not add architecture because it might theoretically be useful.

### 19.2 Smallest-change rule

When a real failure occurs:

1. identify the concrete cause;
2. fix it using the smallest change;
3. avoid creating a general subsystem unless repeated real use proves it necessary;
4. preserve unrestricted raw execution;
5. commit the working result.

### 19.3 No scope drift

Do not turn HEC into:

- SEZU 2;
- an agent platform;
- a governance system;
- a reliability platform;
- a deployment platform;
- an orchestration product;
- a local AI system;
- a package ecosystem;
- a universal abstraction layer.

HEC is the thin, powerful connection between ChatGPT and a full root Linux workstation.

### 19.4 Canonical-change rule

Informal discussion does not alter the HEC design.

A canonical change requires clear StealthEye approval and a corresponding update to the repository documents and this Project source document.

---

## 20. Immediate next action

Begin the build at Slice 0 and Slice 1:

```text
repository foundation
-> Go toolchain
-> one root HEC binary
-> embedded MCP tunnel
-> hec.service
-> call_hec
-> health
-> version
-> unrestricted run
-> first real call from ChatGPT
```

Do not install the full forge first.

Do not add new architecture before the first unrestricted root command succeeds through HEC’s real ChatGPT connection.

---

## 21. Final definition

HEC is:

> A standalone, ChatGPT-native, owner-controlled Linux root workstation that exposes maximal professional capability through the thinnest practical MCP connection, while relying on Git, systemd, tmux, filesystems, package managers, containers, virtual machines, Playwright, and native CLIs instead of rebuilding them.

Its permanent design law is:

> **Maximum capability. Minimum bullshit.**
