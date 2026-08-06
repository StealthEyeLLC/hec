---
name: hec-operator
description: Operate the HEC root workstation efficiently. Use when ChatGPT must choose between synchronous run, durable jobs, persistent terminals, files, uploads, artifacts, capability discovery, Agent Skills, workspace conventions, Git repositories and worktrees, native system tools, or other HEC workstation operations.
---

# HEC Operator

## Understand HEC

Treat HEC as one unrestricted root workstation exposed through one public `call_hec` action. Select an explicit operation branch, use native host tools directly, and keep discovery metadata advisory. HEC does not use capability metadata as an allowlist and does not restrict native execution.

Treat project files and Git as the source of truth. HEC does not control repositories, reconcile workspaces, manage deliveries, or automatically activate project metadata.

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

Call `capabilities` before assuming an optional command, workspace, manifest, recipe, or Skill exists. Search with a concrete query and a small limit. Treat every result as ordinary metadata, not authorization and not an automatic installer.

Use `skill.list` or `skill.find` to inspect metadata first. Call `skill.read` only for the relevant Skill, then read referenced files separately through `file.read` when its instructions require them. Do not load every Skill body during discovery or automatically run workspace Skill scripts.

## Use workspace conventions explicitly

Query `capabilities` to discover known workspaces. When a project is not discovered, use its raw absolute path directly; discovery is never a path jail.

Read `.hec/workspace.toml` explicitly when defaults or environment values are needed. Pass selected `cwd` and `env` values explicitly to `run`, `job.start`, or `terminal.open`. Never assume HEC applies workspace metadata automatically.

Use these ordinary filesystem conventions when appropriate:

```text
/srv/hec/repositories/<repo>.git
/srv/hec/workspaces/<project>/main
/srv/hec/workspaces/<project>/worktrees/<branch>
/srv/hec/deliveries/<prepared-output>
```

The shared bare repository is optional; `/srv/hec/workspaces/<project>/repository` is also valid. Treat `.hec/scratch` as optional disposable material and clean only task-owned state.

## Continue after an interrupted turn

For durable work, recover by calling `job.list`, then `job.status` and `job.output` from the last recorded offset. For terminals, call `terminal.list`, then continue reading the same handle from the last output offset. Resume existing work instead of duplicating it.

## Use native Git worktrees for parallel work

Create a separate native Git worktree and branch for each independent repository task. Prefer `/srv/hec/workspaces/<project>/main` for the main checkout and `/srv/hec/workspaces/<project>/worktrees/<branch>` for parallel worktrees. Keep each worktree clean, commit coherent changes, and integrate only after tests pass.

Do not make concurrent agents edit the same checkout. Use ordinary commands such as `git clone`, `git init --bare`, `git worktree add`, `git worktree list`, `git worktree remove`, `git worktree prune`, `git status`, `git commit`, `git fetch`, and `git push`. Do not expect HEC-specific Git operations.

## Maintain HEC releases explicitly

Read [references/maintenance.md](references/maintenance.md) before building, installing, publishing, or activating an HEC release. Keep installation separate from activation, run HEC-initiated cutovers through a durable job, and verify the exact Git commit after reconnecting. Do not invent automatic rollback or a release controller.

## Prefer native tools

Use native CLIs and package managers through `run`, `job.start`, or a persistent terminal rather than expecting HEC-specific wrappers. Query `capabilities` first when presence is uncertain.

Before substantial browser automation, query `capabilities` for `browser.playwright`, locate `playwright-cli` with `skill.find`, and read that Skill. Invoke the installed CLI through `run` or a persistent terminal, and return generated browser files through HEC artifacts.

## Read the references

- Read [references/operations.md](references/operations.md) for operation selection, essential arguments, handles, and offsets.
- Read [references/forge.md](references/forge.md) for native-tool, recipe, installer, Playwright, and general worktree guidance.
- Read [references/workspaces.md](references/workspaces.md) for workspace metadata, repository layouts, native Git worktree examples, deliveries, and task-owned cleanup.
- Read [references/maintenance.md](references/maintenance.md) for pinned builds, immutable installs, deploy-key publication, explicit durable cutover, and manual older-release selection.
