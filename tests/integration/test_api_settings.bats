#!/usr/bin/env bats
# test_api_settings.bats — Verify GET /api/settings and /api/settings/resolved.

load setup

@test "GET /api/settings returns HTTP 200" {
  status=$(api_status /api/settings)
  [ "$status" -eq 200 ]
}

@test "GET /api/settings response is a valid JSON object" {
  result=$(api_get /api/settings)
  echo "$result" | jq -e '. | type == "object"' > /dev/null
}

@test "GET /api/settings/resolved returns HTTP 200" {
  status=$(api_status /api/settings/resolved)
  [ "$status" -eq 200 ]
}

@test "resolved settings response contains devctl_port key" {
  result=$(api_get /api/settings/resolved)
  assert_json_field "$result" "devctl_port"
}

@test "resolved settings devctl_port value is 4000" {
  result=$(api_get /api/settings/resolved)
  port=$(echo "$result" | jq -r '.devctl_port')
  [ "$port" = "4000" ]
}

@test "resolved settings response contains devctl_host key" {
  result=$(api_get /api/settings/resolved)
  assert_json_field "$result" "devctl_host"
}

@test "resolved settings response contains dns_tld key" {
  result=$(api_get /api/settings/resolved)
  assert_json_field "$result" "dns_tld"
}
