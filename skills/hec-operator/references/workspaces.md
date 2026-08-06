# Workspace and Repository Conventions

Use these filesystem and native Git conventions directly. HEC does not create, reconcile, activate, or clean workspaces for you.

## Create the shared roots on an older host

```bash
install -d -o root -g root -m 0755 \
  /srv/hec \
  /srv/hec/workspaces \
  /srv/hec/repositories \
  /srv/hec/deliveries
```

Do not recursively change ownership or permissions beneath existing roots.

## Initialize an optional shared bare repository

```bash
git init --bare /srv/hec/repositories/example.git
```

The bare repository is optional. A project can instead keep one ordinary checkout at `/srv/hec/workspaces/example/repository`.

## Create the main checkout

```bash
mkdir -p /srv/hec/workspaces/example
git clone /srv/hec/repositories/example.git \
  /srv/hec/workspaces/example/main
```

Use native Git credentials configured for that repository. Do not assume another repository's scoped credential helper applies.

## Add and inspect a parallel worktree

```bash
mkdir -p /srv/hec/workspaces/example/worktrees
git -C /srv/hec/workspaces/example/main \
  worktree add -b feature/example \
  /srv/hec/workspaces/example/worktrees/feature-example

git -C /srv/hec/workspaces/example/main worktree list
```

Give every independent parallel task its own branch and worktree. Never make concurrent agents edit the same checkout.

## Remove and prune a worktree

```bash
git -C /srv/hec/workspaces/example/main \
  worktree remove /srv/hec/workspaces/example/worktrees/feature-example

git -C /srv/hec/workspaces/example/main worktree prune
```

Remove only a task-owned worktree after its changes are committed or intentionally discarded.

## Add advisory workspace metadata

Create `/srv/hec/workspaces/example/.hec/workspace.toml`:

```toml
description = "Example project workspace"
notes = "Project files and Git remain authoritative"
default_cwd = "main"
repository = "main"
tags = ["example", "git"]
skills = ["project-operator"]

[env]
EXAMPLE_MODE = "development"
```

Relative paths resolve from the workspace directory. Absolute paths remain unchanged. HEC does not create declared paths, inject environment values, switch checkouts, or apply these defaults automatically.

## Discover a workspace

Call the existing `capabilities` operation:

```json
{
  "operation": "capabilities",
  "args": {
    "query": "workspace.example",
    "limit": 10
  }
}
```

Workspace cards expose concise metadata, repository kind, environment-variable names, and Skill names. They never expose environment values.

Raw paths remain valid even when a directory is not registered or is skipped by discovery.

## Read explicit defaults or environment values

Read the manifest only when the task needs its values:

```json
{
  "operation": "file.read",
  "args": {
    "path": "/srv/hec/workspaces/example/.hec/workspace.toml"
  }
}
```

Then pass selected values explicitly through ordinary arguments:

```json
{
  "operation": "job.start",
  "args": {
    "argv": ["make", "test"],
    "cwd": "/srv/hec/workspaces/example/main",
    "env": {
      "EXAMPLE_MODE": "development"
    }
  }
}
```

Use the same explicit `cwd` and `env` rule for `run` and `terminal.open`.

## Discover workspace-local Skills progressively

Store project instructions at:

```text
/srv/hec/workspaces/example/.hec/skills/<skill-name>/SKILL.md
```

Use `skill.list` or `skill.find` for metadata, then `skill.read` for one exact Skill. Do not load every workspace Skill body during discovery and do not automatically run Skill scripts.

## Prepare a delivery

Create an explicitly named final deliverable under the ordinary output directory:

```bash
install -m 0644 ./dist/report.pdf \
  /srv/hec/deliveries/example-report.pdf
```

Return it with `artifact.return` when it must cross into ChatGPT. `/srv/hec/deliveries` is not an artifact registry and HEC does not manage delivery lifecycle.

## Clean task-owned scratch only

Use `.hec/scratch` for optional disposable project-local material:

```bash
rm -rf -- /srv/hec/workspaces/example/.hec/scratch/slice-owned-token
```

Delete only state created for the current task. Do not broadly prune repositories, caches, unrelated worktrees, jobs, browser profiles, or owner-created files.
