# HEC v1 Build Plan

**Status:** v1 construction plan frozen; ready to execute after explicit authorization.

This is an execution order, not a governance process. Each slice ends with a directly usable HEC capability. No phase adds receipts, evidence, mandatory verification, approval, rollback, or policy machinery.

## 1. Build start gate

This plan is frozen, but execution does not begin until StealthEye explicitly instructs ChatGPT to start building HEC.

Prebuild document edits, repository planning commits, and source synchronization are allowed before that instruction. Installing packages, compiling HEC, creating services, provisioning the HEC tunnel, or changing active infrastructure are build actions and remain out of scope until the start instruction.

## 2. Build rule

At every point:

- preserve the current working construction path;
- implement the smallest functional vertical slice;
- call the real operation through the real ChatGPT connection;
- fix actual failures;
- commit working code;
- continue.

SEZU and Baby may perform construction work. They are never linked, imported, called, or required by the finished HEC runtime.

## 3. Construction boundary

Initial source repository:

```text
StealthEyeLLC/hec
```

Initial host:

```text
Ubuntu 24.04 x86_64
6 vCPU
11 GiB RAM
100 GB disk
systemd 255
```

Existing construction lifelines remain active until HEC independently works through its own Secure MCP Tunnel:

```text
Baby
Baby MCP
SEZU supervisor
SEZU tunnel
SSH
```

## 4. Slice 0 — repository foundation

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
```

Add:

```text
go.mod
.gitignore
LICENSE decision
Makefile or simple scripts/build.sh
```

Core module dependencies:

```text
github.com/openai/tunnel-client v0.0.11-0.20260806014146-1bf01b0e1079
github.com/modelcontextprotocol/go-sdk v1.4.1
```

The first commit should compile a `hec version` binary locally.

## 5. Slice 1 — direct ChatGPT-to-root connection

### 5.1 Install the HEC Go toolchain

Install Go 1.26.2 under:

```text
/opt/hec/toolchains/go/1.26.2
```

Expose it only to the HEC build scripts initially:

```text
GOROOT=/opt/hec/toolchains/go/1.26.2
PATH=$GOROOT/bin:$PATH
```

Do not replace Ubuntu's packages or existing Node/Python toolchains.

### 5.2 Implement the in-process MCP tunnel

In `hec serve`:

1. create a disposable tunnel generation context;
2. create a restartable HEC-owned MCP transport;
3. construct a fresh tunnel client pinned to the reviewed upstream commit;
4. wait at most 60 seconds for readiness;
5. serve until root cancellation or generation failure;
6. cancel the generation, stop the client within 5 seconds, close every connection and server session, and wait at most 10 seconds for zero workers;
7. create a replacement only after complete cleanup;
8. exit for systemd restart if cleanup cannot complete, rather than overlap generations.

Every transport `Connect` creates a fresh `mcp.NewInMemoryTransports()` pair and fresh MCP server session. The caller context is carried into that session, and closing the client connection cancels and closes the matching session. No closed pair is reused.

The tunnel client is pinned to upstream commit `1bf01b0e1079b097b445a6fe5ddfc4048dd6fe45`, which implements poll-receipt-based `response_timeout`, command deadline propagation, expired-command dropping, and late-response suppression.

### 5.3 Implement the first operations

```text
health
version
run
```

`run` initially needs:

- `command` or `argv`;
- `cwd`;
- environment additions/removals;
- UTF-8 or base64 stdin;
- timeout;
- bounded stdout and stderr;
- UTF-8 or base64 output;
- exit code and signal.

No job system, files subsystem, or skills are required for the first connection.

### 5.4 Install the first release

```text
/opt/hec/releases/0.0.1/bin/hec
/opt/hec/current -> releases/0.0.1
/etc/hec/tunnel.env
/etc/systemd/system/hec.service
```

The first `hec.service` runs as root and embeds the tunnel.

### 5.5 Create a separate OpenAI tunnel and ChatGPT app

Use a new HEC tunnel ID and runtime API key so SEZU remains reachable during construction.

Publish one custom MCP action:

```text
call_hec
```

Do not reuse the SEZU app name or operation catalog.

### 5.6 First real calls

From ChatGPT:

```text
health
version
run -> id
run -> uname -a
run -> systemctl status hec
```

Once those calls succeed, HEC is independently alive.

## 6. Slice 2 — durable jobs

Implement:

```text
job.start
job.status
job.output
job.wait
job.signal
job.list
job.forget
```

### 6.1 Job directory

```text
/var/lib/hec/jobs/<id>/
  spec.json
  stdout
  stderr
  result.json       written only when the runner exits
```

This is process metadata, not an audit record.

### 6.2 Job runner

`hec job-run <spec>`:

1. loads argv or shell command, cwd, env, optional input path, and optional timeout;
2. starts the child in its own process group;
3. forwards termination signals to that group;
4. waits for the child;
5. writes native exit code or signal to `result.json`;
6. exits with the same outcome.

### 6.3 systemd launch

Launch a transient service similar to:

```bash
systemd-run \
  --unit=hec-job-<id> \
  --property=Type=exec \
  --property=KillMode=mixed \
  --property=StandardOutput=append:/var/lib/hec/jobs/<id>/stdout \
  --property=StandardError=append:/var/lib/hec/jobs/<id>/stderr \
  /opt/hec/current/bin/hec job-run /var/lib/hec/jobs/<id>/spec.json
```

Do not use `--collect` initially. HEC can inspect the unit while systemd retains it and can use `result.json` afterward.

### 6.4 Minimal keyed mutation state

When `idempotency_key` is supplied for a mutation, HEC stores a bounded internal record under:

```text
/var/lib/hec/mutation-keys/<sha256-key>.json
```

The only states are missing, `in_progress`, and `completed`. The record binds the key to a canonical hash of the normalized operation and arguments. `in_progress` is durably published before the native effect and `completed` before returning. Matching completed requests replay; changed requests conflict; orphaned ambiguous effects report `uncertain_prior_execution`.

For `job.start`, the allocated job ID and exact systemd unit are recorded before launch. Lost `systemd-run` responses are resolved by inspecting that exact unit, and a clearly absent unit may be retried only with the same exact unit. Existing v0.0.11 job-key files are migrated only after the durable job specification reconstructs the same normalized request.

This is internal duplicate-side-effect suppression, not a receipt, audit, evidence, history, workflow, or generalized cache feature.

### 6.5 Reproduce the ChatGPT failure case

1. start a command that runs for several minutes;
2. disable or restart only `hec.service` after the job handle is returned;
3. confirm the transient systemd job continues;
4. restart HEC;
5. read the same job status and output;
6. repeat the start with the same key and get the same handle.

This is ordinary functional use of the job API, not a reliability framework.

## 7. Slice 3 — files, uploads, and returned artifacts

### 7.1 File operations

Implement:

```text
file.stat
file.list
file.read
file.write
file.append
file.patch
file.remove
```

Use native filesystem APIs. Accept absolute paths.

### 7.2 Chunked upload

Implement:

```text
upload.begin
upload.chunk
upload.finish
upload.abort
```

State:

```text
/var/lib/hec/uploads/<id>/data
```

`chunk` writes decoded bytes at the requested offset. `finish` renames the completed file to an absolute destination or promotes it to an artifact.

No upload database is needed.

### 7.3 Artifacts

Implement:

```text
artifact.return
artifact.stat
artifact.read
artifact.materialize
artifact.list
artifact.delete
```

Artifact layout:

```text
/var/lib/hec/artifacts/<id>/
  metadata.json
  <filename>
```

Directories are converted with native `tar` plus `zstd` into one file.

Register an MCP resource handler for:

```text
hec://artifact/<id>
```

Return resource links from tool results. Keep `artifact.read` for clients that do not expose resource links usefully.

## 8. Slice 4 — persistent arbitrary terminals

Implement the terminal operations with a dedicated tmux socket:

```text
/run/hec/tmux.sock
```

### 8.1 Create

`terminal.open`:

- assigns an ID and optional human name;
- creates a detached tmux session;
- starts the caller's command or a Bash shell;
- enables `pipe-pane` into `/var/lib/hec/terminals/<id>/output`.

### 8.2 Read

Support:

```text
mode=screen
mode=output with byte offset
```

Screen mode uses `capture-pane -p -e -J`.

### 8.3 Write

For exact text or bytes:

1. write a temporary tmux buffer file;
2. `tmux load-buffer`;
3. `tmux paste-buffer`;
4. delete the temporary buffer.

This avoids quoting and `send-keys` interpretation problems.

### 8.4 Control

Implement resize, interrupt, signal, kill, and delete with ordinary tmux and process commands.

## 9. Slice 5 — ChatGPT capability discovery and skills

### 9.1 Agent Skill structure

Create the built-in HEC operator skill:

```text
skills/hec-operator/
  SKILL.md
  agents/openai.yaml
  references/operations.md
  references/forge.md
```

The skill teaches ChatGPT:

- direct exec versus durable job versus terminal;
- file and artifact transfer;
- how to query capabilities;
- how to use Playwright CLI;
- how to use Git worktrees;
- how to operate native tools without expecting wrappers.

### 9.2 Skill operations

Implement:

```text
skill.list
skill.find
skill.read
```

Read only metadata until ChatGPT asks for the full skill. This keeps model context small.

### 9.3 Capability manifests

Add plain TOML manifests and recipe metadata.

`capabilities` searches them using ordinary text matching. It may also check named commands with `exec.LookPath`.

No semantic database or indexing service.

## 10. Slice 6 — browser capability

Install Node prerequisites if not already present, then:

```bash
npm install -g @playwright/cli@latest
playwright-cli install --skills
playwright-cli install-browser --with-deps
```

Record the resolved CLI version in the forge files after the install.

Add a HEC Playwright skill that standardizes:

```text
PLAYWRIGHT_CLI_SESSION=<workspace-or-task-name>
PLAYWRIGHT_MCP_OUTPUT_DIR=/var/lib/hec/browser/output/<name>
```

Use:

```text
--persistent
--profile=/var/lib/hec/browser/profiles/<name>
```

HEC does not implement browser actions in Go. ChatGPT calls `playwright-cli` through execution or a persistent terminal and returns screenshots/downloads/traces through file or artifact operations.

## 11. Slice 7 — core forge

### 11.1 Preserve existing useful tools

Keep:

```text
Incus
Podman
Skopeo
QEMU/KVM
tmux
Node.js 24
Python 3.12
Git
Caddy
```

### 11.2 Install runtime managers

Install:

```text
mise
uv
rustup
Corepack
```

Configure root environment:

```text
/root/.local/bin
/root/.cargo/bin
```

Set:

```text
MISE_TRUSTED_CONFIG_PATHS=/
```

### 11.3 Install base apt set

Use `forge/apt/base.txt` and one ordinary command:

```bash
apt-get update
xargs -a forge/apt/base.txt apt-get install -y
```

The file contains one package name per line with comments removed by the script.

### 11.4 Install Docker carefully

The current host already has Podman, Incus, QEMU, and container-related packages. Before installing official Docker Engine, inspect installed `containerd`, `runc`, and `podman-docker` packages because Docker's official packages may conflict with them.

Then use Docker's official Ubuntu repository and install:

```text
docker-ce
docker-ce-cli
containerd.io
docker-buildx-plugin
docker-compose-plugin
```

Do not remove Podman or Incus merely because Docker is installed.

### 11.5 Install professional extended tools

Install the extended layer from `forge/apt/extended.txt`, then use recipes for upstream-only tools.

Do not install the heaviest desktop/media/CAD/mobile packages until disk reclamation is complete.

## 12. Slice 8 — workspaces and repositories

Create:

```text
/srv/hec/workspaces
/srv/hec/repositories
/srv/hec/deliveries
```

Add workspace discovery to `capabilities`, but no workspace controller.

Recommended repository arrangement:

```text
/srv/hec/repositories/<repo>.git       optional bare/shared repository
/srv/hec/workspaces/<project>/main
/srv/hec/workspaces/<project>/worktrees/<branch>
```

Use normal `git worktree add` for parallel work.

## 13. Slice 9 — HEC's own maintenance operations

HEC needs no custom update daemon.

Provide scripts:

```text
scripts/build.sh
scripts/install.sh
scripts/cutover.sh
```

`build.sh`:

- builds the binary with the pinned Go toolchain;
- writes it into a new release directory.

`install.sh`:

- creates HEC directories;
- installs the unit file;
- creates the current symlink if absent;
- reloads systemd;
- starts HEC.

`cutover.sh`:

- switches `/opt/hec/current` to a specified release;
- restarts HEC.

No automatic fallback release or rollback behavior.

## 14. Final cutover

HEC is ready to replace the construction systems when all of the following ordinary uses work from ChatGPT:

```text
root command
long job and reconnect
persistent terminal
binary file upload
artifact returned to ChatGPT
skill lookup
Playwright browser session
Git repository work
systemd and package operations
Incus or Podman operation
```

This list is not a verification subsystem. It is simply the capability boundary that makes HEC independently useful.

Cutover order:

1. ensure HEC has its own tunnel and ChatGPT app;
2. keep SSH available;
3. stop SEZU tunnel and supervisor;
4. confirm HEC remains connected;
5. stop Baby MCP and Baby;
6. leave all old disk state untouched initially;
7. use HEC for real work;
8. only after StealthEye explicitly approves, delete obsolete services, releases, state directories, storage images, and old construction worktrees;
9. install the broader HEC forge with the reclaimed disk.

## 15. Expected permanent processes

HEC itself requires one permanent service:

```text
hec.service
```

Other permanent services are normal host software chosen for actual use, such as:

```text
sshd
Caddy
Incus
Docker
```

HEC does not claim ownership of them or classify them as HEC subsystems.

## 16. Expected HEC state

HEC-created persistent state is limited to:

```text
jobs and their output
terminal output logs
uploads in progress
returned artifacts
browser profiles and outputs
owner-installed skills
workspaces and repositories
```

No operation history, receipt database, policy state, verification records, or model reasoning is stored.

## 17. Direct implementation decisions

- Language: Go.
- Root execution: yes, default and unrestricted.
- Primary target: the host.
- Public MCP tools: one, `call_hec`.
- HEC services: one, `hec.service`.
- Tunnel: official OpenAI tunnel-client embedded in-process.
- MCP: official Go SDK, in-memory transport.
- Database: none.
- Job manager: systemd transient services.
- Terminal manager: tmux.
- File authority: native filesystem.
- Browser: Playwright CLI and skills.
- Containers: native Incus, Podman, Docker CLIs.
- VMs: native Incus and QEMU/KVM.
- Project history: Git.
- Runtime versions: normal project files plus mise, uv, rustup, and ecosystem managers.
- Logs: journald plus job and terminal output files.
- Safety, approvals, receipts, evidence, verification framework, rollback, policy, audit: none.

## 18. Research basis

Primary references:

- OpenAI tunnel-client pinned reviewed commit and restartable in-memory embedding: https://github.com/openai/tunnel-client
- ChatGPT custom MCP behavior: https://help.openai.com/en/articles/12584461-developer-mode-and-full-mcp-connectors-in-chatgpt-beta
- MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk
- MCP tool structured results: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- systemd transient units and `Type=exec`: https://www.man7.org/linux/man-pages/man1/systemd-run.1.html
- tmux detach and reattach: https://github.com/tmux/tmux/wiki/Getting-Started
- Playwright CLI for coding agents: https://playwright.dev/agent-cli/introduction
- Agent Skills: https://github.com/openai/skills
- Git worktrees: https://git-scm.com/docs/git-worktree
