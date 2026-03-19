---
name: todo
description: Full workflow for picking up and completing a TODO item from TODO.md — read the backlog, clarify, implement, install, browser-test, update docs, and move the item to Completed.
license: MIT
compatibility: opencode
---

## Overview

This skill governs the end-to-end process of working through items in `TODO.md`. Every step below is **mandatory** — none may be skipped.

---

## Step-by-step workflow

### 1. Read the backlog

Open `TODO.md` and identify the **first item** listed under the `# Backlog` section. That is the item you will work on. Do not pick a different item.

### 2. Ask clarifying questions upfront

Before writing a single line of code:

- Re-read the TODO item carefully.
- Identify every ambiguity — implementation approach, scope, edge cases, UI placement, data shape, etc.
- Use the `question` tool to ask **all clarifying questions at once** in a single message. Do not drip-feed questions across multiple rounds.
- Wait for the user's answers before proceeding to implementation.

If the item is completely unambiguous, state that explicitly and ask the user to confirm before proceeding.

### 3. Implement the TODO item

- Load the relevant skills (`go-backend`, `vue-frontend`, `db-migrations`, `add-service`, `install-package`) as needed before writing code.
- Use `TodoWrite` to plan and track sub-tasks for the implementation.
- Follow all project conventions described in `AGENTS.md` and the loaded skills.
- Do not commit code unless the user explicitly asks.

### 4. Build and install

Run the following and fix any errors before continuing:

```sh
make install
sudo systemctl restart devctl
```

Check for startup errors:

```sh
sudo journalctl -u devctl -n 40 --no-pager
```

Do not proceed to testing until the service is running cleanly.

### 5. Test in the browser

Load the `testing-dashboard` skill, then:

- Open `http://127.0.0.1:4000` using the Playwright browser tool.
- Navigate to the area affected by the change.
- Verify the feature works end-to-end — not just that the page loads.
- Check for console errors, broken layout, and unexpected behaviour.
- If anything is broken, fix it and repeat steps 4–5 until the test passes.

### 6. Update the README

Open `README.md` and make targeted updates:

- **New feature** → add a description under the relevant section (or create a new section).
- **Changed feature** → update the existing description.
- **Removed feature** → delete or strike the entry.

Only update sections that are actually affected. Do not rewrite unrelated content.

### 7. Update CHANGELOG.md

If `CHANGELOG.md` does not exist, create it with an `# Unreleased` section at the top.

Add one or more bullet points under `# Unreleased` (or today's date heading if the file uses date-based sections) that describe what was added, changed, or fixed. Keep entries concise and user-facing (e.g. `- Added spx profile viewer with timeline and flamegraph`).

### 8. Move the item to Completed

In `TODO.md`:

1. Remove the bullet from the `# Backlog` section.
2. Append it to the `# Completed` section with a date/time stamp in the format:

```
- <original item text> *(completed YYYY-MM-DD)*
```

---

## Checklist

Use this as a final gate before declaring the task done:

- [ ] First backlog item identified
- [ ] All clarifying questions asked and answered upfront
- [ ] Implementation complete and compiles cleanly
- [ ] `make install` succeeded
- [ ] `sudo systemctl restart devctl` running without errors
- [ ] Feature verified end-to-end in browser via Playwright
- [ ] README updated for new/changed/removed functionality
- [ ] CHANGELOG.md updated with user-facing bullet points
- [ ] TODO item moved to `# Completed` with date stamp
