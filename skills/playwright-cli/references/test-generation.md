# Test generation

Use snapshots and current element references to establish a reliable interaction path before generating test code.

1. Open a named isolated session.
2. Navigate through the target flow.
3. Capture a fresh snapshot after each material state change.
4. Generate locators only for elements that are stable and uniquely identified.
5. Translate the proven flow into the project's Playwright test style.
6. Run the generated test with the project's normal test command.
7. Heal only the failing locator or assertion supported by current evidence.

Do not copy snapshot-specific `eN` references into committed tests. Prefer role, label, text, or test-id locators appropriate to the application.
