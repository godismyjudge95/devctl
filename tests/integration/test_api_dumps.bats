#!/usr/bin/env bats
# test_api_dumps.bats — Verify GET /api/dumps and DELETE /api/dumps.

load setup

@test "GET /api/dumps returns HTTP 200" {
  status=$(api_status /api/dumps)
  [ "$status" -eq 200 ]
}

@test "GET /api/dumps response is a valid JSON array" {
  result=$(api_get /api/dumps)
  echo "$result" | jq -e '. | type == "array"' > /dev/null
}

@test "GET /api/dumps empty array is acceptable on a fresh container" {
  result=$(api_get /api/dumps)
  count=$(echo "$result" | jq '. | length')
  [ "$count" -ge 0 ]
}

@test "DELETE /api/dumps returns HTTP 200 or 204" {
  status=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "${BASE_URL}/api/dumps")
  [ "$status" -eq 200 ] || [ "$status" -eq 204 ]
}
