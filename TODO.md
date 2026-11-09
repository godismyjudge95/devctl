# IMPORTANT - READ THIS FIRST BEFORE WORKING ON TODO ITEMS
Do ONLY ONE task at a time
Once you think you've completed the item, compile and test the feature/fix in the browser on both desktop and mobile - pay attention to spacing especially on mobile
We want to do TDD so create a failing test, then make it green.  Ensure we are testing every change and updating tests as needed to cover the new functionality and all feasible edge cases (leave out cases that can't realistically happen).
The last step in completing any TODO item is to modify the README with any new features (if relevant) or update any sections that need updating (if necessary)
Once an item is completed move it to the "Completed:" section and tag a date/time to it
Add a TODO item to the end of the backlog if the item you just completed should be screenshotted (ie. added to the screenshot script) for the readme
Clean up any leftover screenshots from playwright/puppeteer sessions
Commit all files to the repo BUT DO NOT PUSH

# Backlog

- set up a devctl.test proxy domain in caddy to the devctl dashboard so we can have https - still allow access on the raw 4000 port though

# Feature ideas (notes from Yerd / Lerd comparison)

Keep browser UI + static binaries + CLI/skill (+ the MCP server). Do not add a tray/desktop shell as primary UX.
Competitor clones (optional): `/tmp/yerd`, `/tmp/lerd`.

## Generic reverse proxy / path rules

**Intent:** Let users (and services) map a path prefix on a site → upstream (same-origin websockets, e.g. `app.test/app` → Reverb). Whole-host proxy sites (`something.test` → `http://127.0.0.1:PORT`) shipped in v0.19.0.

Implemented via Caddy Admin API routes, not a second proxy.

**References:**
- Yerd proxies guide (host proxies + path rules, WebSockets): https://yerd.app/guide/proxies
- Yerd proxies CLI: https://yerd.app/reference/cli/proxies
- Lerd host-proxy sites (Node/Python/Go/etc. on host, nginx terminates `.test`+TLS): https://lerd.sh/usage/host-proxy

**Notes:** We already reverse-proxy managed service vhosts; generalize that. Path rules matter for same-origin `wss://` without CORS/cookie pain.

---

## Site / env helpers

**Intent:** One-shot “wire this project” helpers that write connection settings into:
- Laravel / Statamic-style **`.env`**
- WordPress **`wp-config.php`** (or a small include if safer)

Detect installed services (MySQL, Postgres, Valkey, Mailpit, Meili, Typesense, MaxIO, Reverb…), create DB when needed, inject credentials/keys, optional migrate / storage:link.

**References:**
- Lerd env setup (detect services, start them, create DB, write env): https://lerd.sh/features/env-setup
- Lerd project setup / link bootstrap: https://lerd.sh/features/project-setup
- Lerd WordPress getting started: https://lerd.sh/getting-started/wordpress
- Lerd Laravel getting started: https://lerd.sh/getting-started/laravel
- Yerd services (copy-ready Laravel `.env` snippets from GUI): https://yerd.app/guide/services
- Yerd sites (park/link, framework web-root detection): https://yerd.app/guide/sites

**Notes:** Prefer surgical writes (known keys only) with backup before edit. Support both frameworks from day one of this feature — not Laravel-only. CLI + browser + skill.

---

## DB lifecycle (+ one-click transfer across engines)

**Intent:** First-class create / list / drop / backup / restore for MySQL, Postgres, and SQLite. Where feasible, **one-click transfer** between engines for a site: **MySQL ↔ PostgreSQL ↔ SQLite**, then repoint Laravel `.env` / WP config.

**References:**
- Yerd DB CLI (create/list/drop/backup/restore — same SQL family only): https://yerd.app/reference/cli/db
- Lerd database guide (import/export/shell/snapshots): https://lerd.sh/usage/database
- Lerd `db:move` (same-family service → service, repoint `.env`): https://lerd.sh/reference/commands
- Lerd client shims (`mysqldump`, `pg_dump`, etc.) live in the same database doc: https://lerd.sh/usage/database

**Notes:** Cross-engine transfer is harder than Lerd’s same-family `db:move` — plan for schema/type limitations, dump→transform→load, and clear failure UX. SQLite path is especially useful for Laravel default apps. The Databases tab is the browse UI; lifecycle is management/API/CLI.

---

## Richer domain model — wildcards, folders/groups

**Intent:**
- Domain aliases + **wildcard** hosts (`*.blog.test`, worktree/multi-tenant SANs)
- **Folders / groups** for organizing sites in the UI (and optionally site groups where a main owns a base domain and secondaries are subdomains)

**References:**
- Yerd domains CLI (primary, aliases, exact subdomains, single-label wildcards): https://yerd.app/reference/cli/domains
- Yerd sites (park folders, link, groups in GUI): https://yerd.app/guide/sites
- Lerd site groups (main domain + subdomain secondaries, shared/separate DB): https://lerd.sh/usage/site-groups
- Lerd multi-domain / sites: https://lerd.sh/usage/sites
- Lerd git worktrees (wildcard cert SANs for branches — we already have worktrees; align domain model): https://lerd.sh/features/git-worktrees

**Notes:** Cosmetic folders/groups are lower risk than “main owns base domain” routing; can ship grouping first, then hierarchical domains.

---

## Command palette

**Intent:** Global **Ctrl+K** (and `/` if natural) command palette in the browser UI — jump to pages, sites, services; run common actions (start/stop, open site, doctor, share, etc.).

**References:**
- Yerd desktop command palette (Ctrl+K / ⌘K, pages + site actions): https://yerd.app/guide/desktop-app
- Lerd web UI command palette (Cmd/Ctrl+K, `/`): https://lerd.sh/features/web-ui
- Lerd also exposes framework commands via palette: https://lerd.sh/features/commands
- Lerd TUI has `:` palette (optional inspiration only — we stay browser-first): https://lerd.sh/features/tui

---

## Expanded shims and PATH

**Intent:** Richer host PATH integration under `{serverRoot}/bin` (or equivalent):
- Existing: `php`, `php{ver}`, fnm/nvm, mago, sqlite3, path-setup
- Expand: Composer (if not already seamless), **DB client shims** (`mysql`, `mysqldump`, `psql`, `pg_dump`, `redis-cli` / `valkey-cli`) pointing at tools next to managed services, optional Node/Bun tooling clarity
- Reliable `devctl path-setup` / document “managed wins when prepended”

**References:**
- Yerd tooling + PATH install: https://yerd.app/guide/tooling
- Yerd tooling CLI (`path install`, tools): https://yerd.app/reference/cli/tooling
- Lerd PHP/composer host shims: https://lerd.sh/usage/php
- Lerd DB client shims on PATH: https://lerd.sh/usage/database
- Lerd install PATH step: https://lerd.sh/getting-started/installation

**Notes:** Don’t clobber user-installed clients without consent (Lerd prompts when shadowing). Shims should default to loopback + devctl credentials when no `-h` is passed.

---

# Completed


