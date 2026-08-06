---
name: hec-operator
description: Operate the HEC root workstation efficiently. Use when ChatGPT must choose between synchronous run, durable jobs, persistent terminals, files, uploads, artifacts, capability discovery, Agent Skills, Git worktrees, native system tools, or other HEC workstation operations.
---

# HEC Operator

## Understand HEC

Treat HEC as one unrestricted root workstation exposed through one public `call_hec` action. Select an explicit operation branch, use native host tools directly, and keep discovery metadata advisory. HEC does not use capability metadata as an allowlist and does not restrict native execution.

## Select the execution mode

- Use `run` for one noninteractive command that should complete in the current turn.
- Use `job.start` for long work that must survive a failed or interrupted ChatGPT turn. Record the job handle, then use `job.status`, `job.wait`, and offset-based `job.output` reads.
- Use `terminal.open` for interactive or stateful work that needs a persistent shell, REPL, debugger, or TTY. Record the terminal handle and output offset; the tmux-backed terminal can outlive an HEC service restart.

Do not put interactive programs in `run`. Do not restart a durable job merely because a ChatGPT turn failed.

## Select files, uploads, and artifacts

- Use `file.*` for exact small-to-medium filesystem reads, writes, patches, listings, and removals.
- Use `upload.*` to move larger or binary input into HEC in replay-safe chunks.
- Use `artifact.return` to return a file or directory to ChatGPT as an immutable artifact resource. Use `artifact.read` for bounded reads and `artifact.materialize` to copy an artifact back onto the host.

Keep direct `artifact.return` semantics distinct from upload completion. Do not invent a generalized request-deduplication layer.

## Discover capabilities and skills progressively

Call `capabilities` before assuming an optional command, manifest, recipe, or skill exists. Search with a concrete query and a small limit. Treat every result as ordinary metadata, not authorization and not an automatic installer.

Use `skill.list` or `skill.find` to inspect metadata first. Call `skill.read` only for the relevant skill, then read referenced files separately through `file.read` when its instructions require them. Do not load every Skill body during discovery.

## Continue after an interrupted turn

For durable work, recover by calling `job.list`, then `job.status` and `job.output` from the last recorded offset. For terminals, call `terminal.list`, then continue reading the same handle from the last output offset. Resume existing work instead of duplicating it.

## Use Git worktrees for parallel repository work

Create a separate Git worktree and branch for each independent repository task. Keep each worktree clean, commit coherent changes, and integrate only after tests pass. Do not make concurrent agents edit the same checkout.

## Prefer native tools

Use native CLIs and package managers through `run`, `job.start`, or a persistent terminal rather than expecting HEC-specific wrappers. Query `capabilities` first when presence is uncertain.

Before substantial browser automation, query `capabilities` for `browser.playwright`, locate `playwright-cli` with `skill.find`, and read that Skill. Invoke the installed CLI through `run` or a persistent terminal, and return generated browser files through HEC artifacts.

## Read the references

- Read [references/operations.md](references/operations.md) for operation selection, essential arguments, handles, and offsets.
- Read [references/forge.md](references/forge.md) for native-tool, recipe, installer, Playwright, and worktree guidance.
