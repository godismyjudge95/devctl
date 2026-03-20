# devctl — Agent Guide

devctl is a local PHP development environment dashboard for Linux. It runs as a **systemd system service** (root) and serves a browser UI at `http://127.0.0.1:4000`. It manages Caddy, PHP-FPM, and dev services (Redis, PostgreSQL, MySQL, Mailpit, Meilisearch, Typesense, Laravel Reverb).

## Tech stack

| Layer | Choice |
|---|---|
| Backend | Go 1.25, stdlib `net/http`, no third-party router |
| Database | SQLite (`modernc.org/sqlite`), sqlc (codegen), goose (migrations) |
| Frontend | Vue 3 + TypeScript, Pinia, Vite 7, Tailwind CSS v4, shadcn-vue |
| Proxy / TLS | Caddy with internal CA, wildcard `*.test` certs |
| Service unit | `devctl.service` — systemd system service (`/etc/systemd/system/`) |

## Runtime paths

| Path | Purpose |
|---|---|
| `/etc/devctl/devctl.db` | SQLite database |
| `/etc/devctl/services.yaml` | Service definitions (written once on first run) |
| `127.0.0.1:4000` | HTTP dashboard |
| `127.0.0.1:9912` | TCP dump receiver (PHP `php_dd()`) |

## Build commands

```sh
make dev          # go run . (backend only, no frontend rebuild)
make dev-ui       # Vite HMR dev server (cd frontend && npm run dev)
make build        # build-ui + go build
make install      # build + install binary + systemd unit (requires root)
make sqlc         # regenerate db/queries/*.go from db/queries/*.sql
make db-migrate   # apply goose migrations to /etc/devctl/devctl.db
```

## Key source files

```
main.go                     # entry point, subsystem wiring, graceful shutdown
api/server.go               # route registration (all /api/* + SPA fallback)
config/defaults.go          # default service definitions
services/definition.go      # Definition and ServiceState structs
install/install.go          # Installer interface + shared APT/systemctl helpers
db/migrations/              # goose SQL migration files
db/queries/                 # sqlc input SQL + generated Go
frontend/src/lib/api.ts     # all typed fetch wrappers
frontend/src/stores/        # Pinia stores (services, sites, dumps, settings)
```

## Available skills

Load these when working on specific areas:

| Skill | Load when... |
|---|---|
| `go-backend` | Adding API endpoints, handlers, SSE/WebSocket, or touching any Go backend code |
| `vue-frontend` | Working on the Vue SPA — stores, components, API wrappers, Vite pipeline |
| `db-migrations` | Adding or modifying the SQLite schema or sqlc queries |
| `add-service` | Adding a new managed dev service (Definition + defaults.go entry) |
| `install-package` | Implementing a new APT-based service installer |
| `update-skills` | Creating or updating agent skills for this project |

## Testing after changes

After implementing any feature, **always test it in the browser** before considering the task done:

1. Run `make install` to build and install the binary.
2. Run `sudo systemctl restart devctl` and wait for it to be active.
3. Open `http://127.0.0.1:4000` in the browser using the Playwright browser tool.
4. Navigate to the relevant page and verify the feature works end-to-end.
5. Check `sudo journalctl -u devctl -n 40 --no-pager` for any startup errors.

Do not rely on a clean compile as a substitute for a live browser test.

## Networking / DNS

- The `.test` TLD is routed to this machine via the **router's DNS config** — there are no `/etc/hosts` entries for `*.test` domains. Do not add them.
- Caddy listens on `:80` and `:443` and serves `*.test` sites using its **internal CA** (local self-signed certs). The TLS automation policy in `EnsureHTTPServer` sets `issuers: [{module: "internal"}]` for `*.test` — Caddy handles cert generation automatically; do not manually generate or load certificates.

## Server root path

The server root is `~/ddev/sites/server`. The sites path is `~/ddev/sites/`. Any path seen outside of these locations is a red flag indicating misconfiguration. The systemd unit sets `DEVCTL_SERVER_ROOT=/home/daniel/ddev/sites/server`; all runtime paths are derived from this env var via the `paths` package. Never hardcode machine-specific paths — always use `DEVCTL_SERVER_ROOT` or the `paths` package.

## Key conventions

- The binary **requires root** — enforced at startup, logged to systemd journal.
- No third-party HTTP router — use Go 1.22+ `METHOD /path/{param}` pattern in `net/http`.
- No CGO — `modernc.org/sqlite` is pure Go.
- Frontend is embedded in the binary via `//go:embed ui/dist`; always run `make build-ui` before `go build` for a working UI.
- All REST calls in the frontend go through `frontend/src/lib/api.ts` — never call `fetch()` directly in components or stores.
