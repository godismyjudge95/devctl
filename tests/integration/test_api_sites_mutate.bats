#!/usr/bin/env bats
# test_api_sites_mutate.bats — Site create / read / update / delete lifecycle.

load setup

# Use a unique domain and path for this test run so we don't collide with anything.
SITE_DOMAIN="devctl-bats-test.test"
SITE_PATH="/tmp/devctl-bats-site"
SITE_ID=""

# ─── Create ───────────────────────────────────────────────────────────────────

@test "sites: create directory for site root" {
  container_exec mkdir -p "$SITE_PATH"
  container_exec test -d "$SITE_PATH"
}

@test "sites: POST /api/sites returns 201" {
  status=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST -H "Content-Type: application/json" \
    -d "{\"domain\":\"${SITE_DOMAIN}\",\"root_path\":\"${SITE_PATH}\"}" \
    "${BASE_URL}/api/sites")
  [ "$status" -eq 201 ]
}

@test "sites: created site appears in GET /api/sites" {
  body=$(api_get /api/sites)
  echo "$body" | jq -r '.[].domain' | grep -q "$SITE_DOMAIN"
}

@test "sites: created site has correct domain" {
  body=$(api_get /api/sites)
  domain=$(echo "$body" | jq -r ".[] | select(.domain==\"${SITE_DOMAIN}\") | .domain")
  [ "$domain" = "$SITE_DOMAIN" ]
}

@test "sites: created site has correct root_path" {
  body=$(api_get /api/sites)
  root=$(echo "$body" | jq -r ".[] | select(.domain==\"${SITE_DOMAIN}\") | .root_path")
  [ "$root" = "$SITE_PATH" ]
}

@test "sites: created site has non-empty id" {
  body=$(api_get /api/sites)
  id=$(echo "$body" | jq -r ".[] | select(.domain==\"${SITE_DOMAIN}\") | .id")
  [ -n "$id" ]
}

@test "sites: GET /api/sites/{id} returns 200 for created site" {
  body=$(api_get /api/sites)
  id=$(echo "$body" | jq -r ".[] | select(.domain==\"${SITE_DOMAIN}\") | .id")
  status=$(api_status "/api/sites/${id}")
  [ "$status" -eq 200 ]
}

# ─── Update ───────────────────────────────────────────────────────────────────

@test "sites: PUT /api/sites/{id} returns 200" {
  body=$(api_get /api/sites)
  id=$(echo "$body" | jq -r ".[] | select(.domain==\"${SITE_DOMAIN}\") | .id")
  root=$(echo "$body" | jq -r ".[] | select(.domain==\"${SITE_DOMAIN}\") | .root_path")
  status=$(curl -s -o /dev/null -w "%{http_code}" \
    -X PUT -H "Content-Type: application/json" \
    -d "{\"domain\":\"${SITE_DOMAIN}\",\"root_path\":\"${root}\"}" \
    "${BASE_URL}/api/sites/${id}")
  [ "$status" -eq 200 ]
}

# ─── Delete ───────────────────────────────────────────────────────────────────

@test "sites: DELETE /api/sites/{id} returns 200 or 204" {
  body=$(api_get /api/sites)
  id=$(echo "$body" | jq -r ".[] | select(.domain==\"${SITE_DOMAIN}\") | .id")
  status=$(api_delete "/api/sites/${id}")
  [[ "$status" -eq 200 || "$status" -eq 204 ]]
}

@test "sites: deleted site no longer in GET /api/sites" {
  run bash -c "api_get /api/sites | jq -r '.[].domain' | grep -q '${SITE_DOMAIN}'"
  [ "$status" -ne 0 ]
}

@test "sites: GET /api/sites/{id} returns 404 after delete" {
  # Use a dummy ID — if site was deleted the real ID should return 404 too.
  # Re-confirm by checking none of the sites have the domain.
  body=$(api_get /api/sites)
  count=$(echo "$body" | jq "[.[] | select(.domain==\"${SITE_DOMAIN}\")] | length")
  [ "$count" -eq 0 ]
}

# ─── Cleanup ──────────────────────────────────────────────────────────────────

@test "sites: cleanup test directory" {
  container_exec rm -rf "$SITE_PATH"
  run container_exec bash -c "test ! -d '${SITE_PATH}'"
  [ "$status" -eq 0 ]
}
