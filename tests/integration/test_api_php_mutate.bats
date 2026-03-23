#!/usr/bin/env bats
# test_api_php_mutate.bats — PHP settings PUT/GET round-trip tests.
# Verifies that writing PHP ini settings via PUT /api/php/settings persists
# and is returned correctly by GET /api/php/settings.

load setup

# ─── Read original values ─────────────────────────────────────────────────────

@test "php/settings: GET /api/php/settings returns 200" {
  status=$(api_status /api/php/settings)
  [ "$status" -eq 200 ]
}

@test "php/settings: response includes memory_limit" {
  body=$(api_get /api/php/settings)
  val=$(echo "$body" | jq -r '.memory_limit')
  [ -n "$val" ]
}

@test "php/settings: response includes max_execution_time" {
  body=$(api_get /api/php/settings)
  val=$(echo "$body" | jq -r '.max_execution_time')
  [ -n "$val" ]
}

@test "php/settings: response includes upload_max_filesize" {
  body=$(api_get /api/php/settings)
  val=$(echo "$body" | jq -r '.upload_max_filesize')
  [ -n "$val" ]
}

# ─── Mutate: update memory_limit ─────────────────────────────────────────────

@test "php/settings: PUT /api/php/settings with memory_limit=256M returns 200" {
  status=$(api_put_status /api/php/settings '{"memory_limit":"256M"}')
  [ "$status" -eq 200 ]
}

@test "php/settings: GET /api/php/settings reflects updated memory_limit" {
  body=$(api_get /api/php/settings)
  val=$(echo "$body" | jq -r '.memory_limit')
  [ "$val" = "256M" ]
}

# ─── Mutate: update max_execution_time ───────────────────────────────────────

@test "php/settings: PUT /api/php/settings with max_execution_time=60 returns 200" {
  status=$(api_put_status /api/php/settings '{"max_execution_time":"60"}')
  [ "$status" -eq 200 ]
}

@test "php/settings: GET /api/php/settings reflects updated max_execution_time" {
  body=$(api_get /api/php/settings)
  val=$(echo "$body" | jq -r '.max_execution_time')
  [ "$val" = "60" ]
}

# ─── Restore defaults ─────────────────────────────────────────────────────────

@test "php/settings: restore memory_limit to 128M" {
  status=$(api_put_status /api/php/settings '{"memory_limit":"128M","max_execution_time":"30"}')
  [ "$status" -eq 200 ]
}

@test "php/settings: memory_limit restored to 128M" {
  body=$(api_get /api/php/settings)
  val=$(echo "$body" | jq -r '.memory_limit')
  [ "$val" = "128M" ]
}
