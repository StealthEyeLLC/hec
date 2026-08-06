---
name: playwright-cli
description: Automate and test web browsers with the Playwright CLI. Use when ChatGPT must navigate websites, inspect accessibility snapshots, interact with page elements, manage named browser sessions, preserve browser profiles, capture screenshots, downloads, traces, videos, run Playwright code, or debug browser and web-application behavior through HEC.
---

# Playwright CLI through HEC

## Use the installed command

Invoke `playwright-cli` as an installed command through HEC `run` for bounded browser work. Use a persistent HEC terminal when a long interactive debugging session, continuing shell state, dashboard use, or repeated manual intervention is useful.

Do not add browser-specific HEC operations. Use the checked-in Chromium configuration:

```bash
CONFIG=/opt/hec/current/forge/playwright/cli.config.json
```

When validating an uninstalled repository checkout, use `/work/hec/forge/playwright/cli.config.json` instead.

## Create isolated task state

Choose a unique safe task or workspace name. Use the same name for the CLI session, profile, and output directory:

```bash
NAME=task-name
export PLAYWRIGHT_CLI_SESSION="$NAME"
export PLAYWRIGHT_MCP_OUTPUT_DIR="/var/lib/hec/browser/output/$NAME"
PROFILE="/var/lib/hec/browser/profiles/$NAME"
install -d -m 0700 "$PROFILE" "$PLAYWRIGHT_MCP_OUTPUT_DIR"
```

Treat profile directories as sensitive. They may contain cookies, credentials, and authenticated browser state. Do not print profile contents, copy credentials into chat, or return an entire profile as an artifact.

Open Chromium with a named session and persistent profile:

```bash
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" open "https://example.com" \
  --config="$CONFIG" \
  --persistent \
  --profile="$PROFILE"
```

Use separate names, profiles, and output directories for concurrent browser identities.

## Follow the snapshot loop

1. Open or navigate to the target page.
2. Capture a current accessibility snapshot.
3. Read the snapshot file when the CLI returns a path.
4. Interact using a current element reference.
5. Capture and read a new snapshot after navigation or any material DOM change.
6. Save requested outputs with explicit filenames.
7. Locate generated files and return them with HEC `artifact.return`.
8. Close only the named session used by the task.

Element references are snapshot-specific. Do not reuse a stale reference after navigation, form submission, tab changes, reloads, or material page updates.

```bash
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" snapshot \
  --filename="$PLAYWRIGHT_MCP_OUTPUT_DIR/current.md"
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" fill e3 "value"
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" click e4
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" snapshot \
  --filename="$PLAYWRIGHT_MCP_OUTPUT_DIR/after-click.md"
```

Use supported filename options. Piping `snapshot` or `screenshot` output is not a substitute when a durable file is required.

## Navigate and interact

Use `goto`, `go-back`, `go-forward`, `reload`, `click`, `dblclick`, `fill`, `type`, `press`, `keydown`, `keyup`, `hover`, `select`, `check`, `uncheck`, `drag`, `drop`, and `upload` as needed. Prefer snapshot references; use a selector or locator only when it is unique and stable.

Manage tabs with `tab-list`, `tab-new`, `tab-select`, and `tab-close`. Capture a fresh snapshot after selecting a different tab.

## Inspect browser behavior

Use `console` for browser console messages and `requests`, `request`, `request-headers`, `request-body`, `response-headers`, and `response-body` for network inspection. Clear or filter accumulated output when it improves signal.

Use `eval` for small page or element expressions. Use `run-code` for multi-step Playwright code. Read [references/running-code.md](references/running-code.md) before substantial code execution.

## Save outputs

Write files into the dedicated output directory with explicit names:

```bash
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" screenshot \
  --filename="$PLAYWRIGHT_MCP_OUTPUT_DIR/page.png" --full-page
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" pdf \
  --filename="$PLAYWRIGHT_MCP_OUTPUT_DIR/page.pdf"
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" tracing-start
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" click e5
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" tracing-stop
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" video-start \
  "$PLAYWRIGHT_MCP_OUTPUT_DIR/session.webm"
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" click e6
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" video-stop
```

The CLI may choose a generated trace or download filename. Inspect the dedicated output directory after the command and identify the exact resulting path before returning it.

Return browser files through HEC artifacts:

```text
artifact.return(path=<exact generated path>)
```

Confirm the artifact handle, metadata, `hec://artifact` URI, and resource descriptor. Use `artifact.stat` and bounded `artifact.read` when verification is required.

## Preserve or move storage state deliberately

Persistent profiles are the normal mechanism for durable browser state. Use `state-save` and `state-load` only when the task benefits from explicit portable storage state. Keep exported storage files private and return them only when explicitly requested.

Use local storage and cookie commands only for the named task session. See [references/storage-state.md](references/storage-state.md).

## Manage sessions safely

List sessions with `playwright-cli list` and address the task session with `-s=<name>`. Close only that session:

```bash
playwright-cli -s="$PLAYWRIGHT_CLI_SESSION" close
```

Do not use `close-all` or `kill-all` as normal cleanup. Use them only to resolve a concrete orphaned-process problem after verifying no unrelated sessions exist.

## Read focused references

- Read [references/session-management.md](references/session-management.md) for named sessions, profiles, isolation, and cleanup.
- Read [references/storage-state.md](references/storage-state.md) for cookies, local storage, and portable state.
- Read [references/tracing.md](references/tracing.md) for trace workflows.
- Read [references/video-recording.md](references/video-recording.md) for video workflows.
- Read [references/running-code.md](references/running-code.md) for `eval` and `run-code`.
- Read [references/playwright-tests.md](references/playwright-tests.md) for running and debugging Playwright tests.
- Read [references/request-mocking.md](references/request-mocking.md) for routing and network mocks.
- Read [references/test-generation.md](references/test-generation.md) for plan, generate, and heal workflows.
- Read [references/element-attributes.md](references/element-attributes.md) for inspecting attributes not shown in snapshots.
- Read [references/upstream.md](references/upstream.md) for upstream version, provenance, and licensing.
