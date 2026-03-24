# Update Flow Testing Plan

## Problem

The update flow has three UI bugs with no test coverage:

1. **Badge never clears** after update — `update_available` stays `true` because `latestVersions[id]` in memory still holds the old "latest" value even after the binary is replaced.
2. **Service doesn't visibly restart** — timing/polling issue, needs test to confirm.
3. **Update button doesn't disappear** — same root cause as #1 (button visibility is driven by `update_available`).

## Why we can't test all services with one test

Each installer's `UpdateW` is an independent implementation. The API layer (`handleServiceUpdate`) is shared, but the update logic per-service is not. **Mailpit** is the test subject — it's the simplest, cleanest path (download → stop → extract → chmod). Services with genuinely different logic (Meilisearch dump/import, Reverb composer, MySQL debs, Postgres/DNS no-op) would need separate dedicated tests if those specific paths need coverage.

## Faking updates in the test container

### Problem 1: The curl shim won't find update artifacts

The curl shim (installed at `/usr/local/bin/curl` inside the Incus container) matches downloaded files by `basename($DEST)`. Install uses `mailpit-linux-amd64.tar.gz`; UpdateW uses `mailpit-update-linux-amd64.tar.gz`. The shim won't find it.

**Fix**: Add the update artifact explicitly to `scripts/download-artifacts.sh`. It downloads the same tarball as the install artifact but saves it under the update filename:

```bash
download "Mailpit ${MAILPIT_VERSION} (update)" \
  "https://github.com/axllent/mailpit/releases/download/${MAILPIT_VERSION}/mailpit-linux-amd64.tar.gz" \
  "mailpit-update-linux-amd64.tar.gz"
```

No shim changes needed.

### Problem 2: `update_available` is never true in the container

The background checker calls `LatestVersion()` → GitHub API → returns same version as installed. `enrichStates` sees no diff → `update_available: false` always.

**Fix**: Add a `/_testing/` debug route group and a `POST /_testing/services/{id}/latest-version` endpoint that injects a fake version into `s.latestVersions`. This triggers `update_available: true` without touching GitHub.

## The `/_testing/` route group

- Routes registered **only** when `DEVCTL_TESTING=true` env var is set.
- `scripts/test-env.sh` sets `Environment=DEVCTL_TESTING=true` in the systemd unit inside the Incus container.
- When `DEVCTL_TESTING` is not set, the routes are simply not registered. Any call to `/_testing/*` falls through to the SPA handler (same as any other unknown URL) — no special 404 handling required.
- If more debug endpoints are needed in the future, add them here under `/_testing/`.

### Endpoint: `POST /_testing/services/{id}/latest-version`

Body: `{"version": "v9999.0.0"}`

- Calls `s.SetLatestVersion(id, version)`
- Fires `s.poller.Poll()` so SSE subscribers get enriched state immediately
- Returns `{"status":"ok"}`

## Bug fix: `update_available` doesn't clear after update

**Root cause**: After `handleServiceUpdate` succeeds, `s.poller.Poll()` broadcasts fresh state but `s.latestVersions[id]` still holds the injected fake version. `enrichStates` sees diff → still `true`.

**Fix**: Add `recheckLatestVersion(ctx, id)` method. After a successful update, call it as a goroutine (using `context.Background()` since `r.Context()` is cancelled when the SSE stream closes).

```go
func (s *Server) recheckLatestVersion(ctx context.Context, id string) {
    inst, ok := s.installers[id]
    if !ok {
        return
    }
    latest, err := inst.LatestVersion(ctx)
    if err != nil {
        log.Printf("update-checker: recheck %s: %v", id, err)
        return
    }
    s.SetLatestVersion(id, latest)
    s.poller.Poll()
}
```

Called at the end of `handleServiceUpdate`, after `sendSSE(w, flusher, "done", ...)`:

```go
go s.recheckLatestVersion(context.Background(), id)
```

## Files changed

| File | Change |
|---|---|
| `scripts/download-artifacts.sh` | Add `mailpit-update-linux-amd64.tar.gz` artifact |
| `scripts/test-env.sh` | Add `Environment=DEVCTL_TESTING=true` to container systemd unit |
| `api/server.go` | Register `/_testing/` routes when `DEVCTL_TESTING=true` |
| `api/services.go` | Add `handleTestingSetLatestVersion` handler |
| `api/services.go` | Add `recheckLatestVersion` method |
| `api/services.go` | Call `recheckLatestVersion` goroutine after successful update |
| `tests/api/helpers_test.go` | Add `pollUpdateAvailable` helper |
| `tests/api/services_update_test.go` | New — Go API integration test (TDD) |
| `tests/e2e/services_update.spec.ts` | New — Playwright e2e test |

## TDD order

1. `download-artifacts.sh` + `test-env.sh` — infrastructure first
2. Write failing Go test (`services_update_test.go` + `pollUpdateAvailable`)
3. Start `make test-env` container; confirm test fails (404 on `/_testing/`)
4. Add `/_testing/` routes + `handleTestingSetLatestVersion` → steps 1-2 of test pass; step 5 fails (badge stays)
5. Add `recheckLatestVersion` → all steps pass
6. Run `make test-api` — no regressions
7. Write Playwright e2e test (`services_update.spec.ts`)
8. Run e2e — confirm passes
9. `make build` — clean compile

## Test: `TestServiceUpdate_Mailpit_UpdateCycle`

```
Pre-condition: ensure mailpit is installed (install if needed)
1. POST /_testing/services/mailpit/latest-version {"version":"v9999.0.0"} → 200
2. pollUpdateAvailable(t, "mailpit", true, 30s)
3. httpSSE POST /api/services/mailpit/update
   → last event == "done"
   → at least one "output" event
4. pollServiceStatus(t, "mailpit", "running", 30s)
5. pollUpdateAvailable(t, "mailpit", false, 30s)
Cleanup: leave mailpit installed (shared with other tests)
```

## Test: `services_update.spec.ts` (Playwright)

```
describe: 'services update lifecycle — Mailpit'
beforeEach: goto /services, wait for table

test 'update badge appears after version injection':
  - POST /_testing/services/mailpit/latest-version via page.request
  - wait for badge text "update" visible in Mailpit row (30s)

test 'click Update — service restarts and badge clears':
  - inject fake version (idempotent — so test works standalone)
  - wait for badge visible
  - click Update button in Mailpit row
  - wait for status cell → "running" (UPDATE_TIMEOUT = 2 min)
  - assert badge "update" not visible
  - assert Update button not visible
```
