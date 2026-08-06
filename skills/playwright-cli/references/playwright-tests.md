# Playwright tests

Use the repository's existing Playwright test configuration and package manager when present. Do not replace a project test workflow with ad hoc CLI interactions.

For browser diagnosis, use a named CLI session to reproduce the failing state, inspect snapshots, console output, requests, traces, and video, then run the project's normal test command through HEC `run`, `job.start`, or a persistent terminal.

Use `pause-at`, `resume`, and `step-over` only when the installed CLI and the current test workflow support them. Preserve project files and report the exact command, exit status, and relevant artifact paths.
