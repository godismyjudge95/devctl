#!/usr/bin/env bats
# test_api_services.bats — Verify the /api/services endpoint shape and the
# /api/services/events SSE stream.

load setup

@test "GET /api/services returns HTTP 200" {
  status=$(api_status /api/services)
  [ "$status" -eq 200 ]
}

@test "GET /api/services response is a valid JSON array" {
  result=$(api_get /api/services)
  echo "$result" | jq -e '. | type == "array"' > /dev/null
}

@test "GET /api/services response contains at least one element" {
  result=$(api_get /api/services)
  count=$(echo "$result" | jq '. | length')
  [ "$count" -gt 0 ]
}

@test "first service element has an id field (string)" {
  result=$(api_get /api/services)
  id=$(echo "$result" | jq -r '.[0].id')
  [ -n "$id" ]
  [ "$id" != "null" ]
}

@test "first service element has a label field (string)" {
  result=$(api_get /api/services)
  label=$(echo "$result" | jq -r '.[0].label')
  [ -n "$label" ]
  [ "$label" != "null" ]
}

@test "first service element has a status field" {
  result=$(api_get /api/services)
  assert_json_field "$result" "[0].status"
}

@test "first service element has an installed field (boolean)" {
  result=$(api_get /api/services)
  installed=$(echo "$result" | jq '.[0].installed | type')
  [ "$installed" = '"boolean"' ]
}

@test "first service element has a required field (boolean)" {
  result=$(api_get /api/services)
  required=$(echo "$result" | jq '.[0].required | type')
  [ "$required" = '"boolean"' ]
}

@test "at least one service has required set to true" {
  result=$(api_get /api/services)
  count=$(echo "$result" | jq '[.[] | select(.required == true)] | length')
  [ "$count" -gt 0 ]
}

@test "GET /api/services/events returns HTTP 200 with text/event-stream content type" {
  # Use a short max-time so curl does not hang on the persistent SSE stream.
  response=$(curl -s -o /dev/null -w "%{http_code} %{content_type}" \
    --max-time 2 "${BASE_URL}/api/services/events" 2>/dev/null || true)
  http_code=$(echo "$response" | awk '{print $1}')
  content_type=$(echo "$response" | awk '{print $2}')
  [ "$http_code" -eq 200 ]
  [[ "$content_type" == *"text/event-stream"* ]]
}
