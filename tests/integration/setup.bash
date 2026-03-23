# setup.bash — shared helpers for all devctl BATS integration tests.
# Sourced automatically by each .bats file via `load setup`.
#
# These tests run from the HOST against the Incus container.
# $CONTAINER is set by test-env.sh; API calls go to BASE_URL (proxied from host).
# container_exec() forwards shell commands into the container via `incus exec`.

CONTAINER="${DEVCTL_CONTAINER:-}"
BASE_URL="${DEVCTL_BASE_URL:-http://127.0.0.1:4000}"
# Server root inside the container (matches the devctl.service unit written by test-env.sh).
SERVER_ROOT="/home/testuser/ddev/sites/server"

# ─── container_exec ────────────────────────────────────────────────────────────
# Run a command inside the Incus container.
# When DEVCTL_CONTAINER is set the tests run on the host and commands are
# forwarded into the container via `incus exec`.  When BATS itself is executed
# inside the container (legacy mode) this is a passthrough.
container_exec() {
  if [ -n "$CONTAINER" ]; then
    incus exec "$CONTAINER" -- "$@"
  else
    "$@"
  fi
}

# ─── HTTP helpers ──────────────────────────────────────────────────────────────

# api_get — HTTP GET; fails (non-zero exit) on any non-2xx status.
# Usage: api_get /api/services
api_get() {
  curl -sf "${BASE_URL}${1}"
}

# api_status — Returns just the HTTP status code as plain text.
# Usage: status=$(api_status /api/nonexistent)
api_status() {
  curl -s -o /dev/null -w "%{http_code}" "${BASE_URL}${1}"
}

# api_post — HTTP POST with a JSON body; returns the response body.
# A non-2xx response causes a non-zero exit.
# Usage: api_post /api/services/mailpit/start ""
#        api_post /api/sites '{"domain":"x.test","root_path":"/tmp/x"}'
api_post() {
  local path="$1"
  local body="${2:-}"
  if [[ -n "$body" ]]; then
    curl -sf -X POST -H "Content-Type: application/json" -d "$body" "${BASE_URL}${path}"
  else
    curl -sf -X POST "${BASE_URL}${path}"
  fi
}

# api_post_status — Like api_post but returns the HTTP status code instead of body.
api_post_status() {
  local path="$1"
  local body="${2:-}"
  if [[ -n "$body" ]]; then
    curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" -d "$body" "${BASE_URL}${path}"
  else
    curl -s -o /dev/null -w "%{http_code}" -X POST "${BASE_URL}${path}"
  fi
}

# api_put — HTTP PUT with a JSON body; returns the response body.
# Usage: api_put /api/settings '{"dns_tld":"local"}'
api_put() {
  local path="$1"
  local body="${2:-}"
  curl -sf -X PUT -H "Content-Type: application/json" -d "$body" "${BASE_URL}${path}"
}

# api_put_status — Like api_put but returns the HTTP status code.
api_put_status() {
  local path="$1"
  local body="${2:-}"
  curl -s -o /dev/null -w "%{http_code}" -X PUT -H "Content-Type: application/json" -d "$body" "${BASE_URL}${path}"
}

# api_delete — HTTP DELETE; returns the HTTP status code.
# Usage: api_delete /api/sites/abc123
api_delete() {
  curl -s -o /dev/null -w "%{http_code}" -X DELETE "${BASE_URL}${1}"
}

# api_delete_body — HTTP DELETE; returns the response body (fails on non-2xx).
api_delete_body() {
  curl -sf -X DELETE "${BASE_URL}${1}"
}

# api_sse — Stream an SSE endpoint (POST or DELETE) and wait for event: done or event: error.
# Returns 0 if a "done" event is received; 1 if an "error" event is received or timeout.
# Usage: api_sse POST /api/services/mailpit/install
#        api_sse DELETE /api/services/mailpit
api_sse() {
  local method="$1"
  local path="$2"
  local timeout="${3:-300}"  # seconds, default 5 minutes
  local result=1
  local response
  response=$(curl -sf -X "$method" --max-time "$timeout" --no-buffer "${BASE_URL}${path}" 2>/dev/null || true)
  if echo "$response" | grep -q "^event: done"; then
    result=0
  elif echo "$response" | grep -q "^event: error"; then
    result=1
    # Print the error data for debugging
    echo "$response" | grep "^data:" | tail -5 >&2 || true
  fi
  return "$result"
}

# ─── Polling helpers ───────────────────────────────────────────────────────────

# poll_service_status — Poll GET /api/services until the named service reaches
# the expected status, or until the timeout (seconds) is exceeded.
# Usage: poll_service_status mailpit running 30
poll_service_status() {
  local id="$1"
  local want="$2"
  local timeout="${3:-30}"
  local elapsed=0
  while true; do
    local got
    got=$(api_get /api/services 2>/dev/null | jq -r ".[] | select(.id==\"${id}\") | .status" 2>/dev/null || echo "")
    if [[ "$got" == "$want" ]]; then
      return 0
    fi
    if [[ $elapsed -ge $timeout ]]; then
      echo "poll_service_status: timed out waiting for ${id} status=${want} (last: ${got})" >&2
      return 1
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
}

# poll_service_field — Poll GET /api/services until the named service's field
# equals the expected value, or timeout.
# Usage: poll_service_field mailpit installed true 10
poll_service_field() {
  local id="$1"
  local field="$2"
  local want="$3"
  local timeout="${4:-15}"
  local elapsed=0
  while true; do
    local got
    got=$(api_get /api/services 2>/dev/null | jq -r ".[] | select(.id==\"${id}\") | .${field}" 2>/dev/null || echo "")
    if [[ "$got" == "$want" ]]; then
      return 0
    fi
    if [[ $elapsed -ge $timeout ]]; then
      echo "poll_service_field: timed out waiting for ${id}.${field}=${want} (last: ${got})" >&2
      return 1
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
}

# ─── JSON helpers ──────────────────────────────────────────────────────────────

# assert_json_field — Asserts that a JSON string contains a key that is not null.
# Works correctly for boolean false values (jq -e exits 1 for false).
# Usage: assert_json_field "$json" "field_name"
assert_json_field() {
  echo "$1" | jq -e ".$2 != null" > /dev/null
}
