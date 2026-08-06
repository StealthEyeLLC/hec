# HEC forge and native tools

## Native tools are authoritative

Use the host's native package managers and CLIs as the source of truth. HEC exposes execution, persistence, files, transfers, artifacts, terminals, capability metadata, and Skill disclosure; it does not replace ordinary system tools with wrappers.

## Capabilities is advisory discovery

Query `capabilities` before assuming a command, Skill, or optional tool is present. Results come from the embedded operation schema, small TOML manifests, discovered Skill metadata, checked-in recipe names, and an optional exact PATH lookup. Results never authorize, deny, install, or execute anything.

Missing metadata never blocks `run` or any native tool. A missing card is not proof that native execution is unavailable.

## Recipes are ordinary files

Treat forge recipes as checked-in files that describe optional installation work. There is no automatic installer, recipe executor, planner, capability daemon, indexer, or package database in HEC. Read a relevant recipe as an ordinary file, inspect it, then deliberately invoke its commands.

Use `run` for short noninteractive installers, `job.start` for long durable installations, and `terminal.open` for interactive installers or tools.

## Installed core forge

The core forge uses pinned native managers and commands. `mise` is installed for project runtimes and selected standalone tools with `MISE_TRUSTED_CONFIG_PATHS=/`. `uv` and `uvx` provide isolated Python environments and tools without replacing Ubuntu's system Python. Rust is managed through the existing apt-provided `rustup` installation. Corepack manages the recorded pnpm and Yarn releases, while Bun and Deno are available through mise.

Docker Engine coexists with Podman, Skopeo, crun, Incus, and QEMU/KVM. Use the native CLI appropriate to the task; HEC adds no container abstraction. Query `capabilities` before assuming an optional command or grouped capability is present.

Recipes remain ordinary checked-in shell files and run only when deliberately invoked. Heavy desktop, CAD, mobile, database-server, science, and extra-browser layers remain deferred.

## Playwright

Query `capabilities` for `browser.playwright`, locate the `playwright-cli` Skill with `skill.find`, and read it before substantial browser work. Invoke the installed `playwright-cli` command through `run` for bounded work or a persistent terminal for long interactive debugging. Use named sessions, dedicated profiles and output directories, and return generated screenshots, downloads, traces, videos, or PDFs through HEC artifacts. HEC does not add browser-specific operations or a browser wrapper.

## Parallel repository work

Use Git worktrees for independent concurrent tasks. Give each worktree its own branch and working directory, keep changes isolated, run tests in that worktree, and integrate coherent commits. Do not let multiple workers modify the same checkout.
