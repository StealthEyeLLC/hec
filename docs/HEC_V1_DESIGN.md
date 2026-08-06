# HEC v1 Design

**Status:** v1 design frozen; build-ready.

HEC is a ChatGPT-native, unrestricted root workstation. It is intentionally broad in installed capability and intentionally small in runtime architecture.

> **Maximum capability. Minimum bullshit.**

## 1. Product contract

HEC gives ChatGPT direct use of an owner-controlled Linux host as root.

HEC is:

- a single persistent connection from ChatGPT to the host;
- unrestricted synchronous command execution;
- reconnectable long-running commands;
- reconnectable arbitrary terminals;
- binary-safe file transfer and returned artifacts;
- capability and skill discovery designed for ChatGPT;
- a host loaded with professional SWE, DevOps, systems, browser, data, document, media, cloud, networking, debugging, build, deployment, container, and VM tools.

HEC is not:

- a policy engine;
- an approval engine;
- a safety layer;
- an audit, receipt, evidence, or reporting system;
- a verification framework;
- a workflow platform;
- an automatic rollback system;
- a second package manager;
- a second process manager;
- a second container manager;
- a model server;
- a permanent forge container.

Git, systemd, the filesystem, package managers, tmux, Incus, Podman, Docker, QEMU, Playwright, and the other native tools remain authoritative in their own domains.

## 2. Actual machine boundary

The initial HEC host is:

- Ubuntu 24.04;
- x86_64;
- Linux 6.8;
- 6 vCPUs;
- 11 GiB RAM plus zram;
- 100 GB root disk on ext4;
- KVM available;
- Incus 6.0.6 available;
- Podman available;
- QEMU available;
- tmux available.

At design time only about 17 GiB remained free because the SEZU, Baby, Incus, and earlier construction state still occupied most of the disk. HEC core must therefore be built and proven first. Broader forge installation happens after HEC cutover and only after StealthEye explicitly authorizes removal of obsolete construction state.

## 3. One binary, one service

HEC is implemented in Go as one binary:

```text
/opt/hec/current/bin/hec
```

The same binary provides:

```text
hec serve
hec call
hec job-run
hec version
```

`hec serve` runs the MCP server and Secure MCP Tunnel in the same process.

The reviewed OpenAI tunnel client commit pinned below supports embedded operation and current `response_timeout` wire semantics. HEC owns a restartable in-memory MCP transport: each tunnel command connection receives a fresh transport pair and a fresh MCP server session. HEC therefore needs no local TCP listener, Unix socket, stdio child, gateway daemon, or proxy process.

```text
ChatGPT
   |
   | OpenAI Secure MCP Tunnel
   v
hec.service (root)
   |
   | in-memory MCP transport
   v
HEC operation dispatcher
   |
   +-- exec
   +-- systemd transient jobs
   +-- tmux terminals
   +-- files and artifacts
   +-- skills and capability manifests
   +-- every installed native CLI
```

### Initial pinned core dependencies

```text
Go                         1.26.2
openai/tunnel-client       v0.0.11-0.20260806014146-1bf01b0e1079
modelcontextprotocol/go-sdk v1.4.1
```

The tunnel dependency is pinned to the exact reviewed upstream commit `1bf01b0e1079b097b445a6fe5ddfc4048dd6fe45`. Dependency changes occur only when HEC itself is deliberately upgraded.


### Reliability and connection hardening

Public `call_hec` dispatch has one context-aware slot. The gate covers the complete handler lifecycle and has a ten-second acquisition maximum. Panic containment releases the slot, returns a stable internal error, and logs only a redacted function stack.

Each Secure MCP Tunnel client is one disposable generation. A generation owns its tunnel client, restartable MCP transport, connections, server sessions, contexts, and workers. A replacement is created only after the old generation is canceled, stopped, closed, and verified to have zero live workers. Readiness is bounded to 60 seconds, tunnel stop to 5 seconds, and complete generation cleanup to 10 seconds. Cleanup timeout is process-fatal so systemd restarts HEC rather than allowing generation overlap. Reconnect backoff is 250 milliseconds, 500 milliseconds, 1 second, 2 seconds, 5 seconds, then a 10-second cap with bounded jitter.

Fixed synchronous budgets are:

```text
MaxDirectCall            90 seconds
MaxJobWait               15 seconds
ResponseDeliveryReserve  10 seconds
CallGateAcquireTimeout   10 seconds
GenerationReadyTimeout   60 seconds
TunnelStopTimeout         5 seconds
GenerationCleanupTimeout 10 seconds
```

HEC may retain minimal, bounded, internal keyed mutation state solely to prevent duplicate native side effects after ambiguous ChatGPT or tunnel delivery. This state does not create a public receipt API, audit log, evidence system, workflow ledger, universal history, or generalized command cache.

## 4. One public MCP tool

HEC exposes exactly one stable MCP tool:

```text
call_hec
```

Input:

```json
{
  "operation": "run",
  "args": {},
  "idempotency_key": "optional; only meaningful where documented"
}
```

JSON Schema shape:

```json
{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "minLength": 1
    },
    "args": {
      "type": "object",
      "additionalProperties": true
    },
    "idempotency_key": {
      "type": "string",
      "minLength": 1,
      "maxLength": 512
    }
  },
  "additionalProperties": false
}
```

Reasons for one tool:

- ChatGPT uses a frozen snapshot of custom MCP actions and schemas until an administrator refreshes or republishes the app.
- HEC can add commands, skills, recipes, and operation metadata without changing the public MCP action set.
- ChatGPT never has to choose among dozens or hundreds of overlapping public tools.
- Raw unrestricted execution remains obvious and permanent.

The operation registry is dynamic behind the stable tool. `capabilities` exposes the current catalog.

## 5. Result envelope

Every operation returns structured MCP content conforming to one stable output schema. It also returns a short text block for clients that display text better than structured content.

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

Fields:

- `ok`: false for rejected requests, internal failures, timeouts, and non-zero command exits.
- `status`: `completed`, `running`, `failed`, `timed_out`, or `cancelled`.
- `handle`: an opaque job, terminal, upload, stream, or artifact handle when applicable.
- `exit_code` and `signal`: native process outcome.
- `stdout` and `stderr`: inline output when reasonably sized.
- `*_encoding`: `utf8` or `base64`.
- `truncated`: indicates that the caller should use the associated output operation or redirect output to a file.
- `result`: operation-specific structured data.
- `resources`: MCP resource links or embedded content for returned files, images, audio, and other artifacts.
- `error`: only `{ "code": "...", "message": "..." }` plus optional native detail.

There is no receipt ID, evidence record, verification status, policy decision, or audit payload.

## 6. Core operation catalog

The core contains only functionality that raw shell execution cannot expose cleanly through an MCP request.

### Introspection

```text
health
version
capabilities
```

`capabilities` searches:

- built-in HEC operations;
- plain capability manifests;
- installed Agent Skills metadata;
- available install recipes;
- optionally named commands on `PATH`.

It does not enforce capabilities or restrict execution.

### Direct execution

```text
run
```

Arguments:

```json
{
  "command": "optional shell command",
  "argv": ["optional", "direct", "argv"],
  "cwd": "/optional/path",
  "env": {"NAME": "value"},
  "unset_env": ["NAME"],
  "stdin": "optional UTF-8 input",
  "stdin_base64": "optional binary input",
  "timeout_ms": 90000,
  "max_output_bytes": 1048576
}
```

Rules:

- exactly one of `command` and `argv` is supplied;
- `command` runs through `/bin/bash -lc`;
- `argv` uses direct process execution;
- root is the default and unrestricted identity;
- there is no command allowlist, denylist, approval pass, or preview pass;
- synchronous execution is for work expected to finish during the current tool call;
- long work uses `job.start`;
- output is returned exactly, except for explicit size truncation;
- invalid UTF-8 output is base64 encoded.

### Durable noninteractive jobs

```text
job.start
job.status
job.output
job.wait
job.signal
job.list
job.forget
```

A job is a noninteractive command owned by a transient systemd service:

```text
hec-job-<id>.service
```

`job.start` accepts the same command, argv, cwd, environment, stdin-file, and timeout concepts as `run`. It returns immediately with:

```text
job:<id>
```

Implementation:

1. HEC creates `/var/lib/hec/jobs/<id>/`.
2. It writes a minimal command specification.
3. It launches the same HEC binary as `hec job-run <spec>` in a transient systemd service with `Type=exec`.
4. stdout and stderr append directly to files in the job directory.
5. the runner writes a small final result file containing only native exit information.
6. status comes from systemd while active and the result file after completion.

Output is read by byte offset:

```json
{
  "handle": "job:abc",
  "stream": "stdout",
  "offset": 0,
  "limit": 262144
}
```

Jobs do not accept continuing interactive stdin. Interactive programs use terminals.

An optional `idempotency_key` may be supplied for a native mutation. HEC binds the key to the normalized operation and arguments. A completed matching request replays its bounded prior result; a different request conflicts; an orphaned ambiguous mutation reports uncertainty instead of running again. `job.start` additionally records the allocated job ID and exact systemd unit before launch, so an ambiguous `systemd-run` response can be resolved without creating a second unit.

No automatic retry occurs except a bounded retry of the same exact preallocated `job.start` unit after native inspection proves that unit absent.

### Persistent terminals

```text
terminal.open
terminal.list
terminal.read
terminal.write
terminal.resize
terminal.signal
terminal.close
```

Terminals use a dedicated tmux server under `/run/hec/`.

A terminal may run:

- Bash or another shell;
- a debugger;
- a REPL;
- an installer;
- an SSH session;
- a database console;
- any other PTY program.

`terminal.read` supports:

- current visible screen via `capture-pane`;
- accumulated output by byte offset from a `pipe-pane` log.

`terminal.write` loads exact bytes into a tmux buffer and pastes the buffer, avoiding shell-escaping corruption.

Tmux terminals survive ChatGPT and HEC service reconnects as long as the tmux server remains alive. They are not advertised as surviving a host reboot.

### Files and uploads

```text
file.stat
file.list
file.read
file.write
file.append
file.patch
file.remove
upload.begin
upload.chunk
upload.finish
upload.abort
```

HEC accepts absolute paths everywhere. There is no artificial workspace jail.

File reads support byte offset and length. Binary output uses base64.

File writes support UTF-8 and base64 content. Complete replacements are written through a temporary sibling and renamed into place because this is simpler and less failure-prone than leaving partial files. This behavior is invisible and does not introduce approvals or verification.

`file.patch` accepts a unified diff and applies it with native Git or patch tooling.

Chunked upload exists because large binary data should not be forced into one MCP request.

Everything else—copy, move, find, rsync, rclone, archive, extraction, permissions, ownership, ACLs, attributes, mounts, and filesystem operations—remains directly available through `run` and normal tools.

### Artifacts returned to ChatGPT

```text
artifact.return
artifact.stat
artifact.read
artifact.materialize
artifact.list
artifact.delete
```

Artifacts are deliberately simple:

- a copied immutable file; or
- a deterministic `.tar.zst` bundle when the source is a directory.

They live under `/var/lib/hec/artifacts/` and are readable by byte range.

The MCP result returns a resource link when the connected ChatGPT client supports it. `artifact.read` is the universal fallback.

HEC does not implement a content-addressed store, tree database, reference collector, registry, or garbage-collection service in v1.

### Skills

```text
skill.list
skill.find
skill.read
```

HEC skills use the Agent Skills structure:

```text
skill-name/
  SKILL.md
  agents/openai.yaml        optional
  scripts/                  optional
  references/               optional
  assets/                   optional
```

Skill roots:

```text
/opt/hec/current/skills
/etc/hec/skills
/srv/hec/workspaces/<name>/.hec/skills
```

Skills contain instructions and reusable scripts. They are not a permission or workflow system. ChatGPT reads the relevant skill and invokes its scripts or commands through normal HEC execution.

## 7. Capability discovery

HEC must be broad without dumping every installed binary into every ChatGPT turn.

Capability discovery is plain-file based:

```text
/opt/hec/current/capabilities/*.toml
/opt/hec/current/skills/*/SKILL.md
/opt/hec/current/forge/recipes/*
/etc/hec/skills/*
<workspace>/.hec/skills/*
```

A capability manifest may say:

```toml
id = "browser.playwright"
description = "Persistent browser automation for coding and web tasks"
tags = ["browser", "web", "test", "screenshot"]
commands = ["playwright-cli"]
skills = ["playwright-cli"]
recipe = "browser-playwright"
```

`capabilities` performs ordinary metadata and text matching. It returns only the small relevant slice.

There is no embedding server, vector database, capability graph database, planner, or automatic tool installer.

## 8. Browser design

HEC does not implement a custom browser protocol in v1.

It installs the official Playwright CLI for coding agents plus its Agent Skills. The Playwright CLI is specifically designed to be token-efficient, returns accessibility snapshots with stable element references, supports named sessions, persistent disk profiles, screenshots, network inspection, tracing, video, uploads, storage-state save/load, and arbitrary Playwright scripts.

ChatGPT operates it through `run` or a persistent terminal.

Default layout:

```text
/var/lib/hec/browser/profiles/<name>/
/var/lib/hec/browser/output/<name>/
```

Default browser: Chromium.

Firefox and WebKit are installed when needed.

Because HEC operates as root, Chromium may run with its sandbox disabled. HEC adds no browser-origin allowlists, deny lists, or proxy restrictions.

Generated screenshots, traces, videos, downloads, and snapshots are returned through normal HEC file or artifact operations.

## 9. Native systems remain native

HEC does not wrap these domains in large custom APIs:

| Domain | Native authority |
|---|---|
| source history and parallel branches | Git and Git worktrees |
| long processes | systemd transient services |
| interactive processes | tmux |
| services and logs | systemd and journald |
| packages | apt and ecosystem-native managers |
| system containers and VMs | Incus |
| OCI application containers | Podman and Docker |
| virtual machines | KVM/QEMU and optionally libvirt |
| browser automation | Playwright CLI |
| cloud services | official cloud CLIs and APIs |
| files and storage | Linux filesystems and ordinary tools |
| networking | iproute2, nftables, tcpdump, tshark, and native services |
| project environments | project files, mise, uv, rustup, and native ecosystem files |

HEC gives ChatGPT direct access to those tools and ships skills that teach efficient usage.

## 10. Workspaces

A workspace is a convention, not a controller:

```text
/srv/hec/workspaces/<name>/
  repository/
  .hec/
    workspace.toml      optional defaults and notes
    skills/             optional project skills
    scratch/            optional disposable material
```

Raw paths always work. A workspace may define default `cwd`, environment additions, repository path, and skill hints. HEC never attempts to reconcile the machine to a declared workspace state.

Git remains project memory. Normal project files such as `mise.toml`, `pyproject.toml`, `package.json`, `go.mod`, `Cargo.toml`, `Dockerfile`, Compose files, Terraform/OpenTofu files, Ansible files, and CI definitions remain the source of truth.

## 11. Credentials

HEC uses native credential locations:

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

HEC has no credential broker or secret database.

The Secure MCP Tunnel credentials are supplied to `hec.service` through a root-readable environment file:

```text
/etc/hec/tunnel.env
```

Expected variables:

```text
CONTROL_PLANE_TUNNEL_ID=...
CONTROL_PLANE_API_KEY=...
```

## 12. Filesystem layout

```text
/opt/hec/
  releases/<version>/
    bin/hec
    skills/
    capabilities/
    forge/
  current -> releases/<version>
  toolchains/

/etc/hec/
  hec.toml                 optional overrides
  hec.env                  ordinary environment
  tunnel.env               tunnel ID and runtime API key
  skills/                  owner-installed global skills

/srv/hec/
  workspaces/
  repositories/
  deliveries/

/var/lib/hec/
  jobs/
  artifacts/
  uploads/
  browser/

/var/cache/hec/
  downloads/
  sources/
  packages/
  playwright/

/run/hec/
  tmux.sock
```

No database is required.

## 13. Configuration

HEC has useful compiled defaults. `/etc/hec/hec.toml` is optional.

```toml
protocol = "HEC1/1.0.0"
default_cwd = "/root"
shell = ["/bin/bash", "-lc"]
max_inline_output_bytes = 1048576
state_dir = "/var/lib/hec"
cache_dir = "/var/cache/hec"
workspace_dir = "/srv/hec/workspaces"
artifact_dir = "/var/lib/hec/artifacts"
tmux_socket = "/run/hec/tmux.sock"
skill_roots = [
  "/opt/hec/current/skills",
  "/etc/hec/skills"
]
```

HEC starts without this file.

## 14. systemd unit

```ini
[Unit]
Description=HEC ChatGPT-native root workstation
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
EnvironmentFile=-/etc/hec/hec.env
EnvironmentFile=/etc/hec/tunnel.env
Environment=PATH=/opt/hec/current/bin:/opt/hec/bin:/root/.local/bin:/root/.cargo/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
RuntimeDirectory=hec
StateDirectory=hec
CacheDirectory=hec
WorkingDirectory=/root
ExecStart=/opt/hec/current/bin/hec serve
Restart=always
RestartSec=1
TimeoutStopSec=15
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
```

There are deliberately no systemd sandboxing or hardening directives that would restrict root capability.

HEC logs to journald. It adds no logging stack, metrics stack, tracing stack, support bundle, or administrative UI.

## 15. Repository structure

```text
cmd/hec/main.go
internal/hec/app.go
internal/hec/config.go
internal/hec/mcp.go
internal/hec/registry.go
internal/hec/result.go
internal/hec/exec.go
internal/hec/jobs.go
internal/hec/terminals.go
internal/hec/files.go
internal/hec/uploads.go
internal/hec/artifacts.go
internal/hec/skills.go
internal/hec/capabilities.go
internal/hec/tunnel.go
internal/hec/job_runner.go
schemas/call-hec.input.json
schemas/call-hec.output.json
systemd/hec.service
scripts/build.sh
scripts/install.sh
scripts/cutover.sh
skills/hec-operator/SKILL.md
capabilities/*.toml
forge/apt/base.txt
forge/apt/extended.txt
forge/recipes/*.sh
docs/
go.mod
go.sum
README.md
```

The implementation stays in one internal package until code size proves that further package boundaries help.

## 16. Local use

The local CLI calls the same registry directly without MCP:

```bash
hec version
hec call health '{}'
hec call run '{"argv":["uname","-a"]}'
```

This makes HEC independently useful and allows construction or diagnosis even when ChatGPT or the tunnel is unavailable.

## 17. Upgrades

HEC releases are immutable directories:

```text
/opt/hec/releases/<version>
```

Activation is one symlink replacement:

```text
/opt/hec/current
```

Then:

```bash
systemctl restart hec
```

This is ordinary release management. HEC does not implement automatic rollback or a release controller.

## 18. What is intentionally absent

HEC v1 has no:

- permanent forge container;
- internal model;
- database;
- policy or approval system;
- audit trail or receipt store;
- verification hooks;
- automatic preflight;
- automatic retries;
- automatic rollback;
- workflow DAG engine;
- task ledger;
- scheduler daemon;
- content-addressed storage system;
- target abstraction;
- custom browser server;
- custom Git API;
- custom container API;
- custom cloud API;
- package abstraction;
- mandatory isolation.

All of those capabilities are either unnecessary or already available through native tools.

## 19. Research basis

Primary references used for this design:

- OpenAI Secure MCP Tunnel: https://github.com/openai/tunnel-client
- ChatGPT developer mode and MCP apps: https://help.openai.com/en/articles/12584461-developer-mode-and-full-mcp-connectors-in-chatgpt-beta
- MCP tools and structured results: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- Official MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk
- systemd transient services: https://www.man7.org/linux/man-pages/man1/systemd-run.1.html
- tmux: https://github.com/tmux/tmux/wiki/Getting-Started
- Playwright CLI for coding agents: https://playwright.dev/agent-cli/introduction
- Agent Skills: https://github.com/openai/skills
- Git worktrees: https://git-scm.com/docs/git-worktree
- Incus containers and VMs: https://linuxcontainers.org/incus/docs/main/explanation/containers_and_vms/
- Podman: https://docs.podman.io/en/latest/
- Docker Engine on Ubuntu: https://docs.docker.com/engine/install/ubuntu/
- mise: https://mise.jdx.dev/
- uv: https://docs.astral.sh/uv/
- rustup: https://rust-lang.github.io/rustup/
