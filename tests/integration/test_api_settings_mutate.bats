#!/usr/bin/env bats
# test_api_settings_mutate.bats — Settings PUT/GET round-trip tests.
# Verifies that writing a setting via PUT /api/settings persists and is
# returned correctly by GET /api/settings/resolved.

load setup

# ─── Save original values so we can restore them ─────────────────────────────

@test "settings: GET /api/settings/resolved returns current dns_tld" {
  body=$(api_get /api/settings/resolved)
  tld=$(echo "$body" | jq -r '.dns_tld')
  [ -n "$tld" ]
}

# ─── Mutate: change dns_tld ───────────────────────────────────────────────────

@test "settings: PUT /api/settings with dns_tld=batstest returns 200" {
  status=$(api_put_status /api/settings '{"dns_tld":"batstest"}')
  [ "$status" -eq 200 ]
}

@test "settings: GET /api/settings/resolved reflects updated dns_tld" {
  body=$(api_get /api/settings/resolved)
  tld=$(echo "$body" | jq -r '.dns_tld')
  [ "$tld" = "batstest" ]
}

@test "settings: GET /api/settings contains updated dns_tld" {
  body=$(api_get /api/settings)
  tld=$(echo "$body" | jq -r '.dns_tld')
  [ "$tld" = "batstest" ]
}

# ─── Mutate: change devctl_port ───────────────────────────────────────────────
# NOTE: We deliberately do NOT change devctl_port here because it would
# break the running service. We test that the endpoint accepts other keys.

@test "settings: PUT /api/settings can update multiple keys at once" {
  status=$(api_put_status /api/settings '{"dns_tld":"batstest","service_poll_interval":"3000"}')
  [ "$status" -eq 200 ]
}

@test "settings: GET /api/settings/resolved reflects service_poll_interval update" {
  body=$(api_get /api/settings/resolved)
  interval=$(echo "$body" | jq -r '.service_poll_interval')
  [ "$interval" = "3000" ]
}

# ─── Restore original values ──────────────────────────────────────────────────

@test "settings: restore dns_tld to test" {
  status=$(api_put_status /api/settings '{"dns_tld":"test","service_poll_interval":"5000"}')
  [ "$status" -eq 200 ]
}

@test "settings: dns_tld is restored to test" {
  body=$(api_get /api/settings/resolved)
  tld=$(echo "$body" | jq -r '.dns_tld')
  [ "$tld" = "test" ]
}
