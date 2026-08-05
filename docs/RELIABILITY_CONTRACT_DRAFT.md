# HEC Reliability Contract — Draft

**Status:** unfrozen research draft.

HEC is designed on the assumption that any ChatGPT reasoning turn, mobile client session, MCP connection, gateway process, daemon process, browser process, terminal, job, network request, or host boot may fail or disappear at an inconvenient moment.

The goal is not the impossible promise that errors never occur. The goal is that failures are detected, represented truthfully, and recover without lost work, duplicated effects, or reconstruction from chat history.

## Core invariants

1. **No ChatGPT turn owns durable work.**
   Once HEC accepts work, the work and its output state belong to HEC until completion, cancellation, or explicit deletion.

2. **Every mutating request is identifiable.**
   A mutating request has a stable task ID, step ID, and idempotency key.

3. **Retries do not blindly repeat effects.**
   Repeating the same accepted request returns, resumes, or reconciles the original operation instead of starting a duplicate.

4. **Unknown effects are represented as unknown.**
   If HEC cannot prove whether an external or interrupted side effect occurred, it does not report success, failure, or safe retry until it reconciles actual state.

5. **State is committed before and after side effects.**
   HEC durably records accepted intent before dispatch and records observed outcome after execution or reconciliation.

6. **Current machine state outranks history.**
   Resume logic inspects Git, files, services, processes, systemd units, jobs, terminals, browsers, and artifacts instead of assuming the last response was delivered.

7. **Outputs are durable before they are reported.**
   Job output, diagnostics, and produced artifacts are persisted before a tool response claims they exist.

8. **File publication is atomic where the filesystem permits.**
   HEC writes a complete replacement, synchronizes it when durability is required, and atomically publishes it. Expected pre-change hashes prevent editing stale content.

9. **No blind retries of open-world side effects.**
   Network, cloud, email, deployment, payment, publication, and other external effects require an idempotency mechanism or a state probe before retry.

10. **Success requires verification.**
    A zero exit code is evidence, not universal proof. Skills and flows define the observations or probes that establish real completion.

11. **Every multi-step task has a public operational capsule.**
    The capsule stores the goal, acceptance criteria, completed and active steps, current verified facts, blockers, produced references, and next safe action. It never attempts to store private model reasoning.

12. **A fresh ChatGPT context can resume.**
    HEC can compile a compact resume packet from the task capsule and live machine state without replaying the complete conversation.

13. **Simple work remains simple.**
    Direct reads and short commands do not require a heavyweight workflow. Durability machinery appears only where interruption or composition requires it.

14. **HEC remains standalone.**
    SEZU and Baby may be used to construct HEC but are never HEC runtime dependencies.

## Required failure classes

HEC design and tests must cover at least:

- ChatGPT reasoning failure or response interruption;
- mobile or web client closure;
- lost or duplicated MCP requests;
- tunnel and gateway disconnects;
- gateway or root-daemon restart;
- host reboot;
- process, terminal, browser, and service crash;
- timeout, cancellation, signal, and out-of-memory termination;
- disk-full and inode-full conditions;
- output truncation and binary output;
- partial upload or transfer;
- stale file observations and concurrent edits;
- Git conflicts and dirty working trees;
- package-manager locks and interrupted package transactions;
- network loss, rate limits, expired credentials, and provider outages;
- duplicate retries after an unknown response state;
- schema mismatch and stale MCP action definitions;
- verification failure after apparent command success;
- unexpected failures not known in advance.

## Reliability target

The primary target is **zero unrecoverable task-state loss after HEC accepts a request**.

A secondary target is minimizing the frequency of failures through automatic preflight checks, precise schemas, truthful tool descriptions, deterministic state resolution, and closed-loop verification.

## Proof strategy

HEC must include fault-injection and model-interface tests that deliberately terminate clients and processes at every important transition, replay requests, fill storage, create concurrency conflicts, corrupt or truncate test outputs, and verify that the resulting state is recoverable and truthfully represented.
