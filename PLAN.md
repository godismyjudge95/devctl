# devctl — Local Dev Dashboard

A single Go binary running as a systemd user service that serves a Vue 3 / shadcn-vue web dashboard for managing a local PHP development environment. Replaces Laravel Herd on Linux.

## Core Decisions

| Decision | Choice |
|---|---|
| App model | Go binary as systemd service, browser-based dashboard |
| Web server | Caddy + PHP-FPM (multiple versions via `ondrej/php` PPA) |
| Default bind | `127.0.0.1:4000`, configurable to `0.0.0.0` in Settings |
| TLS | Caddy internal CA, `tls internal`, wildcard `*.test` |
| Mail | Mailpit managed as a service, iframe embed |
| Dumps | Custom JSON node tree renderer in Vue, per-site scoped via `HTTP_HOST` |
| SPX | Toggle per site, link to `/_spx` |
| Sites discovery | fsnotify watch of `~/sites`, direct children only, immediately provision |
| Built-in services | Written to `~/.config/devctl/services.yaml` on first run |
| PHP-FPM services | Auto-generated when a PHP version is installed |
| SQLite | `modernc.org/sqlite` + `sqlc` + `pressly/goose` |
| Frontend | Vue 3 + TypeScript + Vite + Tailwind CSS + shadcn-vue |

## Tech Stack

| Layer | Choice | Reason |
|---|---|---|
| Backend language | Go (stdlib-heavy) | Single binary, fast, existing php_dd uses it |
| SQLite driver | `modernc.org/sqlite` | Pure Go, no CGO, cross-compile friendly |
| SQL queries | `sqlc` (codegen) | Type-safe, SQL as source of truth |
| DB migrations | `pressly/goose` v3 | Embedded SQL files, works with `embed.FS` |
| Frontend framework | Vue 3 + TypeScript | Modern, reactive, Vite |
| UI components | shadcn-vue (Radix Vue + Tailwind) | Open-code, composable, dark mode |
| Build tool | Vite | Standard for Vue 3 |
| Config format | YAML | Compatible with existing `services.yaml` schema |
| Local CA / TLS | Caddy's built-in internal CA | Automatic cert renewal, no mkcert dependency |
| Process watching | `github.com/fsnotify/fsnotify` | Sites directory watcher |

## Directory Structure

```
/devctl/
├── main.go
├── go.mod
├── go.sum
│
├── config/
│   ├── config.go           # Load/save ~/.config/devctl/config.yaml
│   └── defaults.go         # Built-in service definitions (written to disk on first run)
│
├── api/
│   ├── server.go           # net/http router, middleware, embed serving
│   ├── services.go         # /api/services + start/stop/restart
│   ├── sites.go            # /api/sites CRUD
│   ├── php.go              # /api/php/versions + settings
│   ├── dumps.go            # /api/dumps REST + /ws/dumps WebSocket
│   ├── mail.go             # /api/mail/config
│   ├── tls.go              # /api/tls/cert (download) + /api/tls/trust
│   ├── logs.go             # /api/services/{id}/logs SSE
│   └── settings.go         # /api/settings GET/PUT
│
├── services/
│   ├── definition.go       # ServiceDefinition struct
│   ├── registry.go         # Load + merge YAML definitions
│   ├── manager.go          # os/exec start/stop/restart/status/version
│   └── poller.go           # Background goroutine, SSE broadcast
│
├── sites/
│   ├── manager.go          # Site CRUD (SQLite) + Caddy sync
│   ├── caddy.go            # Caddy Admin API client
│   └── watcher.go          # fsnotify watcher for sites directory
│
├── php/
│   ├── versions.go         # Detect installed PHP-FPM (scan /etc/php/*)
│   ├── installer.go        # apt install/remove php{X.Y}-fpm + extensions
│   ├── config.go           # Read/write php.ini per version
│   └── extensions.go       # Enable/disable extensions per version
│
├── dumps/
│   ├── server.go           # Wire-up TCP listener + WS hub
│   ├── store.go            # SQLite-backed store (insert, list, clear, prune)
│   ├── tcp.go              # TCP :9912, PHP-serialize+base64 decoder
│   └── hub.go              # WebSocket broadcast hub
│
├── db/
│   ├── db.go               # Open SQLite, apply pragmas, run migrations
│   ├── migrations/
│   │   ├── 001_sites.sql
│   │   ├── 002_dumps.sql
│   │   └── 003_settings.sql
│   └── queries/            # sqlc input .sql files + generated output
│       ├── schema.sql
│       ├── sites.sql
│       ├── dumps.sql
│       ├── settings.sql
│       └── *.go            # sqlc-generated
│
├── php_dd_ext/             # Moved from php_dd/ — C extension + prepend.php
│   ├── ext/                # C extension source (unchanged)
│   ├── prepend/            # prepend.php (modified for JSON node output)
│   ├── bin/                # Compiled .so + server binary
│   └── Makefile
│
├── ui/                     # go:embed target
│   └── dist/               # Built by Vite (git-ignored)
│
├── frontend/               # Vue 3 source
│   ├── src/
│   │   ├── main.ts
│   │   ├── App.vue
│   │   ├── router/index.ts
│   │   ├── stores/
│   │   │   ├── services.ts
│   │   │   ├── sites.ts
│   │   │   ├── php.ts
│   │   │   ├── dumps.ts
│   │   │   └── settings.ts
│   │   ├── lib/
│   │   │   ├── api.ts      # Typed fetch wrappers for all REST endpoints
│   │   │   └── ws.ts       # WebSocket connection manager
│   │   ├── components/
│   │   │   ├── ui/         # shadcn-vue components
│   │   │   ├── DumpNode.vue       # Recursive dump tree renderer
│   │   │   ├── DumpCard.vue       # Outer card (meta bar + DumpNode)
│   │   │   ├── ServiceRow.vue     # Single service table row
│   │   │   ├── SiteCard.vue       # Single site card
│   │   │   └── LogStream.vue      # SSE log tail panel
│   │   └── views/
│   │       ├── ServicesView.vue
│   │       ├── SitesView.vue
│   │       ├── PhpView.vue
│   │       ├── DumpsView.vue
│   │       ├── MailView.vue
│   │       └── SettingsView.vue
│   ├── package.json
│   ├── vite.config.ts
│   ├── tailwind.config.ts
│   └── tsconfig.json
│
├── Makefile
├── devctl.service          # systemd unit file template
└── PLAN.md                 # This file
```

## SQLite Schema

```sql
-- 001_sites.sql
CREATE TABLE sites (
    id               TEXT PRIMARY KEY,       -- slug e.g. "myapp-test"
    domain           TEXT NOT NULL UNIQUE,   -- e.g. "myapp.test"
    root_path        TEXT NOT NULL,
    php_version      TEXT NOT NULL DEFAULT '8.3',
    aliases          TEXT NOT NULL DEFAULT '[]',  -- JSON array of strings
    spx_enabled      INTEGER NOT NULL DEFAULT 0,
    https            INTEGER NOT NULL DEFAULT 1,
    auto_discovered  INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- 002_dumps.sql
CREATE TABLE dumps (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file        TEXT,
    line        INTEGER,
    nodes       TEXT NOT NULL,   -- JSON node tree (see wire format below)
    timestamp   REAL NOT NULL,   -- Unix float from PHP
    site_domain TEXT             -- HTTP_HOST from PHP request
);

-- 003_settings.sql
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

### Default Settings Keys

| Key | Default | Description |
|---|---|---|
| `devctl_port` | `4000` | Dashboard HTTP port |
| `devctl_host` | `127.0.0.1` | Dashboard bind address |
| `dump_tcp_port` | `9912` | php_dd TCP listener port |
| `dump_max_entries` | `500` | Max rows kept in dumps table |
| `caddy_admin_url` | `http://localhost:2019` | Caddy admin API URL |
| `sites_watch_dir` | `/home/$USER/sites` | Directory to watch for auto-discovery |
| `sites_auto_discover` | `true` | Enable/disable auto-discovery |
| `default_php_version` | `8.3` | PHP version for auto-discovered sites |
| `php_extensions` | `[...]` | JSON array of extensions to install per PHP version |
| `mailpit_http_port` | `8025` | Mailpit web UI port |
| `mailpit_smtp_port` | `1025` | Mailpit SMTP port |
| `service_poll_interval` | `5` | Seconds between service status polls |

### Default PHP Extensions (installed with each new PHP version)

```
bcmath, curl, gd, imagick, intl, mbstring, mysql (pdo-mysql),
pgsql (pdo-pgsql), redis, sqlite3, xml, xmlwriter, zip,
opcache, readline, soap
```

## REST API

```
# Services
GET  /api/services                      List all + current status + version
POST /api/services/{id}/start
POST /api/services/{id}/stop
POST /api/services/{id}/restart
GET  /api/services/{id}/logs            SSE: tail log file (last 100 lines + follow)
GET  /api/services/events               SSE: live status updates for all services

# Sites
GET    /api/sites
POST   /api/sites
GET    /api/sites/{id}
PUT    /api/sites/{id}
DELETE /api/sites/{id}
POST   /api/sites/{id}/spx/enable
POST   /api/sites/{id}/spx/disable

# PHP
GET    /api/php/versions                Installed versions + extension lists
POST   /api/php/versions/{ver}/install  Install version + configured extensions
DELETE /api/php/versions/{ver}          Uninstall
GET    /api/php/settings                Global php.ini values
PUT    /api/php/settings                Apply to all installed versions + reload FPM

# Dumps
GET    /api/dumps?page=&limit=&site=    Paginated, filterable by site domain
DELETE /api/dumps                       Clear all
WS     /ws/dumps                        Real-time dump stream

# TLS
GET    /api/tls/cert                    Download Caddy root.crt (PEM)
POST   /api/tls/trust                   Run `caddy trust` (system + Firefox NSS)

# Settings
GET    /api/settings
PUT    /api/settings

# Mail
GET    /api/mail/config                 Returns { httpPort, smtpPort, url }
```

## Dump Wire Format

### TCP line (unchanged from php_dd)
`base64(serialize([null, $context])) + "\n"`

### `$context` structure (new — replaces `html` with `nodes`)

```json
{
  "timestamp": 1710000000.123,
  "source": {
    "file": "/home/daniel/sites/myapp/app/Http/Controllers/FooController.php",
    "line": 42,
    "name": "FooController.php"
  },
  "host": "myapp.test",
  "nodes": [
    {
      "type": "object",
      "class": "App\\User",
      "truncated": 0,
      "children": [
        { "visibility": "public",    "name": "id",    "value": { "type": "scalar", "kind": "int",    "value": 1       } },
        { "visibility": "public",    "name": "name",  "value": { "type": "string", "value": "Alice", "length": 5, "binary": false, "truncated": 0 } },
        { "visibility": "protected", "name": "email", "value": { "type": "string", "value": "alice@example.com", "length": 17, "binary": false, "truncated": 0 } }
      ]
    }
  ]
}
```

### Node types

| Type | Fields |
|---|---|
| `scalar` | `kind`: `null`/`bool`/`int`/`float`/`const`, `value` |
| `string` | `value`, `length`, `binary`, `truncated` (chars cut) |
| `array` | `count`, `indexed` (bool), `truncated`, `children: [{key, value}]` |
| `object` | `class`, `truncated`, `children: [{visibility, name, value}]` |
| `resource` | `resourceType`, `children: [...]` |
| `ref` | `refId`, `refCount` |

## Caddy Vhost Template (per site)

Each site is added to Caddy's JSON config via the Admin API with an `@id` tag for surgical updates:

```json
{
  "@id": "vhost-{site-id}",
  "match": [{ "host": ["{domain}", "...aliases"] }],
  "handle": [{
    "handler": "subroute",
    "routes": [
      {
        "match": [{ "file": { "try_files": ["{path}", "{path}/", "index.php"] } }],
        "handle": [{ "handler": "file_server", "root": "{root_path}" }]
      },
      {
        "match": [{ "not": [{ "file": { "try_files": ["{path}"] } }] }],
        "handle": [{ "handler": "rewrite", "uri": "/index.php{path}?{query}" }]
      },
      {
        "match": [{ "path": ["*.php", "/"] }],
        "handle": [{
          "handler": "reverse_proxy",
          "upstreams": [{ "dial": "unix//run/php/php{version}-fpm.sock" }],
          "transport": {
            "protocol": "fastcgi",
            "root": "{root_path}",
            "split_path": [".php"]
          }
        }]
      }
    ]
  }],
  "terminal": true
}
```

HTTPS: a global `*.test` block in Caddy config uses `tls internal`. All `.test` vhosts inherit it automatically.

## Built-in services.yaml (written to ~/.config/devctl/ on first run)

```yaml
services:
  - id: caddy
    label: Caddy
    start: sudo systemctl start caddy
    stop: sudo systemctl stop caddy
    restart: sudo systemctl restart caddy
    status: systemctl is-active caddy
    status_regex: '(?P<status>active|inactive|failed)'
    version: caddy version
    version_regex: 'v(?P<version>[\d.]+)'
    log: /var/log/caddy/access.log

  - id: redis
    label: Redis
    start: sudo systemctl start redis
    stop: sudo systemctl stop redis
    restart: sudo systemctl restart redis
    status: systemctl is-active redis
    status_regex: '(?P<status>active|inactive|failed)'
    version: redis-server --version
    version_regex: 'v=(?P<version>[\d.]+)'
    log: /var/log/redis/redis-server.log

  - id: postgres
    label: PostgreSQL
    start: sudo systemctl start postgresql
    stop: sudo systemctl stop postgresql
    restart: sudo systemctl restart postgresql
    status: systemctl is-active postgresql
    status_regex: '(?P<status>active|inactive|failed)'
    version: psql --version
    version_regex: '(?P<version>[\d.]+)'
    log: /var/log/postgresql/postgresql-main.log

  - id: mysql
    label: MySQL
    start: sudo systemctl start mysql
    stop: sudo systemctl stop mysql
    restart: sudo systemctl restart mysql
    status: systemctl is-active mysql
    status_regex: '(?P<status>active|inactive|failed)'
    version: mysql --version
    version_regex: '(?P<version>[\d.]+)'
    log: /var/log/mysql/error.log

  - id: meilisearch
    label: Meilisearch
    start: sudo systemctl start meilisearch
    stop: sudo systemctl stop meilisearch
    restart: sudo systemctl restart meilisearch
    status: systemctl is-active meilisearch
    status_regex: '(?P<status>active|inactive|failed)'
    version: meilisearch --version
    version_regex: 'meilisearch (?P<version>[\d.]+)'
    log: /var/log/meilisearch/meilisearch.log

  - id: typesense
    label: Typesense
    start: sudo systemctl start typesense-server
    stop: sudo systemctl stop typesense-server
    restart: sudo systemctl restart typesense-server
    status: systemctl is-active typesense-server
    status_regex: '(?P<status>active|inactive|failed)'
    version: /usr/bin/typesense-server --version
    version_regex: 'typesense-server (?P<version>[\d.]+)'
    log: /var/log/typesense/typesense-server.log

  - id: mailpit
    label: Mailpit
    start: sudo systemctl start mailpit
    stop: sudo systemctl stop mailpit
    restart: sudo systemctl restart mailpit
    status: systemctl is-active mailpit
    status_regex: '(?P<status>active|inactive|failed)'
    version: mailpit version
    version_regex: 'v(?P<version>[\d.]+)'
    log: /var/log/mailpit/mailpit.log

  - id: reverb
    label: Laravel Reverb
    start: sudo systemctl start reverb
    stop: sudo systemctl stop reverb
    restart: sudo systemctl restart reverb
    status: systemctl is-active reverb
    status_regex: '(?P<status>active|inactive|failed)'
    version: ''
    version_regex: ''
    log: ''
```

## Frontend Views

### Layout
Full-height sidebar (shadcn `Sidebar`) + main content area. Sidebar nav items:
- **Services** — badge with count of stopped services (red)
- **Sites** — site count
- **PHP** — installed version count
- **Dumps** — live badge with unread count (clears on view)
- **Mail** — iframe to Mailpit
- **Settings** — gear icon

### Services View
- shadcn `DataTable` with columns: Name, Status (`Badge`: green=Running, red=Stopped, amber=Unknown), Version, Actions
- Per-row: Start / Stop / Restart buttons (disabled contextually based on current status)
- Row click → inline `LogStream.vue` panel (SSE tail, last 100 lines, auto-scroll)
- Live status via SSE `/api/services/events`

### Sites View
- Card grid, one card per vhost: domain (clickable link), root path, PHP version badge, HTTPS + SPX status icons
- "Add Site" → shadcn `Sheet` (slide-in panel) with form: domain, root path, PHP version `Select`, aliases tag input
- Card click → detail `Sheet`: edit form + SPX toggle + "Open Profiler ↗" link + auto-discovered badge
- Live updates via SSE (new/removed sites appear without page refresh)

### PHP View
- One `Card` per installed version
- Card: version number, FPM status badge, installed extensions as `Badge` chips
- "Manage Extensions" → `Dialog` with searchable toggle list; calls apt, reloads FPM
- "Install PHP X.Y" → `Dialog` with version input + extension checklist
- "Uninstall" → confirmation `Dialog`
- **Global Settings** panel: `upload_max_filesize`, `memory_limit`, `max_execution_time` inputs + "Apply to All" button

### Dumps View
- Scrollable list of `DumpCard.vue` components
- `DumpCard`: header bar (ID, file:line, timestamp, site domain badge) + `DumpNode.vue` body
- `DumpNode.vue` recursive renderer:
  - `null`/`bool` → dim tokens
  - `int`/`float` → blue numbers
  - `string` → green, length hint on hover, expand toggle for truncated
  - `array` → collapsible `array:N [...]` with key/index prefixes
  - `object` → collapsible `ClassName { }` with `+`/`#`/`-` visibility prefixes, colored per visibility
  - `resource` → purple type label
  - `ref` → gray `#N` back-reference
- Toolbar: connection status badge, filter by site, Collapse All, Clear All
- Live via WebSocket `/ws/dumps`

### Mail View
- Full-height `<iframe>` to `http://localhost:{mailpit_http_port}`
- Toolbar: SMTP config snippet for copy-paste, Start/Stop Mailpit button

### Settings View
Six card sections:
1. **Dashboard** — bind host, port
2. **Sites** — watch directory, default PHP version, auto-discovery toggle
3. **Caddy** — admin API URL, "Download Root Certificate" button, "Trust Certificate" button
4. **PHP Dump Server** — TCP port, max stored dumps
5. **Mailpit** — HTTP port, SMTP port
6. **PHP Extensions** — editable tag list for extensions installed with new PHP versions

## Makefile Targets

```makefile
make dev-ui      # Vite HMR dev server (frontend only, proxies /api/* to :4000)
make build-ui    # vite build → ui/dist/
make build       # make build-ui + go build -o devctl .
make install     # copy devctl to /usr/local/bin, install devctl.service
make dev         # go run . --dev (serves frontend from disk, verbose logging)
make ext         # Build php_dd C extension (delegates to php_dd_ext/Makefile)
make sqlc        # Run sqlc generate
make db-migrate  # Run goose migrations against dev DB
```

## Implementation Phases

1. **Phase 1 — Scaffold** — Go module, SQLite + migrations, sqlc, config, HTTP server, Vue 3 skeleton, Makefile, first-run logic
2. **Phase 2 — Services** — YAML registry, status poller, start/stop/restart, SSE stream, Services view
3. **Phase 3 — Dumps** — Modify prepend.php for JSON nodes, TCP server, WebSocket hub, DumpNode.vue renderer
4. **Phase 4 — Sites** — Caddy Admin API client, site CRUD, fsnotify watcher, Sites view
5. **Phase 5 — PHP** — Version detection, apt installer, php.ini writer, PHP view
6. **Phase 6 — TLS** — Root cert download, caddy trust endpoint, Settings TLS section
7. **Phase 7 — Mail** — Mailpit service management, iframe view
8. **Phase 8 — SPX** — Per-site toggle, profiler link in Sites detail
9. **Phase 9 — Settings & Polish** — Full settings page, loading skeletons, error boundaries, responsive layout
