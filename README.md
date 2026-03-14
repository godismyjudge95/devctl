# devctl

A local PHP development environment dashboard for Linux. Runs as a systemd service and serves a browser UI at `http://127.0.0.1:4000`.

devctl manages Caddy (TLS proxy), PHP-FPM processes, and optional dev services (Valkey/Redis, PostgreSQL, MySQL, Mailpit, Meilisearch, Typesense, Laravel Reverb) — all from a single dashboard without touching config files.

![Services page showing Caddy running and available services](docs/screenshot-services.png)

---

## Features

- **Services** — start, stop, restart, and one-click install dev services (Valkey, PostgreSQL, MySQL, Mailpit, Meilisearch, Typesense, Laravel Reverb) and PHP-FPM versions — all from one tab
- **Sites** — auto-discovers PHP projects in your sites directory and creates `*.test` vhosts with automatic HTTPS via Caddy's internal CA
- **Git Worktrees** — create and remove git worktrees for any site directly from the UI; each worktree gets its own `*.test` domain, Caddy vhost, and inherits the parent's PHP version
- **PHP CLI** — a global `/usr/local/bin/php` symlink always points at the highest installed PHP version; per-version symlinks (`php8.3`, `php8.4`, …) are also created
- **Global php.ini** — set `memory_limit`, `upload_max_filesize`, `post_max_size`, and `max_execution_time` across all installed PHP versions at once
- **Dumps** — receive and display `php_dd()` / `dd()` variable dumps from any site over TCP (no browser extension needed)
- **TLS** — download or auto-trust Caddy's root CA certificate so `*.test` sites work without browser warnings

---

## Requirements

- **OS**: Ubuntu 22.04+ or Debian 12+ (amd64)
- **Root access**: devctl runs as a systemd system service (root)
- A non-root user whose `~/sites` directory devctl will manage
- DNS: the `.test` TLD must resolve to your machine. The recommended approach is a router-level wildcard DNS entry pointing `*.test` at your local IP. No `/etc/hosts` entries are needed.

---

## Installation

### From a release binary (recommended)

Download the latest binary from the [Releases](https://github.com/godismyjudge95/devctl/releases) page, then run the interactive installer:

```sh
chmod +x devctl
sudo ./devctl install
```

The installer will prompt for your username and sites directory, write a systemd unit file, enable the service, and confirm it is running. Use `--yes` with `--user` and `--sites-dir` for non-interactive installs:

```sh
sudo ./devctl install --user alice --sites-dir /home/alice/sites --yes
```

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

`make install` copies the binary to `/usr/local/bin/devctl` and writes `devctl.service` to `/etc/systemd/system/` (only on first install — it will not overwrite an existing service file). Edit the service file to set `HOME` and `DEVCTL_SITE_USER` to your actual username before enabling.

---

## Uninstall

```sh
sudo devctl uninstall
```

Stops and disables the service, removes the unit file, and optionally removes the binary and `/etc/devctl/` data directory. Your sites directory is never touched.

To also remove all installed services (Caddy, Valkey, Mailpit, PHP versions, etc.) in one step, use `--purge-services`:

```sh
sudo devctl uninstall --purge-services
```

Or combine with `--yes` to skip all confirmation prompts entirely:

```sh
sudo devctl uninstall --yes --purge-services
```

---

## Screenshots

### Services

Manage dev services and PHP-FPM versions. Caddy is always running. Other services can be installed and started on demand. Expand any row to see connection info (socket path, credentials).

![Services page](docs/screenshot-services.png)

### Sites

Auto-discovered sites from your watch directory. Each site gets a `*.test` vhost with HTTPS. Assign a PHP version per site.

![Sites page](docs/screenshot-sites.png)

### Git Worktrees

Any git-backed site can have worktrees added to it. Click the fork icon on a site card, pick a branch (or create a new one), configure which paths to symlink or copy from the parent, and click **Create Worktree**. The worktree is created as a sibling directory (`~/sites/myapp-feature-x/`) and immediately gets its own Caddy vhost (`myapp-feature-x.test`).

**Domain naming:** `{parent-dir}-{branch-slug}.test`. Branch slugging: lowercase, `/` and `_` become `-`, and the `origin-` prefix is stripped from remote-tracking refs (so `origin/my-branch` → `myapp-my-branch.test`).

**Shared resources:** devctl detects the project type (Laravel, Statamic, WordPress, or generic) and pre-fills sensible defaults:

| Project type | Symlinked from parent | Copied from parent |
|---|---|---|
| Laravel / Statamic | `vendor`, `node_modules` | `.env` |
| WordPress | — | `.env`, `wp-config.php` |
| Generic | `vendor`, `node_modules` | — |

Check **Save as default for this site** to persist your symlink/copy config in the site's settings for next time.

Worktree cards on the Sites page show a dashed border, a parent-site link, and the branch name. The parent card shows an active-worktree count badge. Remove a worktree via its **Remove worktree** button — this deletes the directory, prunes the git worktree entry, and removes the Caddy vhost.

**Auto-detection:** If a linked worktree directory appears in your watch folder through other means (e.g. `git worktree add` from the terminal), devctl will auto-discover it, recognise the `.git` file pointer, and automatically link it to its parent site in the dashboard.

### Dumps

Receive and display `php_dd()` variable dumps from any PHP site in real time.

![Dumps page](docs/screenshot-dumps.png)

### Settings

Configure the dashboard host/port, sites watch directory, TLS certificate trust, and the PHP dump server TCP port.

![Settings page](docs/screenshot-settings.png)

---

## Services

| Service | Type | Default port |
|---|---|---|
| Caddy | Supervised (always on) | `:80`, `:443` |
| Valkey (Redis-compatible) | Supervised | `127.0.0.1:6379` |
| PostgreSQL | systemd | — |
| MySQL | Supervised | `127.0.0.1:3306` |
| Meilisearch | Supervised | `127.0.0.1:7700` (also `meilisearch.test`) |
| Typesense | Supervised | `127.0.0.1:8108` (also `typesense.test`) |
| Mailpit | Supervised | `127.0.0.1:8025` (web), `127.0.0.1:1025` (SMTP) |
| Laravel Reverb | Supervised | `127.0.0.1:7383` (also `reverb.test`) |
| PHP-FPM (per version) | Supervised | Unix socket `/run/php/phpX.Y-fpm.sock` |

Supervised services run as direct child processes of devctl. Valkey's service ID is `redis` for Laravel `.env` compatibility.

---

## PHP

PHP versions are installed from the [static-php-cli](https://github.com/crazywhalecc/static-php-cli) project as self-contained static binaries — no PPA or system packages required. Any version available on that index can be installed from the Services tab.

Each version runs as:
```
{siteHome}/sites/server/php/{version}/php-fpm --nodaemonize --fpm-config {siteHome}/sites/server/php/{version}/php-fpm.conf
```

**CLI symlinks created on install:**
- `/usr/local/bin/php{version}` — points at the version-specific CLI binary (e.g. `php8.4`)
- `/usr/local/bin/php` — always points at the highest installed version

---

## PHP Dumps (`php_dd`)

devctl injects `auto_prepend_file = /etc/devctl/prepend.php` into every installed PHP version's FPM ini. This makes a `php_dd()` helper available in all your sites — calling it sends a serialized variable dump to devctl's TCP listener (default port `9912`), which displays it in the **Dumps** tab.

```php
php_dd($someVariable);  // appears in the Dumps tab
```

No browser extension or Xdebug configuration required.

---

## Ports

| Port | Service | Configurable |
|---|---|---|
| `127.0.0.1:4000` | devctl dashboard | Yes (Settings → Dashboard) |
| `:80` / `:443` | Caddy | No |
| `127.0.0.1:2019` | Caddy Admin API | Yes (Settings) |
| `127.0.0.1:9912` | PHP dump receiver | Yes (Settings → PHP Dump Server) |
| `127.0.0.1:6379` | Valkey | No |
| `127.0.0.1:3306` | MySQL | No |
| `127.0.0.1:7700` | Meilisearch | No |
| `127.0.0.1:8108` | Typesense | No |
| `127.0.0.1:8025` | Mailpit (web) | Yes |
| `127.0.0.1:1025` | Mailpit (SMTP) | Yes |
| `127.0.0.1:7383` | Laravel Reverb | No |

---

## Data paths

| Path | Contents |
|---|---|
| `/etc/devctl/devctl.db` | SQLite database (sites, settings, dumps) |
| `/etc/devctl/prepend.php` | PHP auto-prepend for `php_dd()` |
| `/etc/systemd/system/devctl.service` | Systemd unit file |
| `~/sites/server/` | Service binaries and data (Caddy, Valkey, MySQL, etc.) |

---

## Build commands

```sh
make dev          # go run . (backend only, no frontend rebuild)
make dev-ui       # Vite HMR dev server (frontend only)
make build        # build frontend + go build
make install      # build + install binary + systemd unit (requires root)
make sqlc         # regenerate db/queries/*.go from SQL
make db-migrate   # apply goose migrations to /etc/devctl/devctl.db
```

---

## Tech stack

| Layer | Choice |
|---|---|
| Backend | Go 1.25, stdlib `net/http` |
| Database | SQLite (`modernc.org/sqlite`), sqlc, goose |
| Frontend | Vue 3, TypeScript, Pinia, Vite 7, Tailwind CSS v4, shadcn-vue |
| Proxy / TLS | Caddy with internal CA, wildcard `*.test` certs |

---

## License

MIT
