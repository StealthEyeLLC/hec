# HEC v1 ChatGPT Interface

**Status:** complete design draft; not frozen until approved by StealthEye.

HEC exists specifically for ChatGPT. This document defines the external interface from the model's point of view.

## 1. ChatGPT-side objective

The interface should make these choices nearly automatic:

```text
need to run something now        -> hec.exec
need work to outlive this turn   -> hec.job.start
need an interactive PTY          -> hec.terminal.create
need to inspect or edit bytes     -> hec.file.*
need to send or receive a file    -> hec.upload.* / hec.artifact.*
need to know what exists          -> hec.capabilities
need operating guidance           -> hec.skill.search / hec.skill.read
```

There should be no separate mental model for the host, a forge container, a target scheduler, a policy layer, or a workflow service.

## 2. Public tool definition

Tool name:

```text
call_hec
```

Recommended title:

```text
HEC Root Workstation
```

Recommended description:

> Operate the owner-controlled HEC Linux host with unrestricted root authority. Use `hec.exec` for direct commands, `hec.job.start` for noninteractive work that may outlive the current ChatGPT turn, `hec.terminal.create` for interactive programs, file and artifact operations for binary-safe transfer, and `hec.capabilities` or HEC skills when you need to discover installed tools or usage guidance. HEC does not impose command policy, approvals, previews, verification, rollback, or sandboxing.

The description should remain stable. New native operations are discovered through `hec.capabilities`; they do not require adding a new public MCP tool.

## 3. Input economy

Input remains:

```json
{
  "operation": "hec.exec",
  "args": {},
  "idempotency_key": "optional"
}
```

The tool schema should not contain an enormous `oneOf` tree for every operation. That would make the frozen ChatGPT action definition large and require republishing every time the internal catalog changes.

HEC validates operation-specific arguments after dispatch and returns a concise argument error with the expected shape.

## 4. Operation selection rules

### Use `hec.exec` when

- the command is noninteractive;
- it should finish during the current call;
- no future stdin is required;
- losing the current ChatGPT turn would not need the process to continue.

Prefer direct `argv` when the command is already naturally tokenized. Use `command` when pipes, redirects, shell expansion, loops, heredocs, or compound commands are useful.

### Use `hec.job.start` when

- compilation, installation, cloning, rendering, testing, downloading, or another command may take longer than the tool call;
- the command should continue after ChatGPT disconnects;
- output should be read incrementally;
- the operation is noninteractive.

Use an idempotency key when a failed ChatGPT response could otherwise cause the same long job to be started twice.

### Use a terminal when

- the process prompts repeatedly;
- a debugger, REPL, SSH connection, database console, or installer needs continuing input;
- terminal dimensions or PTY behavior matter;
- the task benefits from a session that ChatGPT can leave and return to.

### Use file operations when

- exact binary bytes matter;
- a file is small enough for direct read/write;
- a unified patch is clearer than a shell command;
- stdout would be a poor transport.

### Use upload and artifact operations when

- a file is too large for one tool argument or result;
- the user needs the final file in ChatGPT;
- the output should have a stable resource handle;
- a directory needs to be returned as one archive.

### Use capabilities and skills when

- the correct command is not obvious;
- the tool may be installed under a nonstandard name;
- the capability is optional or installable by recipe;
- an established HEC or project-specific method exists.

## 5. Result economy

HEC output should be optimized for model comprehension rather than operator dashboards.

### Always return

- operation;
- status;
- native exit result when relevant;
- nonempty stdout and stderr;
- handle when future calls need it;
- operation-specific result;
- returned resources;
- a short exact error when present.

### Do not return by default

- echoed request arguments;
- request IDs;
- timestamps unrelated to the result;
- environment dumps;
- host snapshots;
- policy or risk labels;
- verification fields;
- audit metadata;
- trace IDs;
- receipt hashes;
- help prose when the call succeeded;
- the same data in several shapes.

### Text and structured content

The MCP result should include:

1. `structuredContent` containing the HEC result envelope;
2. one concise `TextContent` summary.

Example text summary:

```text
Completed hec.exec on the host with exit code 0.
```

If stdout is the useful answer, the text summary may simply be stdout plus a compact exit note.

### Output size

Default maximum inline stdout plus stderr:

```text
1 MiB
```

When output exceeds the limit:

- set `truncated: true`;
- preserve the first useful portion rather than flooding the context;
- for durable jobs, return `next_offset` so ChatGPT can continue reading;
- for commands likely to produce very large output, ChatGPT should redirect to a file or start a job.

HEC should never silently discard whether truncation occurred.

## 6. Resource presentation

When a result creates an artifact:

- return a structured artifact descriptor;
- return an MCP resource link when supported;
- use an embedded image content block for a small preview when useful;
- retain `hec.artifact.read` as the fallback.

Artifact descriptor:

```json
{
  "handle": "artifact:abc",
  "name": "result.pdf",
  "mime_type": "application/pdf",
  "size": 123456,
  "resource_uri": "hec://artifact/abc"
}
```

No content hash is required unless the actual task asks for one.

## 7. Capability response shape

`hec.capabilities` accepts:

```json
{
  "query": "browser automation with persistent login",
  "limit": 10,
  "include_missing": true
}
```

Return compact cards:

```json
{
  "capabilities": [
    {
      "id": "browser.playwright",
      "description": "Persistent browser automation designed for coding agents",
      "installed": true,
      "commands": ["playwright-cli"],
      "skills": ["playwright-cli"],
      "recipe": null
    }
  ]
}
```

Do not return the entire binary inventory unless explicitly requested.

## 8. Skill disclosure

ChatGPT should see only skill metadata during discovery:

```text
name
description
location
```

The full `SKILL.md` is returned only by `hec.skill.read` or when the caller explicitly asks for the skill.

References, scripts, and assets remain files and are loaded only when relevant.

This mirrors the Agent Skills progressive-disclosure model and preserves context for the actual engineering task.

## 9. Native CLI preference

HEC should not teach ChatGPT that wrappers are inherently better than ordinary commands.

Preference:

```text
raw native CLI
-> established HEC or project skill
-> small HEC primitive when MCP transport requires it
```

Examples:

- use `git` rather than a custom HEC Git API;
- use `systemctl` and `journalctl` rather than a service wrapper;
- use `incus`, `podman`, and `docker` rather than a HEC container abstraction;
- use `playwright-cli` rather than a custom browser action catalog;
- use official cloud CLIs rather than a cloud wrapper;
- use project package managers and build commands rather than a HEC build system.

## 10. Browser interaction

ChatGPT uses the official Playwright CLI skill.

Normal browser loop:

```text
open named session
read accessibility snapshot
interact using current element references
read updated snapshot
capture screenshot/download/trace when useful
return files through HEC artifacts
```

Persistent accounts use named profile directories under `/var/lib/hec/browser/profiles`.

Browser commands run through `hec.exec` when short or a terminal when an interactive/debugging session is useful.

## 11. Handling a failed ChatGPT turn

HEC cannot prevent the ChatGPT app, model invocation, mobile client, or network from failing.

HEC minimizes the consequence without creating a task system.

### Synchronous call

If a synchronous response is lost, ChatGPT inspects current files, Git state, services, or other native state as appropriate. HEC does not retain a universal operation history.

### Durable job

If a job was accepted:

```text
hec.job.list
hec.job.status
hec.job.output
```

recover the process and output.

If the caller used an idempotency key, repeating `hec.job.start` with that key returns the existing job handle.

### Terminal

List terminals and read the existing session.

This is the complete HEC-specific answer to a failed ChatGPT reasoning turn.

## 12. ChatGPT Project use

The ChatGPT Project holds:

- design discussion;
- project instructions;
- canonical HEC documents;
- user intent;
- architectural synthesis.

HEC holds:

- actual files;
- repositories;
- processes;
- services;
- terminals;
- jobs;
- browser profiles;
- returned artifacts;
- installed tools.

The Project is conversational continuity. The host is operational truth.

## 13. App lifecycle

The ChatGPT custom MCP app uses a frozen snapshot of public actions and inputs.

Therefore:

- keep `call_hec` stable;
- do not add public tools for every feature;
- add internal operations, skills, commands, and recipes freely;
- refresh or republish the ChatGPT app only when the `call_hec` input or output schema itself changes.

HEC adds no local UI. ChatGPT is the primary interface; the local `hec call` CLI is the fallback.

## 14. Platform behavior

HEC itself adds no confirmations, safety checks, approvals, or restrictions.

ChatGPT or the hosting workspace may still display confirmations or block certain actions according to platform behavior. That behavior is outside HEC and is not duplicated by HEC.

## 15. Operator skill outline

The future `skills/hec-operator/SKILL.md` should remain short and contain:

1. what HEC is;
2. operation-selection rules from this document;
3. result envelope meanings;
4. use of capability discovery;
5. use of durable jobs after a failed turn;
6. links to concise operation reference files;
7. the rule that raw native tools are preferred and unrestricted.

It should not repeat the forge inventory or embed long reference material into every context.

## 16. Research basis

Primary references:

- ChatGPT custom MCP apps and frozen action snapshots: https://help.openai.com/en/articles/12584461-developer-mode-and-full-mcp-connectors-in-chatgpt-beta
- MCP structured content, output schemas, and resource links: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- OpenAI Secure MCP Tunnel Go embedding: https://github.com/openai/tunnel-client
- OpenAI Agent Skills: https://github.com/openai/skills
- Playwright CLI for coding agents: https://playwright.dev/agent-cli/introduction
