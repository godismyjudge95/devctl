# Changelog

## Unreleased

- Added `make demo` / `make demo-screenshots` targets and `scripts/demo.sh` to spin up a fresh Incus demo container with seeded sites, PHP dumps, mail, SPX profiles, and MaxIO files, then take full-dashboard screenshots automatically
- Updated `scripts/screenshots.js` to capture all dashboard pages (Services, Sites, Dumps, Mail, SPX, Logs, Settings, MaxIO, WhoDB) at both desktop and mobile viewports; `BASE_URL` is now configurable via env var
- Added Logs section to README documenting the real-time log viewer tab
- Added screenshots to Mail, SPX Profiler, Logs, MaxIO, and WhoDB README sections
- Added all mobile screenshots to the Contributing → Screenshots section of the README

## v0.5.0 — 2026-04-07

- Added **dev tools auto-download**: the `sqlite3` CLI and `fnm` (Fast Node Manager, aliased as `nvm`) are now automatically downloaded to `{serverRoot}/bin/` (which is on `$PATH`) during `devctl install` and after each self-update. The latest version is fetched from the upstream source (sqlite.org / GitHub) and the download is skipped when the installed binary is already up-to-date. New tools can be added to `tools/tools.go` without changes to the install flow.

## v0.4.0 — 2026-04-07

- Added **auto-updater**: devctl now checks GitHub for a newer release once per day at 3 am. When an update is available, an amber upgrade button appears next to the devctl logo in the sidebar. Clicking it opens a live progress dialog, downloads and verifies the new binary, backs up the current binary, swaps it, and restarts the service via systemd. The backup is cleaned up automatically on next successful startup.
- Replaced RustFS with MaxIO — S3-compatible object storage now uses the MaxIO binary from coollabsio/maxio; vhosts renamed to maxio.test / s3.maxio.test; Laravel connection.env keys unchanged (AWS_*)
- Added **CLI** (`devctl <namespace>:<command>`): the devctl binary now doubles as a CLI that communicates with the running daemon at `127.0.0.1:4000` (or `$DEVCTL_ADDR`) without requiring root. All 30+ operations from the old MCP server are available as colon-namespaced commands: `services:list`, `services:restart <id>`, `sites:list`, `sites:php <domain> <ver>`, `logs:tail <id>`, `mail:list`, `settings:set key=val`, `php:set memory_limit=512M`, `dns:status`, `dumps:list`, `spx:profiles`, `tls:trust`, etc. Every command supports `--json` for machine-readable output.
- Added **OpenCode skill auto-generation**: run `devctl devctl:skill` to write `~/.agents/skills/devctl-cli/SKILL.md`. The daemon silently regenerates the file on each startup if it already exists.
- Added **OpenCode skill prompt during install**: `devctl install` now offers an optional "Install OpenCode skill?" prompt after the service starts (skipped with `--yes`).
- Removed **MCP server**: the `/mcp` endpoint and `mcpserver/` package have been removed. The CLI replaces all MCP tooling with no external protocol dependency.
- Fixed: `laravel` and `statamic` CLI binaries (installed via `composer global require`) are now accessible both in the user's interactive shell and in all commands devctl runs internally as the site user. devctl appends a `PATH` export block to `.bashrc`, `.zshrc`, and `.bash_profile` at install time, and prepends the Composer global bin directory (`{siteHome}/.config/composer/vendor/bin`) to `PATH` for every command it runs as the site user via `sudo`.
- Fixed: text-only emails (no HTML part) now render their plain text content in the HTML tab instead of showing "No HTML content"
- Fixed: MySQL service no longer flickers to "warning" status immediately after a restart; the health check now retries up to 6 times (3 seconds total) before reporting a restart, giving mysqld time to bring its socket up
- Fixed: auto-discovered sites are now assigned the latest installed PHP version instead of a hardcoded "8.3" default
- Fixed: manually removing a site's directory from disk now automatically deregisters it — stale sites are pruned on startup and removed in real-time via the filesystem watcher

## v0.3.0 — 2026-03-23

### RustFS — S3-compatible object storage

- Added **RustFS** as an installable managed service. devctl downloads the binary from `dl.rustfs.com`, writes a `config.env` with generated credentials, and registers two Caddy vhosts: `rustfs.test` (console proxy) and `s3.rustfs.test` (S3 API proxy on port 9000).
- RustFS runs as a supervised child process on `127.0.0.1:9000` (S3 API) and `127.0.0.1:9001` (web console). The native console is disabled; devctl's own UI is used instead.
- Data is stored at `{serverRoot}/rustfs/data`. Default credentials: `RUSTFS_ACCESS_KEY=devctl` / `RUSTFS_SECRET_KEY=devctlsecret` — edit `config.env` to customise.
- A `connection.env` is written at install time with Laravel-compatible AWS keys (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_ENDPOINT`, `AWS_USE_PATH_STYLE_ENDPOINT`, etc.) — copy these into your `.env` to use RustFS with Laravel's filesystem driver.
- New **Storage** sidebar item opens a full in-browser S3 file manager (`/rustfs`):
  - Three-panel layout: bucket list, folder tree, and object table.
  - Drag-and-drop upload with overlay; drag rows onto tree folders to move or copy (hold Ctrl to copy).
  - Recursive folder download as ZIP (assembled client-side using `fflate`).
  - Sortable columns, search/filter, breadcrumb navigation, lazy-loading folder tree.
  - Context menus on buckets and tree nodes; floating action bar for upload / new folder / download / delete.
  - Server info panel: disk usage, bucket count, object count, uptime, version.
- A server-side AWS Signature V4 proxy (`/api/rustfs/s3/` and `/api/rustfs/admin/`) signs all S3 requests so credentials never leave the server. Presigned download URLs are generated via `GET /api/rustfs/presign`.

### WhoDB — database explorer

- Added **WhoDB** (v0.100.0) as an installable managed service. devctl downloads the binary, writes a `config.env`, and registers a `whodb.test` Caddy reverse-proxy vhost.
- WhoDB runs on `127.0.0.1:8161` and is embedded in the dashboard as a full-page iframe via the new **WhoDB** sidebar link.
- devctl automatically writes pre-populated connection profiles for any installed database service: MySQL, PostgreSQL, and Valkey/Redis.
- A **hook system** (`HookRegistry`) regenerates WhoDB's `config.env` whenever another service is installed or purged, keeping connections up to date automatically.
- New **WhoDB section** in Settings: toggle `WHODB_DISABLE_CREDENTIAL_FORM`, view auto-detected connections (read-only), and manage manual connections (add / edit / delete). Changes are persisted in the SQLite database and applied immediately.

### Service updater system

- Each managed service now reports a **latest available version** by querying its upstream: GitHub Releases API for Caddy, Valkey, Meilisearch, Typesense, Mailpit, RustFS, WhoDB; Packagist for Laravel Reverb; no-op for MySQL, PostgreSQL, and DNS.
- devctl runs an **update checker once per day at 3 am**. Results are cached in memory and pushed into every `ServiceState` response as `latest_version` and `update_available`.
- The **Services page** shows an amber **"Update"** button in the action bar and a `update_available` badge on mobile cards whenever a newer version is detected. Hovering shows a tooltip with the exact from/to version.
- A **browser notification** fires once per service when an update is first detected (notification permission is requested on first visit to the Services page).
- Updates stream output in real time over SSE (same pattern as install/purge).
- Per-service update logic:
  - *Caddy, Mailpit, Typesense, Valkey, RustFS, WhoDB* — stop, replace binary, restart.
  - *Meilisearch* — dump data via API, replace binary, import dump, restart.
  - *MySQL* — re-download and extract `.deb` packages in-place, restart.
  - *PostgreSQL* — re-download and extract tarball in-place, restart.
  - *Laravel Reverb* — `composer update laravel/reverb`, restart.
  - *DNS* — no-op (built-in, nothing to update).

### Full-screen config editor

- New **config editor** for all config-enabled services (Valkey, MySQL, Meilisearch, Typesense, Mailpit, PHP-FPM). Click the file icon on any service row to open a full-screen CodeMirror 6 editor with syntax highlighting, line numbers, and Ctrl+F search.
- PHP-FPM shows two tabs — `php.ini` and `php-fpm.conf` — switchable without leaving the editor.
- **Save & Restart** button (or Ctrl+S) writes the file to disk and immediately restarts the affected service.
- INI/conf/env files use INI syntax highlighting; Meilisearch's `config.toml` uses TOML highlighting.
- The old "Config" tab in service settings dialogs has been removed in favour of the dedicated editor route (`/services/:id/config/:file`).

### Services page action bar redesign

- Per-row actions are now a joined **ButtonGroup**: Start/Stop, Restart, Update (amber, shown only when `update_available`), and a `...` overflow dropdown.
- Settings, "Edit config", and Uninstall are now inside the overflow dropdown, reducing visual noise on the default view.
- The per-row Logs button has been removed; use the new **Logs** page instead.

### SPX Profiler — speedscope flamegraph & trace parser rewrite

- Replaced the hand-rolled SVG flamegraph and Timeline tabs with an embedded [speedscope](https://github.com/jlfwong/speedscope) iframe (v1.25.0, MIT).
- The **SPX trace format changed** in recent SPX builds. The parser was completely rewritten to handle the new `[events]` / `[functions]` section format (was `# func` headers with `+/-` depth markers). JSON metadata fields were also renamed (`exec_ts`, `http_method`, `called_function_count`, etc.).
- SPX traces are converted server-side to speedscope's `SampledProfile` JSON format — stack paths are aggregated with exclusive wall-time weights. Response is gzip-compressed.
- A `maxFlameEvents = 5000` cap prevents OOM on very large traces.
- The flat-profile table was rewritten with virtual scrolling (`useVirtualList` from VueUse), sticky headers, and a row-count footer.
- Speedscope features: Time Order, Left Heavy, Sandwich views; minimap; zoom/pan; search; WebGL-accelerated rendering.
- New API endpoint: `GET /api/spx/profiles/{key}/speedscope`. New static route `/speedscope/` serves the embedded assets.
- Fixed SPX profiler list card overflowing the viewport on mobile — `overflow-hidden` and `min-w-0` applied; URL truncates with ellipsis.

### PHP-FPM `php.ini` fix

- PHP-FPM workers are now launched with `-c {dir}/php.ini`. Without this flag, FPM workers ignored the devctl-managed `php.ini`, meaning SPX profiler settings and any custom INI values were silently not applied.

### Centralised log directory + Logs page

- All managed services now write logs to `{serverRoot}/logs/<service>.log`. Previously logs were scattered across service directories or missing entirely.
- Logs rotate automatically at 10 MB with 3 backup files kept (`.log.1`, `.log.2`, `.log.3`) — no system `logrotate` dependency.
- The DNS server now has a dedicated `dns.log`; previously it had none, causing a startup error.
- New **Logs** page (`/logs`): sidebar lists all log files; clicking a file streams it live over SSE with auto-scroll and error-line highlighting. A **Clear** button truncates the selected file.

### Native config files for all services

- Valkey now starts with a full `valkey.conf` (based on the official Valkey default config) instead of CLI flags. Written once on install, user-editable.
- Meilisearch now uses a `config.toml` (based on the Meilisearch v1.37.0 default). The master key is injected from `config.env`.
- Typesense now uses a `typesense.ini` (documented INI format). The API key is injected from `config.env`.
- Mailpit is now configured via `MP_*` environment variables in `config.env` (no CLI flags). **Breaking change:** a separate `connection.env` now holds the Laravel keys (`MAIL_MAILER`, `MAIL_HOST`, `MAIL_PORT`, `MAIL_ENCRYPTION`) shown in the credentials panel — the old `config.env` was previously shown there.
- PHP `php.ini` is now seeded from the full upstream `php.ini-development` template on first install, with devctl overrides appended: dev-friendly resource limits, OPcache with `validate_timestamps=1`/`revalidate_freq=0`, SPX profiler configuration, and `auto_prepend_file`. The file is user-editable and never overwritten on restart.
- **Breaking change (Meilisearch):** `MEILI_MASTER_KEY` → `MEILISEARCH_KEY` and `MEILI_HOST` → `MEILISEARCH_HOST` in `config.env`. Update your `.env` files accordingly.
- **Breaking change (Typesense):** `config.env` now has separate `TYPESENSE_HOST`, `TYPESENSE_PORT=443`, and `TYPESENSE_PROTOCOL=https` keys (Laravel Scout format) instead of a combined URL.
- Startup migration: existing installs of Valkey, Meilisearch, or Typesense that lack a config file will have one written automatically on next devctl startup.

### MySQL plugin and ICU data fix

- Fixed MySQL startup error: `Can't open shared library component_reference_cache.so`. The installer now extracts `usr/lib/mysql/plugin/*.so` from the deb package.
- Fixed MySQL startup warning about missing ICU regex data — `icudt77l/` is now correctly extracted to `lib/mysql/private/`.
- Existing installations are automatically repaired on next devctl startup via an `EnsureMySQLPlugins` migration (idempotent no-op after first run).

### MCP server

- devctl now runs a **Model Context Protocol (MCP) server** at `/mcp` using the StreamableHTTP transport.
- Exposes **24 tools** covering all major devctl operations: site management, service control, PHP settings, DNS setup, SPX profiling, dump inspection, log access, and settings.
- Exposes **5 resources** (`devctl://sites`, `devctl://services`, `devctl://php`, `devctl://dumps`, `devctl://sites/{domain}`) and **3 prompts** (`DiagnoseSiteIssue`, `EnableProfiling`, `ServiceHealthCheck`).
- AI agents and MCP-compatible clients can now drive devctl programmatically without using the browser UI.

### Navigation changes

- Added **Logs** nav item (always visible).
- Added **WhoDB** nav item (visible when WhoDB is installed); navigating to `/whodb` while uninstalled redirects to Services.
- Added **Storage** nav item (visible when RustFS is installed); navigating to `/rustfs` while uninstalled redirects to Services.
- Root container height changed from `h-screen` to `h-dvh` (fixes mobile viewport height on browsers with dynamic toolbars).

### UI polish & bug fixes

- Dark mode completeness: all hardcoded colours in `DumpNode.vue` given `dark:` variants.
- `SitesView` mobile cards refactored to `<Card><CardContent>` with `min-w-0` + `truncate` on domain links.
- `DumpsView`, `SitesView`, and `ServicesView` header rows use `flex-wrap gap-y-2` for better mobile layout.
- `MailView` toolbar buttons standardised to `icon-xs` / `icon-sm` sizes; nested `ButtonGroup` structure flattened.
- New `--success` / `--success-foreground` CSS custom properties for consistent success-state colouring.
- New `icon-xs` (`size-7`) and `icon-sm` button size variants used consistently across toolbars.
- New shadcn-vue components: Breadcrumb, ContextMenu, DropdownMenu (used by RustFS file manager and Services action bar).
- Unknown `/api/*` paths now return 404 instead of falling through to the SPA handler.

## v0.2.1 — 2026-03-19

### SPX Profiler — custom PHP binaries

Patch release to build and publish custom PHP binaries with SPX profiler support.

## v0.2.0 — 2026-03-19

### SPX Profiler — speedscope flamegraph

- Replaced the hand-rolled SVG flamegraph and Timeline tabs with an embedded [speedscope](https://github.com/jlfwong/speedscope) iframe (v1.25.0, MIT).
- SPX traces are converted server-side to speedscope's `SampledProfile` JSON format, aggregating ~1.8M raw events into unique stack paths with exclusive wall-time weights. Response is gzip-compressed and typically a few hundred KB.
- New API endpoint: `GET /api/spx/profiles/{key}/speedscope`
- New static asset route: `/speedscope/` serves the embedded speedscope assets without SPA interference.
- Speedscope features available: Time Order, Left Heavy, Sandwich views; minimap; zoom/pan; search; WebGL-accelerated rendering.
- Fixed SPX profiler list card overflowing the viewport on mobile (375px) — added `overflow-hidden` and `min-w-0` constraints; URL now truncates with ellipsis.
- Fixed `<main>` allowing horizontal scroll on mobile (`overflow-x-hidden`).
