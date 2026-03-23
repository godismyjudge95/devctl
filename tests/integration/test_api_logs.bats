#!/usr/bin/env bats
# test_api_logs.bats — Verify GET /api/logs (log file list) and
# GET /api/logs/{id}/tail.
#
# Note: The LogFileInfo struct has fields: id, name, path, size.
# On a fresh container the logs directory may be empty (empty array is OK).

load setup

@test "GET /api/logs returns HTTP 200" {
  status=$(api_status /api/logs)
  [ "$status" -eq 200 ]
}

@test "GET /api/logs response is a valid JSON array" {
  result=$(api_get /api/logs)
  echo "$result" | jq -e '. | type == "array"' > /dev/null
}

@test "each log entry has an id field (when logs are present)" {
  result=$(api_get /api/logs)
  count=$(echo "$result" | jq '. | length')
  if [ "$count" -gt 0 ]; then
    # Verify every entry has a non-null id string.
    bad=$(echo "$result" | jq '[.[] | select(.id == null or .id == "")] | length')
    [ "$bad" -eq 0 ]
  fi
}

@test "each log entry has a name field (when logs are present)" {
  result=$(api_get /api/logs)
  count=$(echo "$result" | jq '. | length')
  if [ "$count" -gt 0 ]; then
    bad=$(echo "$result" | jq '[.[] | select(.name == null or .name == "")] | length')
    [ "$bad" -eq 0 ]
  fi
}

@test "GET /api/logs/caddy/tail returns 200 or 404 (caddy may not be installed)" {
  status=$(api_status /api/logs/caddy/tail)
  [ "$status" -eq 200 ] || [ "$status" -eq 404 ]
}
