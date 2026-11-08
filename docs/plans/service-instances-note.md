# Feasibility: more versions and instances of a service (plan item 4.1)

Question: can devctl run two versions (or two instances) of one managed
service at the same time? Owner position: "maybe, but not needed if too
difficult".

**Recommendation: stop.** The cost is high for each service, and the value is
low for most of them. Details and the first slice (if the owner wants it later)
are at the end.

## What already works

- `services.Registry` is keyed only by `Definition.ID` and supports runtime
  `Register`/`Unregister` (services/registry.go:25-48). Two definitions with
  different IDs can live side by side.
- PHP-FPM already does this: one definition per installed version, ID
  `php-fpm-<ver>`, registered at startup (main.go:257-260, main.go:476-497).
  That is the pattern a `postgres-17` or `redis-2` would copy.
- The supervisor, logs (`paths.LogPath(serverRoot, id)`, paths/paths.go:102),
  and `services:*` CLI commands work for any ID.

## Blockers shared by every service

1. One installer per fixed ID: the installer map holds one entry per service
   (install/install.go:93-140). There is no "install version X as ID Y".
2. Fixed install directory: every installer calls
   `paths.ServiceDir(serverRoot, "<name>")` with a literal name
   (for example install/valkey.go:81, install/mysql.go:142,
   install/postgres.go:120). Data dirs sit inside that directory.
3. One pinned version: one constant per service (install/postgres.go:31-32,
   install/mysql.go:18-21, `InstallVersion` in config/defaults.go:57-216), and
   the download catalog has one row per GOOS/GOARCH/name
   (dist/dist.go:29, dist/dist.go:89-95).
4. No port model: ports are literals in config files, `config.env`,
   health checks, and API clients. Only Mailpit and MySQL have a port setting,
   and each is one global key matched on the exact ID
   (api/services.go:491-523, api/services.go:528-534).
5. ID switches in the API and UI: config editor (api/service_settings.go:320-352,
   444-448), databases (api/databases.go:89-128, database/open.go:75-81),
   UI (frontend/src/views/ServicesView.vue:252, 283). A `postgres-17` ID
   would fall through every switch.
6. Unversioned wrappers in `{serverRoot}/bin`: one `psql`, one `mysql`, one
   `valkey-cli` each. A second version needs suffixed names or a selector.

## Per service

| Service | Port, dirs, wrappers that block a second copy | Cost |
|---|---|---|
| Valkey (`redis`) | Port 6379 in valkey.conf and config.env (install/valkey.go:141, 275-279); `valkey-server`/`valkey-cli` linked into bin (install/valkey.go:121-133). Data in the one valkey dir. | S-M |
| PostgreSQL | Version and major pinned; TimescaleDB built against 18.4 (install/postgres.go:29-32); extensions resolve one PG dir (install/pgext.go:37). Port 5432 in postgresql.conf (install/postgres.go:230), config.env (install/postgres.go:537), health check (config/defaults.go:85). Client wrappers fixed names (install/postgres.go:561-578). | L |
| MySQL | Version pinned (install/mysql.go:18-21); port 3306 in my.cnf (install/mysql.go:168, 278); one `mysql_port` setting (api/services.go:509-523); socket in mysqlDir (config/defaults.go:105); wrappers `mysql`, `mysqldump`, `mysqladmin` (install/mysql.go:509-512). | L |
| Mailpit | Ports 1025/8025 in config.env and connection.env (install/mailpit.go:89-99); global port settings (api/services.go:495-503); mail CLI and API talk to one Mailpit. | M |
| Meilisearch | Port 7700 in config and in the upgrade code (install/meilisearch.go:262-394, 465); fixed vhost `meilisearch.test` (install/meilisearch.go:110-112); data format tied to version. | M |
| Typesense | Port 8108 and fixed vhost `typesense.test` (install/typesense.go:111-113). | M |
| Reverb | One Laravel app in the reverb dir; port 7383 (install/reverb.go:91, 147; config/defaults.go:167); vhost `reverb.test` (install/reverb.go:144). | M |
| MaxIO | Port 9900 (install/maxio.go:103, 126, 141; config/defaults.go:208); vhosts `maxio.test`, `s3.maxio.test` (install/maxio.go:124, 138). | M |
| ClickHouse | HTTP 8123 (install/clickhouse.go:150; config/defaults.go:233) plus native port in config.xml; multi-call names linked into bin (install/clickhouse.go:115-127). | M-L |

S = under a day, M = 2-3 days, L = a week or more (each with tests in the VM).
Each estimate assumes the shared work below is done once first (about M-L).

## Shared work before any one service

- An instance model: `{service, instance, version, port}` stored in SQLite, with
  the ID `<service>-<instance>` and a port allocator that checks for free ports.
- Installers take an instance dir and port instead of a literal name.
- API, CLI, and UI switches match on the service kind, not the exact ID.
- A rule for `{serverRoot}/bin` wrappers (default instance keeps the plain
  name; others get a suffix such as `psql-17`).

## Value check

- Real need: PostgreSQL and MySQL major versions (match production).
- Low need: Valkey, Mailpit, Meilisearch, Typesense, MaxIO, ClickHouse. One
  instance with key prefixes, databases, indexes, or buckets covers almost all
  local work.
- No need: Reverb. Each app normally runs its own Reverb.

## Recommendation

Stop now. The shared work plus PostgreSQL alone is about two weeks, it
touches installers, API, CLI, UI, and the bin wrappers, and the owner marked
the item as optional.

If the owner wants it later, the smallest first slice is **a second
PostgreSQL major version** (for example 17 next to 18):

1. `postgres-<major>` definitions registered at startup, as PHP-FPM does.
2. Install dir `{serverRoot}/postgres-<major>`, port from a per-instance
   setting (default 5432 + offset), health check and config.env built from it.
3. Wrappers `psql-<major>` and so on; plain `psql` stays on the default.
4. Database switches match the `postgres` prefix.
5. TimescaleDB and other extensions stay on the default major only.
