# Changelog

## Unreleased

### Service updater system

- Each managed service now has a **`LatestVersion()`** check that queries the appropriate upstream (GitHub releases API for Caddy, Valkey, Meilisearch, Typesense, Mailpit; Packagist for Laravel Reverb; static no-op for MySQL, PostgreSQL, and DNS).
- devctl runs an **update checker on startup and daily at 3 am** — latest versions are cached in memory and injected into every `ServiceState` response as `latest_version` and `update_available`.
- The **Services page** shows an amber **"update"** badge next to the version string and an **"Update"** button in the actions column whenever a newer version is available.
- Hovering the Update button shows a **tooltip** with the exact from/to version (e.g. *"Update from 2.10.0 to v2.11.2"*).
- A **browser notification** is fired when updates are first detected (requires notification permission, requested on first visit to the Services page).
- **Per-service update logic:**
  - *Caddy, Mailpit, Typesense, Valkey* — stops the process, replaces the binary, then the API handler restarts it.
  - *Meilisearch* — fully autonomous: dumps data, replaces binary, imports dump, then the API handler restarts the process.
  - *MySQL* — re-downloads and extracts the `.deb` packages in-place; the API handler restarts MySQL afterward.
  - *PostgreSQL* — re-downloads and extracts the tarball in-place; the API handler restarts PostgreSQL afterward.
  - *Laravel Reverb* — runs `composer update laravel/reverb` then the API handler restarts the process.
  - *PHP FPM* — update is handled via the PHP updater subsystem; binaries are replaced then FPM is restarted.
  - *DNS* — no-op (version is managed by the system).
- The update stream uses the same SSE pattern as install/purge, so output is visible in real time.

### Full-screen config editor

- New **config editor** for all config-enabled services (Valkey, MySQL, Meilisearch, Typesense, Mailpit, PHP-FPM). Click the file icon on any service row to open a full-screen CodeMirror 6 editor with syntax highlighting, line numbers, and Ctrl+F search.
- PHP-FPM shows two tabs — `php.ini` and `php-fpm.conf` — switchable without leaving the editor.
- **Save & Restart** button (or Ctrl+S) writes the file to disk and immediately restarts the affected service.
- INI/conf/env files use INI syntax highlighting; Meilisearch's `config.toml` uses TOML highlighting.
- The old "Config" tab in service settings dialogs has been removed in favour of the dedicated editor route (`/services/:id/config/:file`).

### MySQL plugin and ICU data fix

- Fixed MySQL startup error: `Can't open shared library component_reference_cache.so`. The MySQL installer now also extracts `usr/lib/mysql/plugin/*.so` (28 plugin files) from the deb package, which MySQL 8.4 requires at startup.
- Fixed MySQL startup warning: `Missing data directory for ICU regular expressions`. ICU data (`icudt77l/`) is now correctly extracted to `lib/mysql/private/` where MySQL 8.4 expects it.
- Existing installations are automatically repaired on next devctl startup via the `EnsureMySQLPlugins` migration (downloads the server-core deb, extracts missing files, then becomes a no-op on subsequent restarts).

### Native config files for all services

- Valkey now starts with a full `valkey.conf` (based on the official Valkey 9.0.3 default config) instead of CLI flags. The file is written once on install and is user-editable.
- Meilisearch now uses a `config.toml` (based on the official Meilisearch v1.37.0 default config). The master key is stamped in from `config.env`.
- Typesense now uses a `typesense.ini` (documented INI format mirroring CLI flags). The API key is stamped in from `config.env`.
- Mailpit is now configured via `MP_*` environment variables in `config.env` (no CLI flags); the SMTP bind address, UI address, database path, and max message count are all set there.
- PHP `php.ini` is now seeded from the full upstream `php.ini-development` template on first install, with devctl overrides appended: dev-friendly resource limits, OPcache enabled with `validate_timestamps=1`/`revalidate_freq=0`, SPX profiler settings, and `auto_prepend_file`. The file is user-editable and never overwritten on restart.
- Startup migration: if Valkey, Meilisearch, or Typesense is already installed but lacks a config file (pre-existing installs), devctl writes one automatically on next startup.

### Centralised log directory + Logs page

- All managed services now write their logs to a single `~/sites/server/logs/` directory as `<service>.log` files (e.g. `caddy.log`, `dns.log`, `mysql.log`). Previously logs were scattered or missing entirely.
- Logs rotate automatically at 10 MB with 3 backup files kept (`.log.1`, `.log.2`, `.log.3`) — no dependency on system `logrotate`.
- The DNS server now has a dedicated log file (`dns.log`); previously it had none, causing a startup error.
- New **Logs** page in the dashboard (`/logs`): sidebar lists all log files with sizes; clicking a file streams it live via SSE. A **Clear** button truncates the selected file.

## 2026-03-19

### SPX Profiler — speedscope flamegraph

- Replaced the hand-rolled SVG flamegraph and Timeline tabs with an embedded [speedscope](https://github.com/jlfwong/speedscope) iframe (v1.25.0, MIT).
- SPX traces are converted server-side to speedscope's `SampledProfile` JSON format, aggregating ~1.8M raw events into unique stack paths with exclusive wall-time weights. Response is gzip-compressed and typically a few hundred KB.
- New API endpoint: `GET /api/spx/profiles/{key}/speedscope`
- New static asset route: `/speedscope/` serves the embedded speedscope assets without SPA interference.
- Speedscope features available: Time Order, Left Heavy, Sandwich views; minimap; zoom/pan; search; WebGL-accelerated rendering.
- Fixed SPX profiler list card overflowing the viewport on mobile (375px) — added `overflow-hidden` and `min-w-0` constraints; URL now truncates with ellipsis.
- Fixed `<main>` allowing horizontal scroll on mobile (`overflow-x-hidden`).

