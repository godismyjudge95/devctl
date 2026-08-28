# Managed PostgreSQL Extensions + pg_clickhouse

## Goals

1. Install `pg_clickhouse` so local ClickHouse is queryable from Postgres.
2. Standardize managed Postgres extension install via a registry.
3. Surface extension status in Postgres settings (UI) and CLI (AI-friendly).
4. Auto-wire when both Postgres and ClickHouse are installed (any order).
5. Tests only inside Incus — never against host live data.

## Decisions

| Topic | Choice |
|---|---|
| Registry | Light Go registry (`PostgresExtension` interface) |
| Timescale | First registry member (existing deb-extract path) |
| pg_clickhouse files | Extract prebuilt `.deb` from GitHub (customer-testdeb tag) into Percona tree |
| Auto-wire DB | `template1` only (not the `postgres` maintenance DB) |
| Wire depth | `CREATE EXTENSION` + foreign server + user mapping for local CH |
| FDW defaults | HTTP driver, `127.0.0.1:8123`, user `default`, empty password |
| UI | Postgres service settings dialog (read-only list) |
| CLI | `postgres:extensions` + `postgres:extensions:ensure` |

## Status model

Each managed extension reports: `id`, `label`, `files_installed`, `preload_configured`, `wired`, `ready`, `version`, `note`.

## Lifecycle

- Postgres install/update: install all extension files + preloads; wire when peers present.
- ClickHouse install: if Postgres present, install pg_clickhouse files if missing + wire `template1`.
- After Postgres start: poll and create SQL objects (`EnsurePostgresExtensionsAfterStart`).
- Startup: `EnsurePostgresConfig` refreshes files/preload/wire idempotently.

Contrib search modules (`pg_trgm`, `unaccent`, `fuzzystrmatch`, `btree_gin`) ship in the Percona tree and are wired with `CREATE EXTENSION` on template1 and every connectable database except template0.
