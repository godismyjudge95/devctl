#!/usr/bin/env bats
# test_api_sites.bats — Verify the /api/sites and /api/sites/detect endpoints.

load setup

@test "GET /api/sites returns HTTP 200" {
  status=$(api_status /api/sites)
  [ "$status" -eq 200 ]
}

@test "GET /api/sites response is a valid JSON array" {
  result=$(api_get /api/sites)
  echo "$result" | jq -e '. | type == "array"' > /dev/null
}

@test "GET /api/sites empty array is acceptable (fresh container has no sites)" {
  result=$(api_get /api/sites)
  # Must parse as array — length 0 is fine.
  count=$(echo "$result" | jq '. | length')
  [ "$count" -ge 0 ]
}

@test "GET /api/sites/detect with root_path returns HTTP 200" {
  status=$(api_status "/api/sites/detect?root_path=/tmp")
  [ "$status" -eq 200 ]
}

@test "GET /api/sites/detect without root_path returns HTTP 400" {
  status=$(api_status /api/sites/detect)
  [ "$status" -eq 400 ]
}
