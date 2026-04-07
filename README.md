<p align="center">
  <img src="devctl-mark.png" width="64" alt="devctl logo">
</p>

# devctl

A local PHP development environment dashboard for Linux. Runs as a systemd service and serves a browser UI at `http://127.0.0.1:4000`.

devctl manages Caddy (TLS proxy), a built-in DNS server, PHP-FPM processes, and optional dev services (Valkey/Redis, PostgreSQL, MySQL, Mailpit, Meilisearch, Typesense, Laravel Reverb, WhoDB, MaxIO) — all from a single dashboard without touching config files.

![Services page showing Caddy running and available services](docs/screenshot-services.png)

---

## Table of Contents

- [Overview](#overview)
- [Requirements](#requirements)
- [Installation](#installation)
  - [From a release binary](#from-a-release-binary)
  - [From source](#from-source)
- [Uninstall](#uninstall)
- [Services](#services)
- [PHP](#php)
- [DNS](#dns)
- [Sites](#sites)
- [Git Worktrees](#git-worktrees)
- [PHP Dumps (php\_dd)](#php-dumps-php_dd)
- [SPX Profiler](#spx-profiler)
- [Mail](#mail)
- [Config Editor](#config-editor)
- [Logs](#logs)
- [Browser Notifications](#browser-notifications)
- [CLI](#cli)
- [Ports](#ports)
- [Data Paths](#data-paths)
- [Contributing & Development](#contributing--development)
- [License](#license)

---

## Overview

devctl is a self-contained development environment manager for PHP projects on Linux. Everything runs from a single statically-linked binary that you install once and forget about:

- **No Docker**, no VMs, no `sudo` on every command.
- **One dashboard** at `http://127.0.0.1:4000` to start/stop/install services, manage PHP versions, view logs, and inspect variable dumps.
- **Automatic HTTPS** for all `*.test` sites via Caddy's internal CA — no browser warnings once you trust the certificate once.
- **Zero config files to edit** — devctl writes sensible defaults on first install. Every config file is user-editable and never overwritten on restart.
- **AI-friendly** — a CLI lets AI agents (OpenCode, Claude, Cursor) interact with your dev environment without root: `devctl services:list`, `devctl sites:list`, `devctl logs:tail caddy`, etc.

All service binaries are downloaded directly from their upstream releases and stored under your sites directory (default `~/sites/server/`). Nothing is installed system-wide except the devctl binary and systemd unit.

---

## Requirements

- **OS**: Ubuntu 22.04+ or Debian 12+ (amd64)
- **Root access**: devctl runs as a systemd system service (root)
- A non-root user whose `~/sites` directory devctl will manage
- **DNS**: the `.test` TLD must resolve to your machine. The easiest approach is to use devctl's built-in DNS server with its `systemd-resolved` integration (see [DNS](#dns)). Alternatively, configure a wildcard `*.test` entry in your router's DNS.

---

## Installation

### From a release binary

Download the latest binary from the [Releases](https://github.com/godismyjudge95/devctl/releases) page, then run the interactive installer:

```sh
chmod +x devctl
sudo ./devctl install
```

The installer prompts you to confirm:

1. Which user's sites directory devctl should manage (auto-detected from `SUDO_USER`)
2. Where your sites are stored (default: `~/sites`)
3. Where to install the devctl binary (default: `~/sites/server/devctl/devctl`)

It then writes the systemd unit file, enables the service, and confirms it is running.

For non-interactive (scripted) installs, pass all flags explicitly:

```sh
sudo ./devctl install --user alice --sites-dir /home/alice/sites --yes
```

| Flag | Description |
|---|---|
| `--user` | Non-root user whose sites dir devctl will manage. Auto-detected from `SUDO_USER` if omitted. |
| `--sites-dir` | Directory where sites are stored. Default: `~/sites`. |
| `--path` | Directory to install the devctl binary into. Default: `{sites-dir}/server/devctl`. |
| `--yes` | Skip all confirmation prompts. Requires `--user` in non-interactive mode. |

The dashboard will be available at **http://127.0.0.1:4000** once the service starts.

### From source

Requirements: Go 1.25+, Node.js 18+, npm.

```sh
git clone https://github.com/godismyjudge95/devctl
cd devctl
make build
sudo make install
sudo systemctl enable --now devctl
```

`make install` copies the binary to `~/sites/server/devctl/devctl` and writes `devctl.service` to `/etc/systemd/system/`. Edit the service file to set `HOME`, `DEVCTL_SITE_USER`, and `DEVCTL_SERVER_ROOT` to your actual values before enabling.

---

## Auto-update

devctl checks GitHub for a newer release on startup and again daily at 3 am.

When a newer version is available an amber **↑** button appears next to the **devctl** logo in the sidebar. Hovering over it shows a tooltip with the target version (e.g. *Update devctl to v0.4.0*). Clicking the button opens a progress dialog that streams the download and install steps live, then automatically restarts the service.

**How it works:**

1. The current binary is downloaded from the GitHub release page and verified by running `--version`.
2. The running binary is backed up as `devctl.bak` next to the installed binary.
3. The new binary replaces the current one atomically.
4. `systemctl restart devctl` is triggered — the service comes back up on the new version within seconds.
5. On the next clean startup, the `devctl.bak` backup is removed automatically.

If the download or verification step fails the current binary is not replaced and an error is shown in the dialog.

---

## Uninstall

```sh
sudo devctl uninstall
```

Stops and disables the service, removes the systemd unit file, and optionally removes the binary and devctl data directory (`{serverRoot}/devctl/`). Your sites directory is never touched.

To also remove all installed service binaries in one step, use `--purge-services`:

```sh
sudo devctl uninstall --purge-services
```

Or combine with `--yes` to skip all confirmation prompts:

```sh
sudo devctl uninstall --yes --purge-services
```

---

## Services

Manage all services from the **Services** tab of the dashboard. Caddy and the DNS server are always-on. All other services are optional and can be installed on demand with one click.

Each installed service can be started, stopped, and restarted from the dashboard. Expand any service row to view connection credentials and the config file path.

### Update checker

devctl checks for newer versions on startup and again daily at 3 am. When an update is available, an amber **"update"** badge appears next to the version string and an **Update** button is shown. Hovering shows the exact from/to version. Updates stream their output live via SSE.

| Service | Port(s) | `.test` vhost | Download source | Config file |
|---|---|---|---|---|
| Caddy | `:80`, `:443`, `127.0.0.1:2019` (admin) | — | [github.com/caddyserver/caddy](https://github.com/caddyserver/caddy/releases) | `{serverRoot}/caddy/Caddyfile` (auto-managed) |
| DNS Server | `127.0.0.1:5354` (UDP+TCP) | — | Embedded goroutine (no download) | — |
| Valkey (Redis-compatible) | `127.0.0.1:6379` | — | [download.valkey.io](https://download.valkey.io/releases/) | `{serverRoot}/valkey/valkey.conf` |
| PostgreSQL | `127.0.0.1:5432` | — | [downloads.percona.com](https://downloads.percona.com/downloads/postgresql-distribution-18/) (Percona Distribution) | `{serverRoot}/postgres/data/postgresql.conf` |
| MySQL | `127.0.0.1:3306` | — | [repo.mysql.com/apt](https://repo.mysql.com/apt/) (Ubuntu `.deb` packages, extracted in-place) | `{serverRoot}/mysql/my.cnf` |
| Meilisearch | `127.0.0.1:7700` | `meilisearch.test` | [github.com/meilisearch/meilisearch](https://github.com/meilisearch/meilisearch/releases) | `{serverRoot}/meilisearch/config.toml` |
| Typesense | `127.0.0.1:8108` | `typesense.test` | [dl.typesense.org](https://dl.typesense.org/releases/) | `{serverRoot}/typesense/typesense.ini` |
| Mailpit | `127.0.0.1:8025` (web), `127.0.0.1:1025` (SMTP) | — | [github.com/axllent/mailpit](https://github.com/axllent/mailpit/releases) | `{serverRoot}/mailpit/config.env` (env vars) |
| Laravel Reverb | `127.0.0.1:7383` | `reverb.test` | [packagist.org/laravel/reverb](https://packagist.org/packages/laravel/reverb) (via Composer) | `{serverRoot}/reverb/.env` |
| WhoDB | `127.0.0.1:8161` | `whodb.test` | [github.com/clidey/whodb](https://github.com/clidey/whodb/releases) | `{serverRoot}/whodb/config.env` |
| MaxIO | `127.0.0.1:9000` (S3 API), `127.0.0.1:9001` (console) | `maxio.test` | [github.com/coollabsio/maxio](https://github.com/coollabsio/maxio/releases) (always latest) | `{serverRoot}/maxio/config.env` |
| PHP-FPM (per version) | Unix socket | — | [static-php-cli](https://github.com/crazywhalecc/static-php-cli) | `{serverRoot}/php/{version}/php.ini` |

**Notes:**

- Supervised services (Valkey, MySQL, Meilisearch, Typesense, Mailpit, Reverb, WhoDB, MaxIO, PHP-FPM) run as direct child processes of devctl with automatic restart on crash.
- PostgreSQL runs as a supervised child process but drops privileges to `DEVCTL_SITE_USER` (PostgreSQL refuses to start as root).
- Valkey's service ID is `redis` for Laravel `.env` compatibility (`REDIS_HOST`, `REDIS_PORT`, etc.).
- Config files are written once on install and never overwritten on restart. User edits are preserved.
- Mailpit is configured via `MP_*` environment variables in `config.env` rather than a native config file.
- Meilisearch updates are handled autonomously: devctl dumps the index data, replaces the binary, then re-imports the dump automatically.

### WhoDB

![WhoDB database explorer](docs/screenshot-whodb.png)

[WhoDB](https://github.com/clidey/whodb) is a lightweight database explorer with a web UI, embedded in the devctl sidebar. Install it from the Services tab. devctl automatically configures pre-populated connection profiles for any installed database service (MySQL, PostgreSQL, Valkey/Redis).

A **WhoDB** section in Settings lets you:
- Toggle the credential entry form (`WHODB_DISABLE_CREDENTIAL_FORM`)
- View auto-detected connections (read-only)
- Add, edit, and delete manual connections

Connections are stored in the devctl SQLite database and applied immediately.

### MaxIO

![MaxIO file browser showing bucket contents](docs/screenshot-maxio.png)

[MaxIO](https://github.com/coollabsio/maxio) is a high-performance S3-compatible object storage server (single binary from coollabsio/maxio). Install it from the Services tab. Default credentials are `devctl` / `devctlsecret` — edit `{serverRoot}/maxio/config.env` to change them. Data is stored at `{serverRoot}/maxio/data`. The `maxio.test` vhost proxies to the console UI at port `9001`.

For Laravel, copy the generated `connection.env` values into your `.env`:

```env
AWS_ENDPOINT=https://s3.maxio.test
```

---

## PHP

PHP versions are installed from the [static-php-cli](https://github.com/crazywhalecc/static-php-cli) project as self-contained static binaries. No PPA, no system PHP packages required.

Install any available version from the Services tab. Each version runs as:

```
{serverRoot}/php/{version}/php-fpm -c {serverRoot}/php/{version}/php.ini \
  --nodaemonize --fpm-config {serverRoot}/php/{version}/php-fpm.conf
```

### php.ini defaults

On first install, devctl creates a `php.ini` based on the full upstream `php.ini-development` template with the following overrides appended:

| Setting | Default |
|---|---|
| `memory_limit` | `256M` |
| `upload_max_filesize` | `128M` |
| `post_max_size` | `128M` |
| `max_execution_time` | `120` |
| OPcache | Enabled with `validate_timestamps=1`, `revalidate_freq=0` (dev-safe) |
| SPX profiler | Pre-configured (zero overhead when not active) |
| `auto_prepend_file` | Points to `{serverRoot}/devctl/prepend.php` (for `php_dd()`) |

The `php.ini` is user-editable and never overwritten on restart. To regenerate it with updated defaults, delete the file and restart devctl.

The **Global PHP Settings** panel in the dashboard patches `memory_limit`, `upload_max_filesize`, `post_max_size`, and `max_execution_time` across **all installed PHP versions** at once.

### Config editor

Click the file icon on any PHP-FPM row in the Services tab to open the full-screen config editor. PHP-FPM shows two tabs — `php.ini` and `php-fpm.conf` — switchable without leaving the editor.

### CLI symlinks

On each PHP version install, devctl creates:

- `{serverRoot}/bin/php{version}` — version-specific CLI symlink (e.g. `php8.4`)
- `{serverRoot}/bin/php` — always points to the highest installed version

### Laravel and Statamic CLIs

When PHP is installed, devctl also runs `composer global require laravel/installer` and `composer global require statamic/cli` as the site user. The binaries land in the Composer global bin directory (`{siteHome}/.config/composer/vendor/bin/` by default).

devctl adds both `{serverRoot}/bin` and the Composer global bin directory to the site user's interactive shell PATH by appending an `export PATH=...` block to `.bashrc`, `.zshenv`, and `.bash_profile` (whichever exist). This means `laravel new`, `statamic new`, and other globally-installed Composer tools are available immediately in a new terminal.

devctl also prepends the Composer global bin directory to PATH for every command it runs internally as the site user, so framework tools are accessible in the context of site commands regardless of the shell configuration.

**Dev tools in `{serverRoot}/bin/`:** During install and after a self-update, devctl automatically downloads the latest versions of useful CLI tools into the shared bin directory so they are immediately available in your terminal:

| Tool | Description |
|---|---|
| `sqlite3` | Official SQLite CLI for inspecting `.db` files |
| `fnm` / `nvm` | Fast Node Manager (fnm) with an `nvm` alias — manage Node.js versions |

---

## DNS

devctl includes a built-in DNS server that runs as an in-process goroutine (no separate binary, no daemon). It intercepts queries for configurable TLDs and returns a fixed A record, forwarding all other queries upstream.

**Default behaviour:**

- Listens on `127.0.0.1:5354` (UDP and TCP)
- Intercepts `*.test` queries and returns your primary LAN IP
- Forwards all other queries to the system upstream resolver (read from `/run/systemd/resolve/resolv.conf`, falling back to `/etc/resolv.conf`, then `8.8.8.8`)

### Configuring via the dashboard

Open the gear icon on the DNS Server row in the Services tab:

| Setting | Description |
|---|---|
| Port | UDP/TCP port the server listens on (default `5354`) |
| TLD(s) | Comma-separated list of TLDs to intercept (default `.test`) |
| Target IP | IP address returned for intercepted queries. Click **Auto-detect** to use your primary LAN IP. |
| System DNS | One-click integration with `systemd-resolved` to route `.test` queries system-wide |

### systemd-resolved integration

Click **Configure** to write `/etc/systemd/resolved.conf.d/99-devctl-dns.conf` and restart `systemd-resolved`. This routes all `.test` queries on the machine to devctl's DNS server — no router config or `/etc/hosts` entries needed.

The generated drop-in:

```ini
[Resolve]
DNS=127.0.0.1:5354
Domains=~test
```

Click **Remove** to delete the drop-in and restore the previous resolver behaviour.

---

## Sites

devctl auto-discovers PHP projects in your configured sites watch directory (default: `~/sites`) and creates `*.test` vhosts with automatic HTTPS via Caddy's internal CA. Newly discovered sites are automatically assigned the latest installed PHP version.

When a site directory is removed from disk, devctl automatically deregisters it — both at startup (stale entries are pruned on boot) and at runtime (the filesystem watcher detects deletions and removes the site immediately).

![Sites page](docs/screenshot-sites.png)

**Per-site controls:**

- Assign a PHP version (each site can run a different version)
- Enable or disable the SPX profiler
- Toggle HTTPS
- Set a custom public directory

**TLS:** Caddy's internal CA generates certificates automatically. To eliminate browser warnings, click **Trust Certificate** in Settings to install the CA into your system and browser trust stores (requires `libnss3-tools`).

**Framework detection:** devctl inspects `composer.json` and common project files to detect Laravel, Statamic, WordPress, and generic PHP projects.

---

## Git Worktrees

Any git-backed site can have worktrees added directly from the dashboard. Click the fork icon on a site card, pick or create a branch, configure which paths to symlink or copy from the parent, and click **Create Worktree**.

The worktree is created as a sibling directory (`~/sites/myapp-feature-x/`) and immediately gets its own Caddy vhost (`myapp-feature-x.test`).

**Domain naming:** `{parent-dir}-{branch-slug}.test`. Branch slugging: lowercase, `/` and `_` become `-`, and the `origin-` prefix is stripped (so `origin/my-branch` → `myapp-my-branch.test`).

**Shared resources:** devctl pre-fills sensible defaults based on the detected project type:

| Project type | Symlinked from parent | Copied from parent |
|---|---|---|
| Laravel / Statamic | `vendor`, `node_modules` | `.env` |
| WordPress | — | `.env`, `wp-config.php` |
| Generic | `vendor`, `node_modules` | — |

Check **Save as default for this site** to persist your symlink/copy config in the site's settings for next time.

**Worktree cards** on the Sites page show a dashed border, a parent-site link, and the branch name. The parent card shows an active-worktree count badge.

**Remove a worktree** via its **Remove worktree** button — this deletes the directory, prunes the git worktree entry, and removes the Caddy vhost.

**Auto-detection:** Worktree directories that appear in your watch folder via `git worktree add` in the terminal are auto-discovered, recognised by their `.git` file pointer, and automatically linked to their parent site.

---

## PHP Dumps (`php_dd`)

devctl injects `auto_prepend_file = {serverRoot}/devctl/prepend.php` into every installed PHP version's FPM ini. This makes a `php_dd()` helper available in all your sites without any configuration.

Calling `php_dd()` sends a serialised variable dump over TCP to devctl's dump receiver (default port `9912`), which displays it in the **Dumps** tab in real time.

```php
php_dd($someVariable);         // displays in the Dumps tab
php_dd($a, $b, $c);            // multiple args shown together
dd($someVariable);             // alias — works the same way
```

No browser extension, no Xdebug, no configuration required.

![Dumps page](docs/screenshot-dumps.png)

---

## SPX Profiler

devctl includes native support for [SPX](https://github.com/NoiseByNorthwest/php-spx), a low-overhead PHP profiler. SPX is compiled directly into devctl's custom PHP binaries (available for PHP 8.1–8.4, x86_64). It has zero overhead when not actively profiling.

### Enabling SPX for a site

1. Open a site's settings dialog (gear icon on the site card).
2. Toggle **SPX Profiling** on and save. devctl rewrites the PHP-FPM ini and restarts the pool.

Once enabled, the **Profiler** navigation item appears in the sidebar.

### Triggering a profile

Activate profiling for a specific request by sending the `SPX_ENABLED=1` and `SPX_KEY=dev` cookies, or as query parameters:

```
https://mysite.test/some/page?SPX_ENABLED=1&SPX_KEY=dev
```

The profile is saved to `{serverRoot}/php/{version}/spx-data/` and appears in the Profiler tab automatically.

### Profiler views

| Tab | Description |
|---|---|
| Flat Profile | Sorted table of all called functions with exclusive/inclusive time and call count |
| Flamegraph | SVG call stack visualization — hover for details, colour by call depth |
| Timeline | Chronological call timeline with function name labels |
| Metadata | Request URL, duration, peak memory, and other profiler metadata |

Profiles can be cleared from the UI at any time.

![SPX Profiler showing profile list and flat profile detail](docs/screenshot-spx.png)

---

## Mail

Mailpit provides a local SMTP server and web UI for catching all outbound email from your PHP sites.

- **SMTP**: `127.0.0.1:1025` — configure your app's `MAIL_HOST` / `MAIL_PORT` to this address
- **Web UI**: `http://127.0.0.1:8025` — or click the **Mail** link in the devctl sidebar
- **Storage**: emails are stored at `{serverRoot}/mailpit/data/`

For Laravel, set in your `.env`:

```env
MAIL_MAILER=smtp
MAIL_HOST=127.0.0.1
MAIL_PORT=1025
```

Mailpit is configured via `MP_*` environment variables in `{serverRoot}/mailpit/config.env`. Click the file icon on the Mailpit row in the Services tab to edit this file directly.

![Mail page showing inbox and message detail](docs/screenshot-mail.png)

---

## Config Editor

Every config-enabled service has a file icon in the Services tab that opens a full-screen config editor powered by CodeMirror 6.

**Features:**

- Syntax highlighting and line numbers
- Ctrl+F in-editor search
- **Save & Restart** button — writes the file and restarts the service in one click
- Ctrl+S keyboard shortcut for Save & Restart

**Config-enabled services:**

| Service | Config file(s) |
|---|---|
| Valkey | `valkey.conf` |
| MySQL | `my.cnf` |
| Meilisearch | `config.toml` |
| Typesense | `typesense.ini` |
| Mailpit | `config.env` |
| PHP-FPM | `php.ini`, `php-fpm.conf` (two tabs) |

---

## Logs

The **Logs** tab streams the tail of any service log file directly in the browser. Select a log from the left-hand list to open it in the viewer pane.

- All service logs are stored under `{serverRoot}/logs/` and listed automatically.
- The viewer streams new lines in real time via SSE — no manual refresh needed.
- **Clear** truncates the selected log file to zero bytes.
- On mobile, the list and viewer are separate panes with a back button.

![Logs page showing log file list and viewer](docs/screenshot-logs.png)

---

## Browser Notifications

devctl fires native desktop notifications when events occur while you are on another tab.

| Event | Behaviour |
|---|---|
| New `php_dd()` dump | Fires when you are not on the Dumps page. Multiple dumps within 1.5 s are collapsed into one count notification. Clicking navigates to that dump. |
| New email | Fires when you are not on the Mail page. Bursts are collapsed. Clicking navigates to the Mail page. |
| Service updates available | Fires when the update checker (runs on startup and daily at 3 am) finds a newer version for any installed service. |

Notification permission is requested automatically on first load. devctl uses the **Service Worker Notification API** when supported, with a direct `Notification` API fallback for browsers without service worker support.

---

## CLI

The devctl binary doubles as a CLI that talks to the running daemon at `127.0.0.1:4000` (or `$DEVCTL_ADDR`). All commands work without root.

```sh
devctl services:list              # list all services and status
devctl services:install mailpit   # install an available service
devctl services:restart caddy     # restart a service
devctl sites:list                 # list all sites
devctl sites:php myapp.test 8.4   # switch PHP version for a site
devctl logs:tail caddy --follow   # stream the tail of a log live
devctl mail:list                  # list captured emails
devctl settings:get               # show all settings
devctl settings:set devctl_port=4001  # change a setting (key=value)
devctl php:settings               # show PHP ini settings
devctl php:set memory_limit=512M  # update a PHP ini setting
devctl dns:status                 # check systemd-resolved DNS setup
devctl dumps:list                 # list recent php_dd() dumps
devctl spx:profiles               # list recent SPX profiler captures
devctl tls:trust                  # trust Caddy's internal CA
devctl devctl:update              # check for devctl updates and apply
devctl devctl:skill               # generate an OpenCode CLI skill file
```

### Global flags

| Flag | Description |
|---|---|
| `--json` | Output raw JSON instead of formatted text (works on every command) |
| `--addr=host:port` | Override the daemon address (default: `127.0.0.1:4000`, or `$DEVCTL_ADDR`) |
| `--help` | Show help for a specific command |

### Available commands

| Namespace | Command | Description |
|---|---|---|
| `services` | `services:list` | List all managed services and their status |
| | `services:available` | List services that can be installed |
| | `services:install <id>` | Install an available service |
| | `services:start <id>` | Start a stopped service |
| | `services:stop <id>` | Stop a running service |
| | `services:restart <id>` | Restart a service |
| | `services:update <id>` | Update an installed service to the latest version |
| | `services:credentials <id>` | Show connection credentials for a service |
| `sites` | `sites:list` | List all managed sites |
| | `sites:get <domain>` | Show full details for a site |
| | `sites:php <domain> <version>` | Switch the PHP version for a site |
| | `sites:spx <domain> enable\|disable` | Enable or disable the SPX profiler for a site |
| `php` | `php:versions` | List installed PHP versions and their FPM status |
| | `php:settings` | Show current PHP ini settings (applies to all versions) |
| | `php:set <key=value>...` | Update PHP ini settings |
| `logs` | `logs:list` | List available log files |
| | `logs:tail <id> [--bytes=N] [--follow]` | Show the tail of a log file; `--follow` streams live |
| | `logs:clear <id>` | Clear (truncate) a log file |
| `dumps` | `dumps:list [--domain=]` | List recent php_dd() variable dumps |
| | `dumps:clear` | Delete all php_dd() dumps |
| `spx` | `spx:profiles [--domain=]` | List recent SPX profiler captures |
| | `spx:profile <key>` | Show CPU hotspot functions for an SPX profile |
| `mail` | `mail:list [--limit=N]` | List recent emails captured by Mailpit |
| | `mail:get <id>` | Show the full content of an email |
| | `mail:delete <id>[,<id>...]` | Delete one or more emails by ID |
| | `mail:clear` | Delete all emails from Mailpit |
| `dns` | `dns:status` | Check whether systemd-resolved is configured for `*.test` |
| | `dns:setup` | Configure systemd-resolved to route `*.test` queries to devctl |
| | `dns:teardown` | Remove the systemd-resolved `*.test` DNS configuration |
| `tls` | `tls:trust` | Trust Caddy's internal CA in the system and browser certificate stores |
| `settings` | `settings:get` | Show all devctl settings |
| | `settings:set <key=value>...` | Update devctl settings |
| `devctl` | `devctl:update` | Check for a newer devctl release and update if one is available |
| | `devctl:skill` | Generate an OpenCode agent skill describing all CLI commands |

### OpenCode integration

Run `devctl devctl:skill` once to write an OpenCode skill file to `~/.agents/skills/devctl-cli/SKILL.md`. The daemon silently regenerates the file on every startup if it already exists.

---

## Ports

All ports bind to `127.0.0.1` by default (loopback only). Ports marked configurable can be changed from the Settings tab.

| Port | Service | Configurable |
|---|---|---|
| `127.0.0.1:4000` | devctl dashboard | Yes — Settings → Dashboard |
| `:80` / `:443` | Caddy | No |
| `127.0.0.1:2019` | Caddy Admin API | Yes — Settings |
| `127.0.0.1:5354` | DNS server (UDP+TCP) | Yes — Services → DNS → Settings |
| `127.0.0.1:9912` | PHP dump receiver (TCP) | Yes — Settings → PHP Dump Server |
| `127.0.0.1:6379` | Valkey | No |
| `127.0.0.1:5432` | PostgreSQL | No |
| `127.0.0.1:3306` | MySQL | No |
| `127.0.0.1:7700` | Meilisearch | No |
| `127.0.0.1:8108` | Typesense | No |
| `127.0.0.1:8025` | Mailpit web UI | Yes |
| `127.0.0.1:1025` | Mailpit SMTP | Yes |
| `127.0.0.1:7383` | Laravel Reverb | No |
| `127.0.0.1:8161` | WhoDB | No |
| `127.0.0.1:9000` | MaxIO S3 API | No |
| `127.0.0.1:9001` | MaxIO console | No |

---

## Data Paths

All devctl runtime data lives under `{serverRoot}`, which defaults to `{sitesDir}/server` (e.g. `~/sites/server`). The sites directory is chosen during `devctl install` and stored in the SQLite database.

`{serverRoot}` is set in the systemd unit as `DEVCTL_SERVER_ROOT` and is the single source of truth — it is never hardcoded.

| Path | Contents |
|---|---|
| `{serverRoot}/devctl/devctl.db` | SQLite database (sites, settings, dumps) |
| `{serverRoot}/devctl/prepend.php` | PHP auto-prepend for `php_dd()` |
| `{serverRoot}/devctl/devctl` | devctl binary (default install location) |
| `{serverRoot}/bin/` | Shared bin dir on `PATH` — devctl symlinks and auto-downloaded tools (sqlite3, …) |
| `{serverRoot}/logs/` | Service log files (`caddy.log`, `dns.log`, `mysql.log`, …) |
| `{serverRoot}/caddy/` | Caddy binary, env file, internal CA data |
| `{serverRoot}/valkey/` | Valkey binary, `valkey.conf`, data |
| `{serverRoot}/postgres/` | PostgreSQL binary tarball, `data/` directory |
| `{serverRoot}/mysql/` | MySQL binaries (extracted from `.deb`), `data/` directory |
| `{serverRoot}/meilisearch/` | Meilisearch binary, `config.toml`, index data |
| `{serverRoot}/typesense/` | Typesense binary, `typesense.ini`, data |
| `{serverRoot}/mailpit/` | Mailpit binary, `config.env`, email storage |
| `{serverRoot}/reverb/` | Laravel app that runs `php artisan reverb:start` |
| `{serverRoot}/whodb/` | WhoDB binary, `config.env` |
| `{serverRoot}/maxio/` | MaxIO binary, `config.env`, object data |
| `{serverRoot}/php/{version}/` | PHP static binary, `php.ini`, `php-fpm.conf`, SPX data |
| `/etc/systemd/system/devctl.service` | Systemd unit file |
| `/etc/profile.d/devctl.sh` | Adds `{serverRoot}/bin` to `PATH` for all users |

---

## Contributing & Development

### Tech stack

| Layer | Choice |
|---|---|
| Backend | Go 1.25, stdlib `net/http` (no third-party router) |
| Database | SQLite (`modernc.org/sqlite`), sqlc (codegen), goose (migrations) |
| Frontend | Vue 3, TypeScript, Pinia, Vite 7, Tailwind CSS v4, shadcn-vue |
| Proxy / TLS | Caddy with internal CA, wildcard `*.test` certs |
| Service unit | systemd system service |

### Build commands

```sh
make dev          # go run . (backend only, no frontend rebuild)
make dev-ui       # Vite HMR dev server (frontend only)
make build        # build-ui + go build
make install      # build + install binary + systemd unit (requires root)
make sqlc         # regenerate db/queries/*.go from db/queries/*.sql
make db-migrate   # apply goose migrations to {serverRoot}/devctl/devctl.db
```

### Package layout

| Package | Responsibility |
|---|---|
| `main.go` | Entry point, subsystem wiring, graceful shutdown, update checker |
| `api/` | HTTP handlers, route registration (`api/server.go`), SSE, WebSocket |
| `services/` | Static service registry, exec manager, status poller, process supervisor |
| `sites/` | Site CRUD (SQLite), Caddy Admin API client, fsnotify watcher |
| `php/` | PHP-FPM version detection, install/uninstall, php.ini read/write |
| `dumps/` | TCP dump receiver, WebSocket broadcast hub, SQLite store |
| `install/` | Idempotent service installers — one file per service |
| `config/` | `config.Load()`, static default service definitions (`defaults.go`) |
| `paths/` | Single source of truth for all filesystem paths |
| `dnsserver/` | Built-in DNS server goroutine |
| `cli/` | CLI command registry, HTTP client, lipgloss output, per-namespace command files |
| `selfinstall/` | `devctl install` / `devctl uninstall` sub-commands |
| `db/` | SQLite open, goose migrations (`db/migrations/`), sqlc queries (`db/queries/`) |
| `frontend/` | Vue 3 SPA — `src/lib/api.ts` (all fetch wrappers), `src/stores/` (Pinia) |

### Key conventions

- **No third-party HTTP router** — use Go 1.22+ `METHOD /path/{param}` syntax in `net/http`.
- **No CGO** — `modernc.org/sqlite` is pure Go.
- **Frontend is embedded** in the binary via `//go:embed ui/dist`; run `make build-ui` before `go build` for a working UI.
- **All REST calls** in the frontend go through `frontend/src/lib/api.ts` — never call `fetch()` directly in components or stores.
- **Never hardcode paths** — always use the `paths` package or `DEVCTL_SERVER_ROOT`.
- The binary **requires root** — enforced at startup, logged to the systemd journal.

### Screenshots

The dashboard is fully responsive. On narrow viewports the sidebar collapses into a slide-in drawer and service lists switch to a card layout.

<p align="center">
  <img src="docs/screenshot-mobile-services.png" width="200" alt="Services page on mobile">
  <img src="docs/screenshot-mobile-sites.png" width="200" alt="Sites page on mobile">
  <img src="docs/screenshot-mobile-mail.png" width="200" alt="Mail page on mobile">
  <img src="docs/screenshot-mobile-dumps.png" width="200" alt="Dumps page on mobile">
  <img src="docs/screenshot-mobile-spx.png" width="200" alt="SPX Profiler on mobile">
  <img src="docs/screenshot-mobile-logs.png" width="200" alt="Logs page on mobile">
  <img src="docs/screenshot-mobile-settings.png" width="200" alt="Settings page on mobile">
  <img src="docs/screenshot-mobile-maxio.png" width="200" alt="MaxIO on mobile">
  <img src="docs/screenshot-mobile-whodb.png" width="200" alt="WhoDB on mobile">
</p>

Additional screenshots:

![Settings page](docs/screenshot-settings.png)

---

## License

MIT
