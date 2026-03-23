#!/usr/bin/env bats
# test_api_php.bats — Verify GET /api/php/versions and /api/php/settings.

load setup

@test "GET /api/php/versions returns HTTP 200" {
  status=$(api_status /api/php/versions)
  [ "$status" -eq 200 ]
}

@test "GET /api/php/versions response is a valid JSON array" {
  result=$(api_get /api/php/versions)
  echo "$result" | jq -e '. | type == "array"' > /dev/null
}

@test "GET /api/php/settings returns HTTP 200" {
  status=$(api_status /api/php/settings)
  [ "$status" -eq 200 ]
}

@test "PHP settings response contains memory_limit key" {
  result=$(api_get /api/php/settings)
  assert_json_field "$result" "memory_limit"
}

@test "PHP settings response contains max_execution_time key" {
  result=$(api_get /api/php/settings)
  assert_json_field "$result" "max_execution_time"
}

@test "PHP settings response contains upload_max_filesize key" {
  result=$(api_get /api/php/settings)
  assert_json_field "$result" "upload_max_filesize"
}
