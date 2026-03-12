---
name: testing-dashboard
description: How to test devctl features via the browser dashboard — always use the Playwright snapshot tool, never the screenshot tool, and always verify changes in the UI at http://127.0.0.1:4000 after deploying.
---

# Skill: testing-dashboard

## Overview

After building and deploying devctl (`go build -o devctl . && make deploy`), always verify the change works by opening the dashboard in the browser and interacting with it. Do not assume the build passing is sufficient.

## Rules

1. **Always test in the dashboard.** Navigate to `http://127.0.0.1:4000` after every deploy and verify the relevant page or feature visually and functionally.

2. **Never use the screenshot tool.** Use `playwright_browser_snapshot` instead. Screenshots are large binary blobs that fill the context window rapidly and provide no more useful information than the accessibility snapshot.

3. **Use the snapshot tool to inspect state.** `playwright_browser_snapshot` returns a structured accessibility tree — use it to read table values, button states, and text content before and after interactions.

4. **Interact to confirm behaviour.** Don't just check the page loads. Click buttons, trigger actions (start/stop/install), and confirm the resulting state change appears in a follow-up snapshot.

## Workflow

```
1. make deploy (or go build -o devctl . && make deploy)
2. playwright_browser_navigate → http://127.0.0.1:4000/<relevant-page>
3. playwright_browser_snapshot   ← read current state
4. playwright_browser_click / interact as needed
5. playwright_browser_snapshot   ← confirm state changed as expected
```

## Dashboard pages

| Path | What to check |
|---|---|
| `/services` | Service status, version strings, start/stop/install actions |
| `/sites` | Site list, create/delete, HTTPS routing |
| `/php` | PHP version selector, per-site PHP version |
| `/mail` | Mailpit iframe |
| `/dumps` | dd() dump stream |
| `/settings` | TLS trust, settings toggles |
