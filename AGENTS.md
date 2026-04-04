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

### Running `make install`

Run it without any `sudo` prefix — the Makefile builds the UI and binary as your normal user, then calls `sudo` internally only for the steps that need root:

```sh
make install
```

**Never** run `sudo make install` — it builds the frontend as root, leaving `ui/dist/` owned by root and breaking all future builds with an EACCES error.

If `ui/dist/` is already owned by root (symptoms: Vite fails with `EACCES, Permission denied: .../ui/dist/assets`), fix it first:

```sh
sudo chown -R daniel:daniel ui/dist/
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
| `integration-testing` | Writing, running, or debugging any integration test |

## NEVER run tests on the host machine

**Do not run any `go test` commands on the host.** This machine is a live development system. Even unit tests that appear safe (no build tags, use `t.TempDir()`, etc.) must not be run on the host — the rule is absolute, with no exceptions.

All tests — including plain unit tests in Go packages — must run inside the dedicated Incus test container. Use `go test -c` to compile the test binary on the host, then push and run it inside the container:

```sh
# Unit tests (e.g. cli/ package)
make build
make test-env          # in one terminal — starts container, blocks until Ctrl+C
# in another terminal:
go test -c -o mypackage.test ./cli/
incus file push mypackage.test $DEVCTL_CONTAINER/tmp/mypackage.test
incus exec $DEVCTL_CONTAINER -- chmod 755 /tmp/mypackage.test
incus exec $DEVCTL_CONTAINER -- /tmp/mypackage.test -test.v

# Integration tests (tests/api/)
make build
make test-env          # in one terminal — starts container, blocks until Ctrl+C
DEVCTL_BASE_URL=http://127.0.0.1:4000 make test-api   # in another terminal
```

Load the `integration-testing` skill for the full workflow.

## Integration tests — MUST run inside Incus

The integration tests in `tests/api/` are tagged `//go:build integration` and run against a **live devctl instance**. They mutate real state (emails, sites, services, settings). Running them against the host system devctl at `http://127.0.0.1:4000` will corrupt live data.

**NEVER run integration tests on the host machine.** Always run them inside the dedicated Incus test container.

```sh
# WRONG — runs against host devctl, mutates live data
go test -tags integration ./tests/api/

# RIGHT — build first, then start the container, then run tests
make build
make test-env          # in one terminal — starts container, blocks until Ctrl+C
DEVCTL_BASE_URL=http://127.0.0.1:4000 make test-api   # in another terminal
```

Load the `integration-testing` skill for the full workflow: container setup, TDD procedure, where to put tests, and available helpers.

| Skill | Load when... |
|---|---|
| `integration-testing` | Writing, running, or debugging any integration test |

## Testing after changes

After implementing any feature, add or update the relevant tests before considering the task done:

- **Go package changes** (e.g. `cli/`, `selfinstall/`, `php/`) → add or update unit tests in the same package (`*_test.go`). Compile with `go test -c`, push the binary into the Incus container, and run it there.
- **Backend API changes** → add or update Go API integration tests in `tests/api/`. Load the `integration-testing` skill for the full workflow.
- **Frontend / UI changes** → add or update Playwright e2e tests in `tests/e2e/`. Load the `testing-dashboard` skill for conventions and tooling.

Do not rely on a clean compile as a substitute for automated tests. Never skip running tests because they "look simple" — compile, push, and run in the container every time.

## Networking / DNS

- The `.test` TLD is routed to this machine via the **router's DNS config** — there are no `/etc/hosts` entries for `*.test` domains. Do not add them.
- Caddy listens on `:80` and `:443` and serves `*.test` sites using its **internal CA** (local self-signed certs). The TLS automation policy in `EnsureHTTPServer` sets `issuers: [{module: "internal"}]` for `*.test` — Caddy handles cert generation automatically; do not manually generate or load certificates.

## Server root path

The server root is `~/ddev/sites/server`. The sites path is `~/ddev/sites/`. Any path seen outside of these locations is a red flag indicating misconfiguration. The systemd unit sets `DEVCTL_SERVER_ROOT=/home/daniel/ddev/sites/server`; all runtime paths are derived from this env var via the `paths` package. Never hardcode machine-specific paths — always use `DEVCTL_SERVER_ROOT` or the `paths` package.

## Finding runtime files outside the project

Never hardcode runtime paths. Always resolve `DEVCTL_SERVER_ROOT` from the running service first:

```sh
SERVER_ROOT=$(sudo systemctl show devctl --property=Environment \
  | tr ' ' '\n' | grep DEVCTL_SERVER_ROOT | cut -d= -f2)
SITES_ROOT=$(dirname "$SERVER_ROOT")
```

Then look for files in this order:

1. Sites root: `$SITES_ROOT` (e.g. `$SITES_ROOT/server/php/8.4/php.ini`)
2. Server root: `$SERVER_ROOT`
3. Only if not found in the above: `/etc/devctl/`, system paths, etc.

The vast majority of runtime files (PHP ini, FPM configs, Caddy config, etc.) live under the sites/server root, not under `/etc/php` or other system directories.

## Key conventions

- The binary **requires root** — enforced at startup, logged to systemd journal.
- No third-party HTTP router — use Go 1.22+ `METHOD /path/{param}` pattern in `net/http`.
- No CGO — `modernc.org/sqlite` is pure Go.
- Frontend is embedded in the binary via `//go:embed ui/dist`; always run `make build-ui` before `go build` for a working UI.
- All REST calls in the frontend go through `frontend/src/lib/api.ts` — never call `fetch()` directly in components or stores.
