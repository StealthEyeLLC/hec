# HEC v1 Prebuild Freeze

**Status:** complete.

This document records the final state immediately before implementation. It is a planning artifact, not a build phase.

## Frozen public interface

```text
MCP tool name: call_hec
Title: HEC
Description: Operate the HEC workstation.
```

Input:

```json
{
  "operation": "run",
  "args": {},
  "idempotency_key": "optional"
}
```

Optional risk annotations are omitted initially.

## Frozen internal vocabulary

```text
run

job.start
job.status
job.output
job.wait
job.signal
job.list
job.forget

terminal.open
terminal.list
terminal.read
terminal.write
terminal.resize
terminal.signal
terminal.close

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

artifact.return
artifact.stat
artifact.read
artifact.materialize
artifact.list
artifact.delete

capabilities
skill.list
skill.find
skill.read
health
version
```

## Frozen architecture

- one root Go binary;
- one permanent `hec.service`;
- embedded OpenAI Secure MCP Tunnel;
- embedded MCP server connected in memory;
- one stable public MCP tool;
- unrestricted host-root execution;
- systemd transient services for durable jobs;
- tmux for persistent terminals;
- native filesystem operations for file transport;
- simple returned artifacts;
- skills and plain capability manifests;
- native CLIs for Git, services, packages, containers, VMs, browsers, clouds, and infrastructure;
- no HEC database;
- no permanent forge container;
- no SEZU or Baby runtime dependency.

## Deferred until real operation

The initial build does not include:

- warning-heavy public metadata;
- a separate read-only public action;
- optional MCP risk annotations;
- platform-specific confirmation workarounds;
- additional safety, approval, receipt, evidence, audit, verification, rollback, reconciliation, policy, or workflow machinery;
- extra architecture intended only to prevent hypothetical failures.

A deferred item may be added only when actual HEC use demonstrates a concrete problem, the smallest proposed change solves it, and StealthEye confirms the change adds no meaningful friction.

## Prebuild completion checklist

- [x] Product identity and exclusions are canonical.
- [x] Host-first unrestricted-root boundary is frozen.
- [x] One-binary and one-service architecture is frozen.
- [x] Public tool name, title, and description are frozen.
- [x] Internal operation vocabulary is frozen.
- [x] Result shape and output-economy rules are defined.
- [x] Durable jobs, terminals, files, uploads, artifacts, skills, capabilities, browser, workspaces, credentials, and forge strategy are designed.
- [x] Construction slices and cutover order are defined.
- [x] Existing SEZU, Baby, and SSH lifelines remain untouched.
- [x] The real VPS baseline is recorded in the ChatGPT Project.
- [x] The canonical ChatGPT Project source is synchronized with the repository.
- [x] Broad research and architecture work is closed.

## Build start gate

No package installation, compilation, service creation, tunnel provisioning, runtime deployment, or infrastructure cutover begins until StealthEye explicitly instructs ChatGPT to start building HEC.

The first build target is:

```text
call_hec
-> health
-> version
-> run
-> unrestricted root command through the real ChatGPT connection
```
