# HEC operation guide

Use this guide to select among the 38 explicit `call_hec` operation branches. Arguments named below are the essential selectors, not a replacement for the JSON Schema.

## Synchronous execution

| Operation | Select it when | Essential arguments and returned behavior |
|---|---|---|
| `run` | One noninteractive process should finish in the current turn. | Supply exactly one of `command` or `argv`; optionally set `cwd`, environment, stdin, timeout, and output limit. Returns inline stdout/stderr and process outcome, not a handle. |

## Durable jobs

| Operation | Select it when | Essential arguments and returned behavior |
|---|---|---|
| `job.start` | Work may run beyond the turn or must survive ChatGPT interruption. | Supply `command` or `argv`; optional execution settings and top-level `idempotency_key`. Returns a durable `job:` handle. |
| `job.status` | Check one durable job without waiting. | Supply `handle`. Returns current job status and process outcome when known. |
| `job.output` | Read durable stdout or stderr incrementally. | Supply `handle`, `stream`, optional `offset` and `limit`. Persist `next_offset` for the next read. |
| `job.wait` | Wait bounded time for state change or completion. | Supply `handle` and optional timeout. Returns job status and whether the wait itself timed out. |
| `job.signal` | Send a supported signal to a running job. | Supply `handle` and `signal`. The same handle remains authoritative. |
| `job.list` | Recover handles after an interrupted turn or inspect retained jobs. | Optional pagination/filter arguments. Returns retained job metadata; do not forget unrelated jobs. |
| `job.forget` | Remove one completed dedicated job from HEC state. | Supply `handle`. Forget only state you own and no longer need. |

## Files and transfers

| Operation | Select it when | Essential arguments and returned behavior |
|---|---|---|
| `file.stat` | Read metadata for one path. | Supply `path`. Returns type, size, mode, and timestamps without a handle. |
| `file.list` | List a directory deterministically. | Supply `path`; optional pagination. Persist `next_offset` until `eof`. |
| `file.read` | Read exact bounded bytes from a file. | Supply `path`; optional `offset` and `limit`. Persist `next_offset`; binary data may be base64 encoded. |
| `file.write` | Atomically create or replace one file. | Supply `path` and exactly one of UTF-8 `content` or `content_base64`. Returns bytes written. |
| `file.append` | Append exact bytes to one file. | Supply `path` and exactly one content form. Returns bytes appended. |
| `file.patch` | Apply a unified diff in a working directory. | Supply `cwd` and `patch`. Returns patch outcome; no persistent handle. |
| `file.remove` | Remove dedicated file or directory state. | Supply `path` and required removal options. Avoid unrelated state. |
| `upload.begin` | Start a chunked inbound transfer. | Supply a safe `filename`. Returns an `upload:` handle and initial offset. |
| `upload.chunk` | Add binary-safe bytes to an upload. | Supply `handle`, exact `offset`, and `data_base64`. Use the returned next offset; retries at the same offset are replay-safe. |
| `upload.finish` | Commit an upload to a destination or artifact. | Supply `handle` and exactly one destination form. Repeating a successful finish is replay-safe and returns the same completion. |
| `upload.abort` | Remove an incomplete dedicated upload. | Supply `handle`. Do not abort unrelated uploads. |

## Artifacts

| Operation | Select it when | Essential arguments and returned behavior |
|---|---|---|
| `artifact.return` | Return a host file or directory to ChatGPT. | Supply `path`; optional name/media metadata. Returns an immutable `artifact:` handle and MCP resource link. |
| `artifact.stat` | Inspect one returned artifact. | Supply `handle`. Returns artifact metadata. |
| `artifact.read` | Read artifact bytes incrementally. | Supply `handle`; optional `offset` and `limit`. Persist `next_offset`. |
| `artifact.materialize` | Copy an artifact back to an explicit host path. | Supply `handle` and absolute `destination`. Returns the destination. |
| `artifact.list` | List retained artifacts. | Optional pagination. Persist returned offsets when present. |
| `artifact.delete` | Delete one dedicated retained artifact. | Supply `handle`. Do not delete unrelated artifacts. |

## Persistent terminals

| Operation | Select it when | Essential arguments and returned behavior |
|---|---|---|
| `terminal.open` | Start a stateful interactive tmux-backed terminal. | Optional command/argv, name, cwd, environment, width, and height. Returns a persistent `terminal:` handle. |
| `terminal.list` | Recover or inspect terminal handles. | Empty args. Returns live terminal metadata. |
| `terminal.read` | Read current screen or accumulated output. | Supply `handle` and `mode`; output mode accepts `offset` and `limit`. Persist `next_offset`. |
| `terminal.write` | Send exact input to a terminal. | Supply `handle` and exactly one of `data` or `data_base64`. Returns bytes written. |
| `terminal.resize` | Change terminal geometry. | Supply `handle`, `width`, and `height`. The handle stays the same. |
| `terminal.signal` | Send a supported signal to the terminal process group. | Supply `handle` and `signal`. |
| `terminal.close` | Close one dedicated persistent terminal. | Supply `handle`. Close only terminals created for the current task. |

## Capabilities and skills

| Operation | Select it when | Essential arguments and returned behavior |
|---|---|---|
| `capabilities` | Search installed operation metadata, manifests, skills, recipes, or one exact command on PATH. | Optional nonempty `query`, `limit`, and `include_missing`. Returns compact cards; no command is executed. |
| `skill.list` | Page through discovered Agent Skill metadata. | Optional `offset` and `limit`. Returns metadata only with `next_offset`, total, and `eof`. |
| `skill.find` | Search Skill name, description, and location. | Supply nonempty `query`; optional `limit`. Returns ranked metadata only. |
| `skill.read` | Load one Skill control document after discovery. | Supply exactly one exact `name` or discovered absolute `location`. Returns the complete `SKILL.md`, never referenced files automatically. |

## Introspection

| Operation | Select it when | Essential arguments and returned behavior |
|---|---|---|
| `health` | Confirm HEC service liveness. | Empty args. Returns `alive`. |
| `version` | Confirm installed release, protocol, commit, and build date. | Empty args. Returns exact build metadata. |
