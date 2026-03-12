---
name: db-migrations
description: How to add or modify the devctl SQLite schema using goose SQL migrations and regenerate type-safe queries with sqlc
license: MIT
compatibility: opencode
metadata:
  layer: backend
  concerns: database, schema, sqlc, goose
---

## Overview

devctl uses **SQLite** (`modernc.org/sqlite` — pure Go, no CGO) with two complementary tools:

- **goose** — incremental SQL migrations stored as embedded files
- **sqlc** — generates type-safe Go query code from `.sql` files

## Migration files

Location: `db/migrations/`

Current migrations:
- `001_sites.sql`
- `002_dumps.sql`
- `003_settings.sql`
- `004_site_settings.sql` — adds `settings TEXT NOT NULL DEFAULT '{}'` to `sites` table

### File naming

Use sequential integer prefixes with a descriptive snake_case suffix:

```
004_example_table.sql
```

### File format

Each migration file requires goose directive comments:

```sql
-- +goose Up
CREATE TABLE example (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE example;
```

Rules:
- Always include a `-- +goose Down` block (even if it's just `DROP TABLE`).
- Use `TEXT` for UUIDs and JSON arrays (SQLite has no native UUID or array type).
- Use `INTEGER NOT NULL DEFAULT 0` for booleans.
- Use `DATETIME NOT NULL DEFAULT (datetime('now'))` for timestamps.

### Applying migrations

Migrations are applied **automatically at startup** — `db/db.go` calls `goose.Up` with the embedded migration files every time devctl starts. You never need to run migrations manually in production; just restart the service.

The `make db-migrate` target exists only as a development convenience (e.g. to apply a migration while iterating without restarting the service):

```sh
make db-migrate
# equivalent: /home/daniel/go/bin/goose -dir db/migrations sqlite3 /etc/devctl/devctl.db up
# NOTE: uses full goose path — sudo strips $PATH, so the Makefile uses the absolute path
```

## sqlc — query codegen

Location of source SQL: `db/queries/`  
Location of generated Go: `db/queries/` (same dir, `.go` files)

### Adding a new query

1. Open or create a `.sql` file in `db/queries/` (e.g. `example.sql`).
2. Write the query with a sqlc annotation:

```sql
-- name: GetExample :one
SELECT * FROM example WHERE id = ? LIMIT 1;

-- name: ListExamples :many
SELECT * FROM example ORDER BY created_at DESC;

-- name: CreateExample :exec
INSERT INTO example (id, name, created_at) VALUES (?, ?, ?);

-- name: DeleteExample :exec
DELETE FROM example WHERE id = ?;
```

3. Regenerate:

```sh
make sqlc
# equivalent: cd db && sqlc generate
```

This updates `db/queries/models.go` and the corresponding `*_sql.go` file.

### Using generated queries in handlers

`s.queries` on `*api.Server` is a `*dbq.Queries`. All generated methods are available on it:

```go
row, err := s.queries.GetExample(r.Context(), id)
rows, err := s.queries.ListExamples(r.Context())
err = s.queries.CreateExample(r.Context(), dbq.CreateExampleParams{
    ID:        uuid.NewString(),
    Name:      "foo",
    CreatedAt: time.Now().UTC().Format(time.RFC3339),
})
```

## Checklist when changing the schema

- [ ] Add a new numbered migration file in `db/migrations/`
- [ ] Add/update query SQL in `db/queries/`
- [ ] Run `make sqlc` to regenerate Go types
- [ ] Update any affected handler code to use the new generated types
- [ ] Test with `make db-migrate` against the dev DB (`/etc/devctl/devctl.db`)
