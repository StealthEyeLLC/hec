# HEC forge and native tools

## Native tools are authoritative

Use the host's native package managers and CLIs as the source of truth. HEC exposes execution, persistence, files, transfers, artifacts, terminals, capability metadata, and Skill disclosure; it does not replace ordinary system tools with wrappers.

## Capabilities is advisory discovery

Query `capabilities` before assuming a command, Skill, or optional tool is present. Results come from the embedded operation schema, small TOML manifests, discovered Skill metadata, checked-in recipe names, and an optional exact PATH lookup. Results never authorize, deny, install, or execute anything.

Missing metadata never blocks `run` or any native tool. A missing card is not proof that native execution is unavailable.

## Recipes are ordinary files

Treat forge recipes as checked-in files that describe optional installation work. There is no automatic installer, recipe executor, planner, capability daemon, indexer, or package database in HEC. Read a relevant recipe as an ordinary file, inspect it, then deliberately invoke its commands.

Use `run` for short noninteractive installers, `job.start` for long durable installations, and `terminal.open` for interactive installers or tools.

## Playwright

Do not assume the Playwright CLI or Chromium is installed. Query `capabilities` for the exact command or check command presence without executing it. When installed, invoke Playwright CLI through `run` or a persistent terminal; HEC does not add a browser wrapper.

## Parallel repository work

Use Git worktrees for independent concurrent tasks. Give each worktree its own branch and working directory, keep changes isolated, run tests in that worktree, and integrate coherent commits. Do not let multiple workers modify the same checkout.
