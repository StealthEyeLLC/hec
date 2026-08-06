# Running Playwright code

Use `eval` for a short page or element expression:

```bash
playwright-cli -s="$NAME" eval "document.title"
playwright-cli -s="$NAME" eval "el => el.getAttribute('data-testid')" e5
```

Use `run-code` for multi-step browser logic. Pass an inline JavaScript function or a file:

```bash
playwright-cli -s="$NAME" run-code \
  "async page => { await page.getByRole('button', { name: 'Save' }).click(); return await page.title(); }"
playwright-cli -s="$NAME" run-code --filename="$OUTPUT/task.js"
```

Keep code bounded to the active page and named task session. Save reusable or auditable code to an explicit file. Do not embed credentials in code or print secrets returned from the page.
