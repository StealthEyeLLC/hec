# Video recording

Use the installed CLI's explicit video commands:

```bash
playwright-cli -s="$NAME" video-start "$OUTPUT/session.webm" --size=800x600
playwright-cli -s="$NAME" click e5
playwright-cli -s="$NAME" video-stop
```

Perform at least one visible action while recording. Confirm the resulting file is nonempty and identify its exact path before returning it.

Use `video-show-actions` and `video-hide-actions` only when action callouts improve the requested recording. Keep video output inside the dedicated task output directory.
