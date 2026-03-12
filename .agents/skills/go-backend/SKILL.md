---
name: go-backend
description: Patterns and conventions for working on the devctl Go backend — adding API endpoints, handlers, SSE/WebSocket, service management, and error handling
license: MIT
compatibility: opencode
metadata:
  layer: backend
  language: go
---

## Overview

The backend is a single Go binary (`main.go`) that runs as a **systemd system service** (root). It uses `net/http` stdlib only — no third-party router. All subsystems are wired in `main.go` and passed into `api.NewServer(...)`.

## Package layout

| Package | Responsibility |
|---|---|
| `api/` | HTTP handlers, route registration, SSE, WebSocket upgrade |
| `services/` | Static service registry, exec manager, status poller, process supervisor |
| `sites/` | Site CRUD (SQLite), Caddy Admin API client, fsnotify watcher |
| `php/` | PHP-FPM version detection, installer, php.ini read/write |
| `dumps/` | TCP dump receiver, WebSocket broadcast hub, SQLite store |
| `install/` | Idempotent APT-based service installers |
| `config/` | Config dir setup, static default service definitions |
| `db/` | SQLite open, goose migrations, sqlc-generated queries |

## Adding a new API endpoint

1. Add the route in `api/server.go` inside `registerRoutes()` using Go 1.22+ method+path syntax:
   ```go
   s.mux.HandleFunc("GET /api/example/{id}", s.handleGetExample)
   ```
2. Implement the handler on `*Server` in its own file (e.g. `api/example.go`).
3. Use `r.PathValue("id")` (Go 1.22+) to read path parameters — never use a third-party router.
4. Respond with `writeJSON(w, payload)` for success (defined in `api/` as a small helper).
5. Use `http.Error(w, msg, statusCode)` for errors — keep them terse.

## Handler conventions

```go
func (s *Server) handleGetExample(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    row, err := s.queries.GetExample(r.Context(), id)
    if err == sql.ErrNoRows {
        http.Error(w, "not found", http.StatusNotFound)
        return
    }
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    writeJSON(w, row)
}
```

- Always pass `r.Context()` to DB queries.
- No global state in handlers — all dependencies are on `*Server`.
- Return early on error (no nested ifs).

## SSE (Server-Sent Events)

Pattern used in `api/services.go` (`handleServiceEvents`) and `api/services.go` (`handleServiceLogs`):

```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
flusher := w.(http.Flusher)

for {
    select {
    case <-r.Context().Done():
        return
    case msg := <-ch:
        fmt.Fprintf(w, "data: %s\n\n", msg)
        flusher.Flush()
    }
}
```

## WebSocket

WebSocket upgrade lives in `api/dumps.go`. Uses `github.com/gorilla/websocket`. The hub pattern (register/unregister/broadcast) is in `dumps/hub.go` — follow the same pattern for any new WebSocket endpoint.

## Database access

- All queries go through `s.queries` (`*dbq.Queries` from sqlc codegen in `db/queries/`).
- Raw `*sql.DB` (`s.db`) is available for transactions or one-off exec.
- Never write raw SQL in handlers — add it to `db/queries/*.sql` and regenerate with `make sqlc`.

## Service registry

Service definitions are **static Go code** in `config/defaults.go` — there is no `services.yaml` at runtime. `services.NewRegistry(config.DefaultServices())` converts the slice to an in-memory `*Registry` at startup.

## Process supervisor (`services/supervisor.go`)

`Supervisor` manages child processes for services with `Managed: true` (e.g. Laravel Reverb):

```go
supervisor := services.NewSupervisor()
go supervisor.Run(runCtx)   // auto-restart on crash, stop all on ctx done

supervisor.Start(def)       // forks def.ManagedCmd + def.ManagedArgs in $HOME/sites/<id>
supervisor.Stop(id)         // SIGTERM → 10s → SIGKILL
supervisor.Restart(def)     // Stop + Start
supervisor.IsRunning(id)    // bool
```

- `Start` is a no-op if the process is already running.
- Stdout/Stderr of child processes are forwarded to `log.Printf` with the service ID prefix.
- Working dir is `os.ExpandEnv("$HOME/sites/" + def.ID)` by convention.

## Credentials endpoint

`GET /api/services/{id}/credentials` reads `$HOME/sites/<id>/.env`, parses known keys, and returns a JSON map. Returns 404 if the file is missing. Currently returns `REVERB_APP_ID`, `REVERB_APP_KEY`, `REVERB_APP_SECRET`.

## Running as root / systemd context

- The binary requires root (enforced with an `os.Getuid() == 0` check in `main.go`).
- Config dir: `/etc/devctl/` — database at `/etc/devctl/devctl.db`.
- Log with the stdlib `log` package — output goes to the systemd journal.

## Build

```sh
make dev          # go run . (no frontend rebuild)
make build        # builds frontend then Go binary
make install      # build + install binary + systemd unit (requires root)
```
