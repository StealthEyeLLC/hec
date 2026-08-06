# Element attributes

Accessibility snapshots omit many DOM attributes. Inspect a current element reference with `eval`:

```bash
playwright-cli -s="$NAME" eval "el => el.id" e5
playwright-cli -s="$NAME" eval "el => el.className" e5
playwright-cli -s="$NAME" eval "el => el.getAttribute('data-testid')" e5
playwright-cli -s="$NAME" eval "el => ({href: el.href, disabled: el.disabled})" e5
```

Use a reference from the latest snapshot. Capture a new snapshot before inspecting an element after navigation or a material DOM update.
