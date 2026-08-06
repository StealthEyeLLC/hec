# Session management

Use the current CLI session syntax:

```bash
playwright-cli -s=<name> <command> [args]
```

Create one session name per independent browser identity. Pair it with:

```text
/var/lib/hec/browser/profiles/<name>
/var/lib/hec/browser/output/<name>
```

Create both task directories with mode `0700`. Open the session with the checked-in Chromium config and a dedicated profile:

```bash
playwright-cli -s="$NAME" open "$URL" \
  --config=/opt/hec/current/forge/playwright/cli.config.json \
  --persistent \
  --profile="/var/lib/hec/browser/profiles/$NAME"
```

Use `playwright-cli list` to inspect sessions. Close one session with:

```bash
playwright-cli -s="$NAME" close
```

Persistent state means the profile survives browser-process closure. It does not require the same browser PID or daemon to survive an HEC restart.

For an isolation test, open two sessions with different profile and output paths, set different local-storage values, close one, and verify the other remains usable.

Avoid `close-all` and `kill-all`. Use either only after confirming an orphaned-process problem and verifying no unrelated sessions exist.
