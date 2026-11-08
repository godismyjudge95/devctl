# Competitor parity plan

Status: approved by the owner on 2026-11-06. Phases 0 to 3 are done on `main` (2026-11-08). Item 4.1 has a feasibility note (`service-instances-note.md`) that recommends to stop. D1 and D2 still wait for an owner decision.

Open after the build:

- 2.2 Xdebug ships with the next PHP binaries release (`xdebug-spike.md`). Linux PHP 8.x binaries now link against glibc (Ubuntu 24.04+ / Debian 13+, glibc 2.38 symbols) so they can load `xdebug.so`. `scripts/prove-glibc-xdebug.sh` passed on arm64 Ubuntu 26.04.
- 3.1 The request inspector covers PHP 8.1+ (8.2+ for apps without Laravel), with no C extension (`request-inspector-spike.md`).

## Goal

Close the feature gaps between devctl and its three competitors (Laravel Herd, Lerd, Yerd) that the owner approved. Keep the devctl identity: one static binary, no containers, web UI first, Linux and macOS.

The gap research (2026-11-06) compared Herd 2.1 docs, Lerd at commit `6d19a46`, and Yerd `main` (2.1.0-rc.1) with the devctl code.

## Rules for every item

- Read `AGENTS.md` first. Load the project skill for the area (`go-backend`, `vue-frontend`, `db-migrations`, `add-cli-command`, `add-service`, `integration-testing`).
- **Never run tests on the host.** Compile only (`go test -c`). Run tests in the Incus container (Linux) or the OrbStack VM (macOS, `make test-env`). Destroy test machines when you finish.
- Every API change gets a `tests/api/` integration test. Every Go package change gets a unit test. Every UI change gets a `tests/e2e/` Playwright test.
- Every new CLI command: add it to the registry, support `--json`, update the README CLI table and the generated skill (`devctl devctl:skill` output).
- All frontend REST calls go through `frontend/src/lib/api.ts`.
- Never hardcode paths. Use the `paths` package.
- One phase item = one branch and one reviewable change. Do not mix items.
- The working tree had uncommitted changes from other work on 2026-11-06 (`CHANGELOG.md`, `README.md`, `install/mysql.go`, `install/postgres.go`, `main.go`, `selfinstall/*`, `install/*_wrapper_test.go`). Do not revert them and do not include them in your commits. Ask the owner what to do with them before you start.
- Ask the owner before you commit, push, or open a PR.

## Out of scope

| Item | Reason |
|---|---|
| MariaDB | Tried before. Cross-platform binary problems. |
| Tray or menu-bar app | devctl is web UI first. The PWA covers this need. |

## Needs an owner decision before work starts

These two items are explained here. Do not start them until the owner says yes.

### D1. User proxy sites

A user makes a `.test` domain that sends traffic to a local port or URL. Example: `vite.test` → `http://127.0.0.1:5173`, or `myapi.test` → a Node or Go dev server on port 3000. The site gets HTTPS from Caddy like any other site. Herd (`herd proxy`), Lerd, and Yerd (`proxy add`) all have this.

devctl already has most of the code. `sites/manager.go` has `SiteType: "ws"` with `WSUpstream`, but only service vhosts (`reverb.test`, `s3.maxio.test`) use it. The work is: allow the user to create that site type from the UI and the CLI (`sites:proxy add <domain> <url>`), and allow plain HTTP upstreams, not only WebSocket.

### D2. Worktree database mode

Today a new worktree shares the parent database (`DB_DATABASE` is not rewritten). A migration in the branch therefore changes the data of the main checkout. "Database mode" is a choice at worktree creation:

- `shared` (the current behavior)
- `clone`: copy the parent database to `<db>_<branch-slug>` and point the worktree `.env` at the copy
- `empty`: create an empty `<db>_<branch-slug>`, optionally run `php artisan migrate`

Herd and Lerd both have this. devctl already has database duplicate (`POST /api/databases/{engine}/databases/duplicate`), so `clone` reuses that code.

---

## Phase 0: README

### 0.1 Fix the comparison table

`README.md:63-98` has wrong values. Correct these cells:

| Row | Old | New |
|---|---|---|
| Herd: Git worktrees | ❌ | ✅ (Workspaces, Herd 2.0) |
| Herd: SPX profiler | ❌ | ✅ (macOS) |
| Herd: Meilisearch, Typesense | ❌ | ✅ (Pro) |
| Herd: Object storage | ❌ | ✅ (MinIO / RustFS, Pro) |
| Lerd: ClickHouse, Typesense | ❌ | ✅ (container presets) |
| Lerd: Database GUI | ✅ | Partial (phpMyAdmin / pgAdmin presets) |
| Yerd: Meilisearch, Reverb | ❌ | ✅ |
| Yerd: AI integration | ❌ | MCP |
| devctl: Windows | WSL2 | Verify. Requirements do not list WSL2. Remove it if it is not supported. |

Update the table again at the end of each phase as devctl gains features.

---

## Phase 1: quick wins

### 1.1 Database import

- **Now:** export exists only for one table (`handleExportDB`, `api/databases.go:622`). No import exists.
- **Add:**
  - Import a `.sql` (and `.sql.gz`) file into a database, for MySQL and PostgreSQL. Stream the upload to the engine CLI (`mysql`, `psql`) through the wrappers in `{serverRoot}/bin`. Do not load the full file into memory.
  - Export a full database (not only one table) as SQL with `mysqldump` / `pg_dump`.
  - UI: "Import SQL…" and "Export database…" on the database context menu in `DatabasesView.vue`. Show progress.
  - Option to create the target database if it does not exist.
- **Accept:** a 500 MB dump imports without a memory spike. Errors from the engine show in the UI.

### 1.2 `db:*` CLI commands

Add a `db` namespace (`cli/db.go`) on top of the existing database API:

| Command | Action |
|---|---|
| `db:list [engine]` | List databases |
| `db:create <engine> <name>` | Create |
| `db:drop <engine> <name> [--force]` | Drop. Refuse system databases (`mysql`, `sys`, `information_schema`, `performance_schema`, `postgres`, `template0`, `template1`). |
| `db:duplicate <engine> <src> <dst>` | Copy |
| `db:import <engine> <name> <file>` | Import (uses 1.1) |
| `db:export <engine> <name> [file]` | Full dump (uses 1.1). Write to stdout when no file. |
| `db:query <engine> <name> <sql>` | Run SQL, print a table or JSON |
| `db:connection [domain]` | Print the connection details and URL for the site in the cwd (read its `.env`) |

When the cwd is a site, `engine` and `name` default to the site `.env` (`DB_CONNECTION`, `DB_DATABASE`).

### 1.3 `postgresql.conf` tab in the config editor

- PostgreSQL is not in the config editor list. Add `postgresql.conf` and `pg_hba.conf` (two tabs, like ClickHouse) from `{serverRoot}/postgres/data/`.
- Save & Restart must work.

### 1.4 PHP ini settings for each version

- **Now:** the Global PHP Settings panel and `php:set` apply to all versions (`php.ApplySettings`).
- **Add:**
  - Optional version scope: `PUT /api/php/settings?version=8.4` and `devctl php:set memory_limit=1G --version=8.4`.
  - Free-form directives: `devctl php:ini:set <version|all> <key> <value>`, `php:ini:unset`, `php:ini:list`. Write them in a devctl-managed block or a conf.d file so that user edits elsewhere in `php.ini` stay.
  - UI: a version selector on the PHP settings panel (All / 8.3 / 8.4 …). Show which values differ from the global value.
- 2.2 (Xdebug) uses this.

### 1.5 Mail CLI

devctl proxies Mailpit (`api/mail.go`). The Mailpit API has search (`to:`, `from:`, `subject:`, `after:`). Add:

| Command | Action |
|---|---|
| `mail:list` filters | `--to`, `--from`, `--subject`, `--since` |
| `mail:latest` | The newest message (full content) |
| `mail:wait [filters] [--timeout=30s]` | Block until a matching new message arrives, then print it. Exit non-zero on timeout. Agents and tests use this for sign-up flows. |
| `mail:links <id\|latest>` | Print all URLs in the message (HTML and text parts) |
| `mail:get --format=text\|html\|raw\|json` | Format choice |
| `mail:attachments <id>`, `mail:attachment <id> <part> --out <file>` | List and save attachments |

### 1.6 `doctor` command

- `devctl doctor [--json]` and `devctl doctor:fix`. Also a Doctor panel in the UI (Settings or its own page).
- Use a check registry (a table of checks), not one long function. Each check has an ID, a severity (ok, warn, fail), a message, a remedy, and an optional safe fix.
- Exit code 1 when any check fails, so CI can use it.
- First checks:
  - daemon reachable; elevate daemon running; Caddy running and admin API reachable
  - ports 80/443 bound by Caddy, not by another program
  - DNS server answers `*.test`; resolver installed (`elevate:status`)
  - Caddy CA trusted by the OS; PHP CA bundle current (fix: rebuild it)
  - at least one PHP version installed; each FPM pool running (fix: restart the pool)
  - every site points at an installed PHP version; every site root exists
  - `{serverRoot}/bin` on the user PATH
  - service config files parse (where a check is cheap)
  - devctl update available (warn)
  - disk space on the server root (warn)
- Fixes must be safe and unprivileged. For a fix that needs root, print the `sudo devctl elevate …` command.

---

## Phase 2: core features

### 2.1 MCP server

- **Context budget is a hard requirement.** The owner did not add MCP before because of context cost. Popular servers now save context, so devctl must too:
  - A small set of grouped tools (target 8–12), each with an `action` argument (the Lerd pattern), not one tool for each endpoint.
  - Short tool descriptions. Put the detail in an MCP resource or a `help` action that returns it on demand.
  - Large outputs (logs, rows, dumps) are paged and truncated by default.
- **Transport:** `devctl mcp` (stdio) is a thin client of the daemon HTTP API, the same as the CLI. Reuse the CLI client and the command registry where you can, so the CLI and MCP do not drift. Also consider a streamable-HTTP endpoint on the daemon (`/mcp`).
- **Tool groups (first draft):** `sites`, `services`, `php`, `db`, `mail`, `logs`, `dumps` (and later `requests`), `doctor`, `worktrees`.
- **Safety:** destructive actions (drop, delete, clear, uninstall) need an explicit `confirm: true`.
- **Install:** `devctl mcp:install` prints or writes the config for Claude Code, Cursor, OpenCode, Zed, and VS Code. Keep `devctl devctl:skill` working.
- **Accept:** measure the token size of the `tools/list` response and report it. Target under 3k tokens.

### 2.2 Xdebug toggle

- **Risk first.** devctl PHP is a static build from static-php-cli. `scripts/patch-spc-for-php8.sh` notes that spc marks xdebug as shared-only. A static binary may not be able to load a shared `.so`. Do a spike first and answer:
  1. Can the current Linux and macOS builds `dlopen` a shared extension?
  2. If not, can spc build xdebug statically (patch it to `["static", "shared"]` like pcov)?
  3. Does a built-in but disabled Xdebug cost performance? (`xdebug.mode=off` costs almost nothing in Xdebug 3.)
- **Feature (after the spike):**
  - Toggle Xdebug for each PHP version (UI and `devctl php:xdebug <version> on|off [--mode=debug,develop,coverage,profile,trace]`). Restart only that FPM pool.
  - Trigger mode as the default (`xdebug.start_with_request=trigger`), so the browser extension decides.
  - Optional stretch: a second FPM pool with Xdebug, chosen only when the request has the `XDEBUG_SESSION` cookie (Herd Pro "Xdebug detection"). Normal requests stay fast.
- This needs a new PHP binaries release. Use the `create-release` skill.

### 2.3 Capture normal `dump()` / `dd()`

- **Verify first.** `php/prepend.php` already defines `dd()` and `dump()` when they do not exist. Prepend runs before Composer autoload, so these possibly already win over symfony/var-dumper. Find out exactly which calls devctl misses today. Probable gaps:
  - `VarDumper::dump()` called directly (Laravel `->dump()` / `->dd()` on collections, query builders, `Str`)
  - Laravel `ddd()`, Ray (`ray()`)
  - CLI and queue workers (does the CLI `php` wrapper load the prepend?)
- **Fix:** register a VarDumper handler (`VarDumper::setHandler`) when the class loads, so that every VarDumper dump reaches devctl. Keep the output in the page optional (setting: "also dump to the page").
- Rename the docs from "`php_dd()`" to "dumps", and keep `php_dd()` as an alias.

### 2.4 Per-site Node version

- fnm is already a helper. Use it; do not add a second Node manager.
- Read the site `.nvmrc` / `.node-version` / `package.json` `engines.node`. Show the Node version on the site card and in site settings. Allow a change (writes `.nvmrc`).
- Node install / list / uninstall from the UI and CLI (`node:list`, `node:install <ver>`).
- `{serverRoot}/bin/node`, `npm`, `npx` shims that pick the version of the site in the cwd (`fnm exec --using`).
- Worker supervision (3.2) uses this to run Vite with the right Node version.

### 2.5 Public tunnel (share a site)

- Use a cloudflared quick tunnel first: no account, random `trycloudflare.com` URL. Download cloudflared as a helper (the helpers system already does downloads, checksums, and updates).
- Caddy must accept the tunnel Host header and pass the original site host to PHP. Check what `APP_URL` and signed URLs do; document it.
- UI: a Share button on the site card. Show the public URL with a copy button and a Stop button. CLI: `sites:share <domain>`, `sites:unshare <domain>`, `sites:shares`.
- Stretch: named Cloudflare tunnels on the user domain; ngrok and Expose as other providers.

---

## Phase 3: large features

### 3.1 Request inspector with N+1 detection

- **Target:** one view for each request: SQL queries (SQL, bindings, duration, caller file and line), dumps, logs, exceptions, dispatched jobs, cache events, outgoing HTTP, views. Problem flags: N+1 queries, duplicate queries, slow queries, slow requests. No change to the app code.
- **Design spike (required):** Herd, Lerd, and Yerd all use a C extension. Compare at least two options and prototype the best one before you build:
  1. A PHP userland agent loaded by `prepend.php` that hooks Laravel when it boots (for example a booted callback or a service provider added through the autoloader), with a generic PDO fallback.
  2. A small C extension built into the static PHP binaries (Zend observer API on PDO and functions). More work; works with any framework.
- **Transport:** reuse the TCP dump receiver (`dumps/tcp.go`, port 9912) or add a message type. Group events by a request ID.
- **N+1 rule:** the same normalized SQL from the same caller N times in one request (N ≥ 3, configurable).
- **UI:** a Requests page (list → request detail with tabs). Click a file:line to open it in the editor (`vscode://`, `phpstorm://` URL setting).
- **CLI and MCP:** `requests:list`, `requests:show <id>`, `requests:watch --json` (NDJSON), `requests:problems`.
- This also covers "parsed log entries" for the request. A full Laravel log viewer is a separate later item.

### 3.2 Worker supervision

- Supervise processes for each site with the existing `services.Supervisor` (restart with backoff, logs in `{serverRoot}/logs/`, visible in the Logs page).
- Built-in worker types: `queue` (`artisan queue:work`), `horizon`, `schedule` (`artisan schedule:work`), `vite` (`npm run dev`, with the Node version from 2.4), and `custom` (any command).
- Run with the PHP version of the site (the `php{version}` wrapper) and as the site user.
- UI: a Workers section in site settings and on the site card (start, stop, restart, status, last log lines). CLI: `workers:list [domain]`, `workers:start <domain> <type>`, `workers:stop`, `workers:restart`, `workers:logs`.
- Store workers in SQLite (goose migration + sqlc). Restart them after a daemon restart.
- Notify (browser notification) when a worker crashes several times.
- Vite HMR: check that `https://site.test` pages can reach the Vite dev server (CORS and TLS). Consider a Caddy route.

### 3.3 Project config file (opt-in)

- **Off by default.** A setting turns it on: `project_config_enabled` (global). When it is off, devctl does not read the file.
- File: `.devctl.yaml` in the site root. Optional `.devctl.local.yaml` (not committed) overrides it.
- First keys: `php`, `node`, `public_dir`, `https`, `cors`, `aliases`, `spx`, `workers` (from 3.2), `services` (services the project needs; devctl offers to install and start them).
- devctl applies the file when it discovers the site and when the file changes (the fsnotify watcher exists). The UI shows "managed by .devctl.yaml" next to the values the file controls.
- `devctl sites:config:init` writes a file from the current site settings.
- Define a JSON Schema for the file so editors can validate it.

### 3.4 New project wizard

- UI: "New site" with a template choice. CLI: `sites:new <name> --template=…`.
- Templates (first set): Laravel (starter kit none / React / Vue / Svelte / Livewire, Pest or PHPUnit, database SQLite / MySQL / PostgreSQL), Statamic, WordPress (download, `wp-config.php`, database, admin user, through `wp` CLI), plain PHP.
- The Laravel installer and the Statamic CLI are already installed globally with PHP. Use them.
- Stream progress (SSE, like service installs). On success: create the database, write `.env` (DB, mail → Mailpit, Redis → Valkey), register the site, open it.

---

## Phase 4: stretch

### 4.1 More versions and instances of a service

The owner said: "maybe, but not needed if too difficult". Do a short feasibility note first (per service: can two versions share the install layout, port allocation, data directories, and the `Definition` model?). Stop and report if the cost is high.

---

## Suggested order

1. 0.1
2. 1.1 → 1.2 (1.2 uses 1.1)
3. 1.3, 1.4, 1.5, 1.6 (independent)
4. 2.1 (MCP; after 1.x so the new commands are in it)
5. 2.3, 2.2 spike, 2.4, 2.5
6. 3.1 spike, 3.2, 3.1, 3.3, 3.4
7. 4.1 note
