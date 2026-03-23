#!/usr/bin/env bats
# test_api_services_credentials.bats — Verify the credentials endpoint
# for services that expose connection credentials.
#
# This file installs mailpit and valkey as minimal dependencies, checks
# their credentials, then purges them.  It is self-contained — services
# are installed and removed within this file's @test sequence.

load setup

# ─── Setup: install mailpit and valkey ────────────────────────────────────────

@test "credentials-setup: install mailpit" {
  api_sse POST /api/services/mailpit/install 180
  poll_service_field mailpit installed true 10
}

@test "credentials-setup: install valkey" {
  api_sse POST /api/services/redis/install 180
  poll_service_field redis installed true 10
}

# ─── Mailpit credentials ──────────────────────────────────────────────────────

@test "mailpit: GET /api/services/mailpit/credentials returns 200" {
  status=$(api_status /api/services/mailpit/credentials)
  [ "$status" -eq 200 ]
}

@test "mailpit: credentials response is valid JSON" {
  body=$(api_get /api/services/mailpit/credentials)
  echo "$body" | jq . > /dev/null
}

@test "mailpit: credentials contain MAIL_HOST" {
  body=$(api_get /api/services/mailpit/credentials)
  echo "$body" | jq -r 'keys[]' | grep -q "MAIL_HOST"
}

@test "mailpit: credentials contain MAIL_PORT" {
  body=$(api_get /api/services/mailpit/credentials)
  echo "$body" | jq -r 'keys[]' | grep -q "MAIL_PORT"
}

# ─── Valkey credentials ───────────────────────────────────────────────────────

@test "valkey: GET /api/services/redis/credentials returns 200" {
  status=$(api_status /api/services/redis/credentials)
  [ "$status" -eq 200 ]
}

@test "valkey: credentials response is valid JSON" {
  body=$(api_get /api/services/redis/credentials)
  echo "$body" | jq . > /dev/null
}

@test "valkey: credentials contain REDIS_HOST" {
  body=$(api_get /api/services/redis/credentials)
  echo "$body" | jq -r 'keys[]' | grep -q "REDIS_HOST"
}

@test "valkey: credentials contain REDIS_PASSWORD" {
  body=$(api_get /api/services/redis/credentials)
  echo "$body" | jq -r 'keys[]' | grep -q "REDIS_PASSWORD"
}

# ─── Non-credential services return 404 ───────────────────────────────────────

@test "caddy: GET /api/services/caddy/credentials returns 404 (no credentials)" {
  status=$(api_status /api/services/caddy/credentials)
  [ "$status" -eq 404 ]
}

@test "dns: GET /api/services/dns/credentials returns 404 (no credentials)" {
  status=$(api_status /api/services/dns/credentials)
  [ "$status" -eq 404 ]
}

# ─── Teardown: purge installed services ───────────────────────────────────────

@test "credentials-teardown: purge mailpit" {
  api_sse DELETE /api/services/mailpit 60
  poll_service_field mailpit installed false 10
}

@test "credentials-teardown: purge valkey" {
  api_sse DELETE /api/services/redis 60
  poll_service_field redis installed false 10
}
