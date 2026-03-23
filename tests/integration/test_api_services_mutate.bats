#!/usr/bin/env bats
# test_api_services_mutate.bats — Full install → running → stop → start → purge
# lifecycle tests for every installable devctl service.
#
# Tests are ordered: each service's install must succeed before its stop/start/
# purge tests run.  BATS executes @test blocks in file order.
#
# Run time: ~5–10 min with cached artifacts; ~30 min with fresh downloads.
# All installs require the devctl-test-artifacts cache to be populated
# (run `sudo make test-artifacts-download` once).

load setup

# ─── Caddy (required, already running — reinstall then verify) ─────────────────

# Caddy is a required service.  It is already installed and running when the
# container starts.  We verify its state and then exercise start/restart.
# (Purge is blocked for required services — tested separately.)

@test "caddy: installed=true in initial state" {
  run bash -c "curl -sf '${BASE_URL}/api/services' | jq -r '.[] | select(.id==\"caddy\") | .installed'"
  [ "$status" -eq 0 ]
  [[ "$output" == "true" ]]
}

@test "caddy: status=running in initial state" {
  poll_service_status caddy running 15
}

@test "caddy: binary exists on disk" {
  container_exec test -f "${SERVER_ROOT}/caddy/caddy"
}

@test "caddy: restart returns 200" {
  status=$(api_post_status /api/services/caddy/restart "")
  [ "$status" -eq 200 ]
}

@test "caddy: status=running after restart" {
  poll_service_status caddy running 20
}

# ─── Valkey (Redis) ────────────────────────────────────────────────────────────

@test "valkey: installed=false before install" {
  run bash -c "curl -sf '${BASE_URL}/api/services' | jq -r '.[] | select(.id==\"redis\") | .installed'"
  [ "$status" -eq 0 ]
  [[ "$output" == "false" ]]
}

@test "valkey: install SSE stream ends with done event" {
  api_sse POST /api/services/redis/install 300
}

@test "valkey: installed=true after install" {
  poll_service_field redis installed true 10
}

@test "valkey: status=running after install" {
  poll_service_status redis running 30
}

@test "valkey: binary exists on disk" {
  container_exec test -f "${SERVER_ROOT}/valkey/valkey-server"
}

@test "valkey: symlink in bin dir" {
  container_exec test -L "${SERVER_ROOT}/bin/valkey-server"
}

@test "valkey: config.env exists and non-empty" {
  container_exec test -s "${SERVER_ROOT}/valkey/config.env"
}

@test "valkey: config.env contains REDIS_HOST" {
  container_exec grep -q "REDIS_HOST" "${SERVER_ROOT}/valkey/config.env"
}

@test "valkey: config.env contains REDIS_PASSWORD" {
  container_exec grep -q "REDIS_PASSWORD" "${SERVER_ROOT}/valkey/config.env"
}

@test "valkey: stop returns 200" {
  status=$(api_post_status /api/services/redis/stop "")
  [ "$status" -eq 200 ]
}

@test "valkey: status=stopped after stop" {
  poll_service_status redis stopped 15
}

@test "valkey: start returns 200" {
  status=$(api_post_status /api/services/redis/start "")
  [ "$status" -eq 200 ]
}

@test "valkey: status=running after start" {
  poll_service_status redis running 20
}

@test "valkey: purge SSE stream ends with done event" {
  api_sse DELETE /api/services/redis 60
}

@test "valkey: installed=false after purge" {
  poll_service_field redis installed false 10
}

@test "valkey: binary removed after purge" {
  run container_exec bash -c "test ! -f '${SERVER_ROOT}/valkey/valkey-server'"
  [ "$status" -eq 0 ]
}

@test "valkey: symlink removed after purge" {
  run container_exec bash -c "test ! -L '${SERVER_ROOT}/bin/valkey-server'"
  [ "$status" -eq 0 ]
}

# ─── Mailpit ───────────────────────────────────────────────────────────────────

@test "mailpit: installed=false before install" {
  run bash -c "curl -sf '${BASE_URL}/api/services' | jq -r '.[] | select(.id==\"mailpit\") | .installed'"
  [ "$status" -eq 0 ]
  [[ "$output" == "false" ]]
}

@test "mailpit: install SSE stream ends with done event" {
  api_sse POST /api/services/mailpit/install 180
}

@test "mailpit: installed=true after install" {
  poll_service_field mailpit installed true 10
}

@test "mailpit: status=running after install" {
  poll_service_status mailpit running 30
}

@test "mailpit: binary exists on disk" {
  container_exec test -f "${SERVER_ROOT}/mailpit/mailpit"
}

@test "mailpit: symlink in bin dir" {
  container_exec test -L "${SERVER_ROOT}/bin/mailpit"
}

@test "mailpit: config.env exists and non-empty" {
  container_exec test -s "${SERVER_ROOT}/mailpit/config.env"
}

@test "mailpit: connection.env exists and non-empty" {
  container_exec test -s "${SERVER_ROOT}/mailpit/connection.env"
}

@test "mailpit: connection.env contains MAIL_HOST" {
  container_exec grep -q "MAIL_HOST" "${SERVER_ROOT}/mailpit/connection.env"
}

@test "mailpit: stop returns 200" {
  status=$(api_post_status /api/services/mailpit/stop "")
  [ "$status" -eq 200 ]
}

@test "mailpit: status=stopped after stop" {
  poll_service_status mailpit stopped 15
}

@test "mailpit: start returns 200" {
  status=$(api_post_status /api/services/mailpit/start "")
  [ "$status" -eq 200 ]
}

@test "mailpit: status=running after start" {
  poll_service_status mailpit running 20
}

@test "mailpit: purge SSE stream ends with done event" {
  api_sse DELETE /api/services/mailpit 60
}

@test "mailpit: installed=false after purge" {
  poll_service_field mailpit installed false 10
}

@test "mailpit: binary removed after purge" {
  run container_exec bash -c "test ! -f '${SERVER_ROOT}/mailpit/mailpit'"
  [ "$status" -eq 0 ]
}

# ─── Meilisearch ───────────────────────────────────────────────────────────────

@test "meilisearch: installed=false before install" {
  run bash -c "curl -sf '${BASE_URL}/api/services' | jq -r '.[] | select(.id==\"meilisearch\") | .installed'"
  [ "$status" -eq 0 ]
  [[ "$output" == "false" ]]
}

@test "meilisearch: install SSE stream ends with done event" {
  api_sse POST /api/services/meilisearch/install 300
}

@test "meilisearch: installed=true after install" {
  poll_service_field meilisearch installed true 10
}

@test "meilisearch: status=running after install" {
  poll_service_status meilisearch running 30
}

@test "meilisearch: binary exists on disk" {
  container_exec test -f "${SERVER_ROOT}/meilisearch/meilisearch"
}

@test "meilisearch: symlink in bin dir" {
  container_exec test -L "${SERVER_ROOT}/bin/meilisearch"
}

@test "meilisearch: config.env exists and contains master key" {
  container_exec test -s "${SERVER_ROOT}/meilisearch/config.env"
  container_exec grep -q "MEILISEARCH_KEY" "${SERVER_ROOT}/meilisearch/config.env"
}

@test "meilisearch: caddy vhost site appears in GET /api/sites" {
  run bash -c "curl -sf '${BASE_URL}/api/sites' | jq -r '.[].domain' | grep -q meilisearch"
  [ "$status" -eq 0 ]
}

@test "meilisearch: stop returns 200" {
  status=$(api_post_status /api/services/meilisearch/stop "")
  [ "$status" -eq 200 ]
}

@test "meilisearch: status=stopped after stop" {
  poll_service_status meilisearch stopped 15
}

@test "meilisearch: start returns 200" {
  status=$(api_post_status /api/services/meilisearch/start "")
  [ "$status" -eq 200 ]
}

@test "meilisearch: status=running after start" {
  poll_service_status meilisearch running 20
}

@test "meilisearch: purge SSE stream ends with done event" {
  api_sse DELETE /api/services/meilisearch 60
}

@test "meilisearch: installed=false after purge" {
  poll_service_field meilisearch installed false 10
}

@test "meilisearch: binary removed after purge" {
  run container_exec bash -c "test ! -f '${SERVER_ROOT}/meilisearch/meilisearch'"
  [ "$status" -eq 0 ]
}

@test "meilisearch: caddy vhost removed after purge" {
  run bash -c "curl -sf '${BASE_URL}/api/sites' | jq -r '.[].domain' | grep -q meilisearch"
  [ "$status" -ne 0 ]
}

# ─── Typesense ─────────────────────────────────────────────────────────────────

@test "typesense: installed=false before install" {
  run bash -c "curl -sf '${BASE_URL}/api/services' | jq -r '.[] | select(.id==\"typesense\") | .installed'"
  [ "$status" -eq 0 ]
  [[ "$output" == "false" ]]
}

@test "typesense: install SSE stream ends with done event" {
  api_sse POST /api/services/typesense/install 300
}

@test "typesense: installed=true after install" {
  poll_service_field typesense installed true 10
}

@test "typesense: status=running after install" {
  poll_service_status typesense running 30
}

@test "typesense: binary exists on disk" {
  container_exec test -f "${SERVER_ROOT}/typesense/typesense-server"
}

@test "typesense: symlink in bin dir" {
  container_exec test -L "${SERVER_ROOT}/bin/typesense-server"
}

@test "typesense: config.env exists and contains API key" {
  container_exec test -s "${SERVER_ROOT}/typesense/config.env"
  container_exec grep -q "TYPESENSE_API_KEY" "${SERVER_ROOT}/typesense/config.env"
}

@test "typesense: caddy vhost appears in GET /api/sites" {
  run bash -c "curl -sf '${BASE_URL}/api/sites' | jq -r '.[].domain' | grep -q typesense"
  [ "$status" -eq 0 ]
}

@test "typesense: stop returns 200" {
  status=$(api_post_status /api/services/typesense/stop "")
  [ "$status" -eq 200 ]
}

@test "typesense: status=stopped after stop" {
  poll_service_status typesense stopped 15
}

@test "typesense: start returns 200" {
  status=$(api_post_status /api/services/typesense/start "")
  [ "$status" -eq 200 ]
}

@test "typesense: status=running after start" {
  poll_service_status typesense running 20
}

@test "typesense: purge SSE stream ends with done event" {
  api_sse DELETE /api/services/typesense 60
}

@test "typesense: installed=false after purge" {
  poll_service_field typesense installed false 10
}

@test "typesense: binary removed after purge" {
  run container_exec bash -c "test ! -f '${SERVER_ROOT}/typesense/typesense-server'"
  [ "$status" -eq 0 ]
}

# ─── WhoDB ─────────────────────────────────────────────────────────────────────

@test "whodb: installed=false before install" {
  run bash -c "curl -sf '${BASE_URL}/api/services' | jq -r '.[] | select(.id==\"whodb\") | .installed'"
  [ "$status" -eq 0 ]
  [[ "$output" == "false" ]]
}

@test "whodb: install SSE stream ends with done event" {
  api_sse POST /api/services/whodb/install 180
}

@test "whodb: installed=true after install" {
  poll_service_field whodb installed true 10
}

@test "whodb: status=running after install" {
  poll_service_status whodb running 30
}

@test "whodb: binary exists on disk" {
  container_exec test -f "${SERVER_ROOT}/whodb/whodb"
}

@test "whodb: symlink in bin dir" {
  container_exec test -L "${SERVER_ROOT}/bin/whodb"
}

@test "whodb: caddy vhost appears in GET /api/sites" {
  run bash -c "curl -sf '${BASE_URL}/api/sites' | jq -r '.[].domain' | grep -q whodb"
  [ "$status" -eq 0 ]
}

@test "whodb: stop returns 200" {
  status=$(api_post_status /api/services/whodb/stop "")
  [ "$status" -eq 200 ]
}

@test "whodb: status=stopped after stop" {
  poll_service_status whodb stopped 15
}

@test "whodb: start returns 200" {
  status=$(api_post_status /api/services/whodb/start "")
  [ "$status" -eq 200 ]
}

@test "whodb: status=running after start" {
  poll_service_status whodb running 20
}

@test "whodb: purge SSE stream ends with done event" {
  api_sse DELETE /api/services/whodb 60
}

@test "whodb: installed=false after purge" {
  poll_service_field whodb installed false 10
}

@test "whodb: binary removed after purge" {
  run container_exec bash -c "test ! -f '${SERVER_ROOT}/whodb/whodb'"
  [ "$status" -eq 0 ]
}

# ─── RustFS ────────────────────────────────────────────────────────────────────

@test "rustfs: installed=false before install" {
  run bash -c "curl -sf '${BASE_URL}/api/services' | jq -r '.[] | select(.id==\"rustfs\") | .installed'"
  [ "$status" -eq 0 ]
  [[ "$output" == "false" ]]
}

@test "rustfs: install SSE stream ends with done event" {
  api_sse POST /api/services/rustfs/install 180
}

@test "rustfs: installed=true after install" {
  poll_service_field rustfs installed true 10
}

@test "rustfs: status=running after install" {
  poll_service_status rustfs running 30
}

@test "rustfs: binary exists on disk" {
  container_exec test -f "${SERVER_ROOT}/rustfs/rustfs"
}

@test "rustfs: symlink in bin dir" {
  container_exec test -L "${SERVER_ROOT}/bin/rustfs"
}

@test "rustfs: config.env exists and contains access key" {
  container_exec test -s "${SERVER_ROOT}/rustfs/config.env"
  container_exec grep -q "RUSTFS_ACCESS_KEY\|RUSTFS_ROOT_USER\|ACCESS_KEY" "${SERVER_ROOT}/rustfs/config.env"
}

@test "rustfs: connection.env exists" {
  container_exec test -s "${SERVER_ROOT}/rustfs/connection.env"
}

@test "rustfs: caddy vhost rustfs.test appears in GET /api/sites" {
  run bash -c "curl -sf '${BASE_URL}/api/sites' | jq -r '.[].domain' | grep -q 'rustfs\.test'"
  [ "$status" -eq 0 ]
}

@test "rustfs: stop returns 200" {
  status=$(api_post_status /api/services/rustfs/stop "")
  [ "$status" -eq 200 ]
}

@test "rustfs: status=stopped after stop" {
  poll_service_status rustfs stopped 15
}

@test "rustfs: start returns 200" {
  status=$(api_post_status /api/services/rustfs/start "")
  [ "$status" -eq 200 ]
}

@test "rustfs: status=running after start" {
  poll_service_status rustfs running 20
}

@test "rustfs: purge SSE stream ends with done event" {
  api_sse DELETE /api/services/rustfs 60
}

@test "rustfs: installed=false after purge" {
  poll_service_field rustfs installed false 10
}

@test "rustfs: binary removed after purge" {
  run container_exec bash -c "test ! -f '${SERVER_ROOT}/rustfs/rustfs'"
  [ "$status" -eq 0 ]
}

# ─── PostgreSQL ────────────────────────────────────────────────────────────────

@test "postgres: installed=false before install" {
  run bash -c "curl -sf '${BASE_URL}/api/services' | jq -r '.[] | select(.id==\"postgres\") | .installed'"
  [ "$status" -eq 0 ]
  [[ "$output" == "false" ]]
}

@test "postgres: install SSE stream ends with done event (may take several minutes)" {
  api_sse POST /api/services/postgres/install 600
}

@test "postgres: installed=true after install" {
  poll_service_field postgres installed true 10
}

@test "postgres: status=running after install" {
  poll_service_status postgres running 60
}

@test "postgres: postgres binary exists on disk" {
  container_exec test -f "${SERVER_ROOT}/postgres/bin/postgres"
}

@test "postgres: psql symlink in bin dir" {
  container_exec test -L "${SERVER_ROOT}/bin/psql"
}

@test "postgres: data directory initialised by initdb" {
  container_exec test -d "${SERVER_ROOT}/postgres/data/base"
}

@test "postgres: config.env exists with connection details" {
  container_exec test -s "${SERVER_ROOT}/postgres/config.env"
  container_exec grep -q "POSTGRES_HOST\|DB_HOST\|PGHOST" "${SERVER_ROOT}/postgres/config.env"
}

@test "postgres: libreadline-dev APT package installed" {
  container_exec dpkg -l libreadline-dev | grep -q "^ii"
}

@test "postgres: stop returns 200" {
  status=$(api_post_status /api/services/postgres/stop "")
  [ "$status" -eq 200 ]
}

@test "postgres: status=stopped after stop" {
  poll_service_status postgres stopped 20
}

@test "postgres: start returns 200" {
  status=$(api_post_status /api/services/postgres/start "")
  [ "$status" -eq 200 ]
}

@test "postgres: status=running after start" {
  poll_service_status postgres running 30
}

@test "postgres: purge SSE stream ends with done event" {
  api_sse DELETE /api/services/postgres 120
}

@test "postgres: installed=false after purge" {
  poll_service_field postgres installed false 10
}

@test "postgres: postgres binary removed after purge" {
  run container_exec bash -c "test ! -f '${SERVER_ROOT}/postgres/bin/postgres'"
  [ "$status" -eq 0 ]
}

# ─── MySQL ─────────────────────────────────────────────────────────────────────

@test "mysql: installed=false before install" {
  run bash -c "curl -sf '${BASE_URL}/api/services' | jq -r '.[] | select(.id==\"mysql\") | .installed'"
  [ "$status" -eq 0 ]
  [[ "$output" == "false" ]]
}

@test "mysql: install SSE stream ends with done event (may take several minutes)" {
  api_sse POST /api/services/mysql/install 600
}

@test "mysql: installed=true after install" {
  poll_service_field mysql installed true 10
}

@test "mysql: status=running after install" {
  poll_service_status mysql running 60
}

@test "mysql: mysqld binary exists on disk" {
  container_exec test -f "${SERVER_ROOT}/mysql/bin/mysqld"
}

@test "mysql: mysql client wrapper in bin dir" {
  container_exec test -x "${SERVER_ROOT}/bin/mysql"
}

@test "mysql: data directory initialised by mysqld --initialize-insecure" {
  container_exec test -d "${SERVER_ROOT}/mysql/data/mysql"
}

@test "mysql: config.env exists with connection details" {
  container_exec test -s "${SERVER_ROOT}/mysql/config.env"
  container_exec grep -q "MYSQL_HOST\|DB_HOST\|MYSQL_PORT" "${SERVER_ROOT}/mysql/config.env"
}

@test "mysql: libnuma1 APT package installed" {
  container_exec dpkg -l libnuma1 | grep -q "^ii"
}

@test "mysql: stop returns 200" {
  status=$(api_post_status /api/services/mysql/stop "")
  [ "$status" -eq 200 ]
}

@test "mysql: status=stopped after stop" {
  poll_service_status mysql stopped 20
}

@test "mysql: start returns 200" {
  status=$(api_post_status /api/services/mysql/start "")
  [ "$status" -eq 200 ]
}

@test "mysql: status=running after start" {
  poll_service_status mysql running 30
}

@test "mysql: purge SSE stream ends with done event" {
  api_sse DELETE /api/services/mysql 120
}

@test "mysql: installed=false after purge" {
  poll_service_field mysql installed false 10
}

@test "mysql: mysqld binary removed after purge" {
  run container_exec bash -c "test ! -f '${SERVER_ROOT}/mysql/bin/mysqld'"
  [ "$status" -eq 0 ]
}
