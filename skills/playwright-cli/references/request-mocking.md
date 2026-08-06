# Request mocking

Inspect traffic before mocking:

```bash
playwright-cli -s="$NAME" requests
playwright-cli -s="$NAME" request 3
playwright-cli -s="$NAME" response-body 3
```

Add a narrow route for the required pattern:

```bash
playwright-cli -s="$NAME" route "https://api.example.test/**" --body='{"ok":true}'
playwright-cli -s="$NAME" route-list
```

Remove only the route created by the task:

```bash
playwright-cli -s="$NAME" unroute "https://api.example.test/**"
```

Avoid an unqualified `unroute` when unrelated routes might exist. Record mocks in test code when they are part of a reproducible project test.
