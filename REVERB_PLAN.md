# Reverb Implementation Plan

## Summary

Add install/purge/manage support for Laravel Reverb as a devctl-supervised child process.
Reverb runs as `php artisan reverb:start` under devctl (not systemd), lives at
`$HOME/sites/reverb`, and is proxied via a Caddy vhost at `reverb.test`.

---

## Key Decisions

- **Convention over config**: Reverb always lives at `$HOME/sites/reverb`. No DB setting for path.
- **services.yaml removed**: Service definitions are static Go code (`config/defaults.go`).
  Per-install overrides use DB settings (existing k/v table). No YAML read/write.
- **Process supervisor**: New `services/supervisor.go` — devctl forks `php artisan reverb:start`
  as a child process, auto-restarts on crash, stops cleanly on devctl shutdown.
  Designed for reuse (Meilisearch, Caddy eventually).
- **Site type**: Sites table gets a generic `settings TEXT DEFAULT '{}'` JSON column
  (not WS-specific columns). WS upstream stored as `{"ws_upstream": "127.0.0.1:7383"}`.
- **Install Registry**: Moves from a package-level `var` to `install.NewRegistry(siteManager, queries)`
  so ReverbInstaller can receive its dependencies.
- **Credentials API**: `GET /api/services/{id}/credentials` reads `$HOME/sites/<id>/.env`
  and returns parsed key/value pairs for known credential keys.

---

## Execution Order

All items are complete. No remaining manual steps — migration `004_site_settings.sql` is embedded
and will be applied automatically the next time devctl starts.

---

## Notes / Watch-outs

- `runShellW` in install helpers doesn't set `Dir` — reverb installer needs a variant
  that sets the working directory for `php artisan` commands. Add `runShellInDirW`. ✅ Done.
- The `.env` patcher must handle both "key exists, update value" and "key missing, append". ✅ Done.
- `config/reverb.php` allowed_origins patch: the generated file uses env() calls.
  After `install:broadcasting`, the line looks like:
  `'allowed_origins' => [env('REVERB_HOST', '127.0.0.1')],`
  Replace with: `'allowed_origins' => ['*'],` ✅ Done.
- Supervisor `Start` must not double-start if already running (guard with IsRunning check). ✅ Done.
- On devctl restart, supervisor auto-starts reverb only if `IsInstalled()` — clean. ✅ Done.
- `make sqlc` requires sqlc to be installed; `make db-migrate` requires goose + the DB to exist.
- `wrapOutput` is defined in `install/postgres.go` (not `install.go`). Use it from there or inline.
- `make db-migrate` uses full path `/home/daniel/go/bin/goose` (sudo strips `$PATH`). ✅ Fixed in Makefile.
