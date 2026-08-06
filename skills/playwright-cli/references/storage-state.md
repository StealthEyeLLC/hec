# Storage state

Prefer a dedicated persistent profile for durable cookies, local storage, IndexedDB, and other browser state.

Inspect and modify task-local state with:

```bash
playwright-cli -s="$NAME" cookie-list
playwright-cli -s="$NAME" cookie-get <name>
playwright-cli -s="$NAME" cookie-set <name> <value> [options]
playwright-cli -s="$NAME" cookie-delete <name>
playwright-cli -s="$NAME" cookie-clear
playwright-cli -s="$NAME" localstorage-list
playwright-cli -s="$NAME" localstorage-get <key>
playwright-cli -s="$NAME" localstorage-set <key> <value>
playwright-cli -s="$NAME" localstorage-delete <key>
playwright-cli -s="$NAME" localstorage-clear
playwright-cli -s="$NAME" sessionstorage-list
```

Use portable state only when it materially helps:

```bash
playwright-cli -s="$NAME" state-save "$OUTPUT/state.json"
playwright-cli -s="$NAME" state-load "$OUTPUT/state.json"
```

Treat profiles and exported storage as credentials. Do not print them into normal chat output, commit them, or return them unless explicitly requested.
