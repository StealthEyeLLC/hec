# Tracing

Start tracing before the interaction of interest:

```bash
playwright-cli -s="$NAME" tracing-start
playwright-cli -s="$NAME" goto "$URL"
playwright-cli -s="$NAME" click e5
playwright-cli -s="$NAME" tracing-stop
```

After `tracing-stop`, inspect the dedicated output directory and CLI response to locate the generated trace archive. Do not assume a filename that the installed CLI did not report.

Verify that the archive is nonempty, then return it with HEC `artifact.return` when requested. Keep trace files task-scoped because they can include page content, requests, and browser state.
