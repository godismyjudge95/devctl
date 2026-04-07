#!/usr/bin/env bash
# scripts/demo.sh — Creates a fresh devctl-demo Incus container with realistic
# seed data and takes dashboard screenshots for every page of the README.
#
# Usage:
#   bash scripts/demo.sh                   # full setup + screenshots
#   bash scripts/demo.sh --screenshots-only # skip setup, just re-run screenshots
#
# Prerequisites:
#   make build                              # binary must be built first
#   make test-env-setup                     # bake devctl-ubuntu-base image
#   make test-artifacts-download            # optional: cache service binaries
#   cd scripts && npm install               # puppeteer (first run only)
#
# Container: devctl-demo — destroyed and recreated on every full run.
# Dashboard forwarded to: http://127.0.0.1:4001
# WhoDB     forwarded to: http://127.0.0.1:8161 (must match WhoDBView.vue hardcode)

set -euo pipefail

CONTAINER="devctl-demo"
SERVER_ROOT="/home/testuser/ddev/sites/server"
SITES_ROOT="/home/testuser/ddev/sites"
DEMO_PORT="4001"   # host port → container:4000
WHODB_PORT="8161"  # host port → container:8161 (iframe in WhoDBView.vue hardcodes this)

# ─── Colour helpers ────────────────────────────────────────────────────────────
if [ -t 1 ] && command -v tput &>/dev/null && tput colors &>/dev/null && [ "$(tput colors)" -ge 8 ]; then
  GREEN="$(tput setaf 2)"; RED="$(tput setaf 1)"; CYAN="$(tput setaf 6)"
  BOLD="$(tput bold)"; RESET="$(tput sgr0)"
else
  GREEN="" RED="" CYAN="" BOLD="" RESET=""
fi

info()    { printf '%s→ %s%s\n' "${CYAN}"  "$*" "${RESET}"; }
success() { printf '%s✓ %s%s\n' "${GREEN}" "$*" "${RESET}"; }
error()   { printf '%s✗ %s%s\n' "${RED}"   "$*" "${RESET}" >&2; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# ─── --screenshots-only mode ───────────────────────────────────────────────────
for arg in "$@"; do
  if [[ "$arg" == "--screenshots-only" ]]; then
    if ! incus list --format=csv --columns=n 2>/dev/null | grep -q "^${CONTAINER}$"; then
      error "Container '${CONTAINER}' not found. Run without --screenshots-only first."
      exit 1
    fi
    info "Re-running screenshots against existing container..."
    [[ ! -d "${SCRIPT_DIR}/node_modules" ]] && (cd "$SCRIPT_DIR" && npm install)
    BASE_URL="http://127.0.0.1:${DEMO_PORT}" node "${SCRIPT_DIR}/screenshots.js"
    success "Screenshots updated."
    exit 0
  fi
done

# ─── Prerequisites ─────────────────────────────────────────────────────────────
command -v incus &>/dev/null || { error "Incus is not installed."; exit 1; }
[[ -f "./devctl" ]] || { error "Binary ./devctl not found — run 'make build' first."; exit 1; }

# ─── Destroy existing demo container ──────────────────────────────────────────
if incus list --format=csv --columns=n 2>/dev/null | grep -q "^${CONTAINER}$"; then
  info "Destroying existing container '${CONTAINER}'..."
  incus delete --force "$CONTAINER"
  success "Container destroyed."
fi

# ─── Launch fresh container ───────────────────────────────────────────────────
info "Launching container '${CONTAINER}'..."
if incus launch devctl-ubuntu-base "$CONTAINER" 2>/dev/null; then
  success "Container launched from cached image 'devctl-ubuntu-base'."
else
  info "Cached image not found — falling back to images:ubuntu/24.04..."
  incus launch images:ubuntu/24.04 "$CONTAINER"
  success "Container launched."
fi

# ─── Wait for systemd ─────────────────────────────────────────────────────────
info "Waiting for systemd..."
TIMEOUT=30; ELAPSED=0
while true; do
  STATE="$(incus exec "$CONTAINER" -- systemctl is-system-running 2>/dev/null || true)"
  [[ "$STATE" == "running" || "$STATE" == "degraded" ]] && break
  [[ $ELAPSED -ge $TIMEOUT ]] && { error "Timed out waiting for systemd."; exit 1; }
  sleep 1; ELAPSED=$((ELAPSED + 1))
done
success "Systemd ready."

# ─── Push binary ──────────────────────────────────────────────────────────────
info "Pushing devctl binary..."
incus file push ./devctl "$CONTAINER/usr/local/bin/devctl"
incus exec "$CONTAINER" -- chmod 755 /usr/local/bin/devctl
success "Binary pushed."

# ─── Create testuser + site directories ───────────────────────────────────────
info "Creating testuser and directories..."
incus exec "$CONTAINER" -- useradd -m testuser
incus exec "$CONTAINER" -- mkdir -p "$SERVER_ROOT"
incus exec "$CONTAINER" -- chown -R testuser:testuser /home/testuser/ddev
success "testuser created."

# ─── Mount artifact cache + curl shim ─────────────────────────────────────────
ARTIFACTS_POOL="default"
ARTIFACTS_VOLUME="devctl-test-artifacts"
ARTIFACTS_MOUNT="/var/cache/devctl-artifacts"

if incus storage volume show "$ARTIFACTS_POOL" "$ARTIFACTS_VOLUME" &>/dev/null; then
  info "Mounting artifact cache..."
  incus config device add "$CONTAINER" artifacts disk \
    pool="$ARTIFACTS_POOL" source="$ARTIFACTS_VOLUME" path="$ARTIFACTS_MOUNT"
  success "Artifact cache mounted at ${ARTIFACTS_MOUNT}."

  info "Installing curl cache shim..."
  incus exec "$CONTAINER" -- tee /usr/local/bin/curl >/dev/null <<'CURLSHIM'
#!/bin/bash
CACHE_DIR="/var/cache/devctl-artifacts"
REAL_CURL="/usr/bin/curl"
DEST="" PREV=""
for arg in "$@"; do
  [[ "$PREV" == "-o" ]] && DEST="$arg"
  PREV="$arg"
done
if [[ -n "$DEST" ]]; then
  CACHED="${CACHE_DIR}/$(basename "$DEST")"
  if [[ -f "$CACHED" ]]; then
    cp "$CACHED" "$DEST"
    echo "curl-shim: served $(basename "$DEST") from cache" >&2
    exit 0
  fi
fi
exec "$REAL_CURL" "$@"
CURLSHIM
  incus exec "$CONTAINER" -- chmod 755 /usr/local/bin/curl
  success "curl shim installed."
else
  info "No artifact cache — installs will download from the internet."
fi

# ─── Create demo site directories with framework markers ──────────────────────
info "Creating demo site directories..."

# Laravel — detected by presence of 'artisan' file
incus exec "$CONTAINER" -- bash -c "
  mkdir -p '${SITES_ROOT}/laravel.test/public'
  touch '${SITES_ROOT}/laravel.test/artisan'
  echo '<?php' > '${SITES_ROOT}/laravel.test/public/index.php'
  chown -R testuser:testuser '${SITES_ROOT}/laravel.test'
"

# Statamic — detected by vendor/statamic directory
incus exec "$CONTAINER" -- bash -c "
  mkdir -p '${SITES_ROOT}/statamic.test/vendor/statamic' '${SITES_ROOT}/statamic.test/public'
  echo '<?php' > '${SITES_ROOT}/statamic.test/public/index.php'
  chown -R testuser:testuser '${SITES_ROOT}/statamic.test'
"

# WordPress — detected by wp-config.php
incus exec "$CONTAINER" -- bash -c "
  mkdir -p '${SITES_ROOT}/wordpress.test'
  touch '${SITES_ROOT}/wordpress.test/wp-config.php'
  chown -R testuser:testuser '${SITES_ROOT}/wordpress.test'
"

success "Site directories created (laravel.test, statamic.test, wordpress.test)."

# ─── Write devctl service unit ─────────────────────────────────────────────────
info "Writing devctl.service..."
incus exec "$CONTAINER" -- tee /etc/systemd/system/devctl.service >/dev/null <<EOF
[Unit]
Description=devctl — Local PHP Dev Dashboard
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/devctl daemon
Restart=on-failure
RestartSec=5s
Environment=HOME=/home/testuser
Environment=DEVCTL_SITE_USER=testuser
Environment=DEVCTL_SERVER_ROOT=${SERVER_ROOT}
Environment=DEVCTL_TESTING=true

[Install]
WantedBy=multi-user.target
EOF
incus exec "$CONTAINER" -- systemctl daemon-reload
incus exec "$CONTAINER" -- systemctl enable devctl
incus exec "$CONTAINER" -- systemctl start devctl
success "devctl service started."

# ─── Wait for devctl HTTP ─────────────────────────────────────────────────────
info "Waiting for devctl HTTP API..."
TIMEOUT=90; ELAPSED=0
while true; do
  if incus exec "$CONTAINER" -- curl -sf http://127.0.0.1:4000/api/settings/resolved >/dev/null 2>&1; then
    success "devctl responding on http://127.0.0.1:4000."
    break
  fi
  [[ $ELAPSED -ge $TIMEOUT ]] && {
    error "Timed out waiting for devctl HTTP API."
    incus exec "$CONTAINER" -- journalctl -u devctl -n 20 --no-pager || true
    exit 1
  }
  sleep 1; ELAPSED=$((ELAPSED + 1))
done

# ─── Helper: install a service via the SSE API ────────────────────────────────
install_service() {
  local svc="$1" label="$2" timeout="${3:-300}"
  info "Installing ${label}..."
  # Fetch the pinned install_version from the service definition so the
  # installer can skip the GitHub API version check (api.github.com may
  # be unreachable from inside the Incus container).
  local ver
  ver=$(incus exec "$CONTAINER" -- \
    sh -c "curl -sf http://127.0.0.1:4000/api/services | jq -r '.[] | select(.id==\"${svc}\") | .install_version'" 2>/dev/null || true)
  local url="http://127.0.0.1:4000/api/services/${svc}/install"
  [[ -n "$ver" && "$ver" != "null" ]] && url="${url}?version=${ver}"
  RESPONSE=$(incus exec "$CONTAINER" -- \
    curl -sf -X POST --max-time "$timeout" --no-buffer "$url" 2>/dev/null || true)
  if echo "$RESPONSE" | grep -q "^event: done"; then
    success "${label} installed."
  elif echo "$RESPONSE" | grep -q "^event: error"; then
    error "${label} install failed:"
    echo "$RESPONSE" | grep "^data:" | tail -5 >&2
    exit 1
  else
    local installed
    installed=$(incus exec "$CONTAINER" -- \
      sh -c "curl -sf http://127.0.0.1:4000/api/services | jq -r '.[] | select(.id==\"${svc}\") | .installed'" 2>/dev/null || echo "unknown")
    if [[ "$installed" == "true" ]]; then
      success "${label} already installed."
    else
      error "${label} install returned unexpected response: ${RESPONSE:0:200}"
      exit 1
    fi
  fi
}

# ─── Helper: wait for a service to reach running state ────────────────────────
wait_running() {
  local svc="$1" label="$2" timeout="${3:-60}"
  local elapsed=0
  while true; do
    local status
    status=$(incus exec "$CONTAINER" -- \
      sh -c "curl -sf http://127.0.0.1:4000/api/services | jq -r '.[] | select(.id==\"${svc}\") | .status'" 2>/dev/null || echo "")
    [[ "$status" == "running" ]] && { success "${label} is running."; return 0; }
    [[ $elapsed -ge $timeout ]] && { error "Timed out waiting for ${label} (last: ${status})."; return 1; }
    sleep 2; elapsed=$((elapsed + 2))
  done
}

# ─── Install Caddy (required) ─────────────────────────────────────────────────
install_service "caddy" "Caddy" 120
wait_running    "caddy" "Caddy" 30

# ─── Install PHP 8.3 ──────────────────────────────────────────────────────────
info "Installing PHP 8.3..."
PHP_STATUS=$(incus exec "$CONTAINER" -- \
  curl -s -o /dev/null -w "%{http_code}" -X POST --max-time 600 \
  http://127.0.0.1:4000/api/php/versions/8.3/install 2>/dev/null || echo "000")
if [[ "$PHP_STATUS" == "200" ]]; then
  success "PHP 8.3 installed."
elif [[ "$PHP_STATUS" == "000" ]]; then
  error "PHP 8.3 install timed out."
  exit 1
else
  error "PHP 8.3 install returned HTTP ${PHP_STATUS}."
  exit 1
fi

# ─── Create demo sites via API ────────────────────────────────────────────────
info "Creating demo sites..."

create_site() {
  local domain="$1" root="$2"
  local result
  result=$(incus exec "$CONTAINER" -- \
    curl -sf -X POST -H "Content-Type: application/json" \
    -d "{\"domain\":\"${domain}\",\"root_path\":\"${root}\"}" \
    http://127.0.0.1:4000/api/sites 2>/dev/null || true)
  if echo "$result" | grep -q '"id"'; then
    success "Site ${domain} created."
  else
    error "Failed to create site ${domain}: ${result:0:200}"
    exit 1
  fi
}

create_site "laravel.test"   "${SITES_ROOT}/laravel.test"
create_site "statamic.test"  "${SITES_ROOT}/statamic.test"
create_site "wordpress.test" "${SITES_ROOT}/wordpress.test"

# ─── Install optional services ────────────────────────────────────────────────
# Running: Valkey/Redis, Mailpit, MySQL, Meilisearch, WhoDB, MaxIO
# Not installed (shown as installable): PostgreSQL, Typesense, Reverb

install_service "redis"       "Valkey/Redis"  300
wait_running    "redis"       "Valkey/Redis"  30

install_service "mailpit"     "Mailpit"       180
wait_running    "mailpit"     "Mailpit"       30

install_service "mysql"       "MySQL"         600
wait_running    "mysql"       "MySQL"         90

install_service "meilisearch" "Meilisearch"   300
wait_running    "meilisearch" "Meilisearch"   30

install_service "whodb"       "WhoDB"         300
wait_running    "whodb"       "WhoDB"         120

install_service "maxio"       "MaxIO"         300
wait_running    "maxio"       "MaxIO"         30

# ─── Seed demo data ───────────────────────────────────────────────────────────
info "Seeding demo data (dumps, mail, SPX profiles, MaxIO files)..."

incus exec "$CONTAINER" -- python3 - <<'SEED_SCRIPT'
import base64, gzip, io, json, os, socket, time
import urllib.request, urllib.error

API = "http://127.0.0.1:4000"
SERVER_ROOT = "/home/testuser/ddev/sites/server"

# ── TCP Dumps ─────────────────────────────────────────────────────────────────
print("  Seeding PHP dumps via TCP...")
dumps = [
    {
        "timestamp": time.time() - 300,
        "source": {
            "file": "/home/testuser/ddev/sites/laravel.test/app/Http/Controllers/HomeController.php",
            "line": 42, "name": "HomeController.php",
        },
        "host": "laravel.test",
        "nodes": [
            {"type": "string", "value": "Hello from Laravel!", "length": 19, "binary": False, "truncated": 0},
        ],
    },
    {
        "timestamp": time.time() - 250,
        "source": {
            "file": "/home/testuser/ddev/sites/laravel.test/app/Models/User.php",
            "line": 87, "name": "User.php",
        },
        "host": "laravel.test",
        "nodes": [
            {
                "type": "array", "count": 3, "indexed": False, "truncated": 0,
                "children": [
                    {"key": {"type": "string", "value": "id",    "length": 2, "binary": False, "truncated": 0}, "value": {"type": "scalar", "kind": "int",    "value": 1}},
                    {"key": {"type": "string", "value": "name",  "length": 4, "binary": False, "truncated": 0}, "value": {"type": "string", "value": "Alice Martin", "length": 12, "binary": False, "truncated": 0}},
                    {"key": {"type": "string", "value": "email", "length": 5, "binary": False, "truncated": 0}, "value": {"type": "string", "value": "alice@laravel.test", "length": 18, "binary": False, "truncated": 0}},
                ],
            },
        ],
    },
    {
        "timestamp": time.time() - 200,
        "source": {
            "file": "/home/testuser/ddev/sites/statamic.test/app/Tags/NavTag.php",
            "line": 23, "name": "NavTag.php",
        },
        "host": "statamic.test",
        "nodes": [
            {
                "type": "object", "class": "Illuminate\\Support\\Collection", "truncated": 0,
                "children": [
                    {
                        "visibility": "protected", "name": "items",
                        "value": {
                            "type": "array", "count": 3, "indexed": True, "truncated": 0,
                            "children": [
                                {"key": {"type": "scalar", "kind": "int", "value": 0}, "value": {"type": "string", "value": "Home",    "length": 4, "binary": False, "truncated": 0}},
                                {"key": {"type": "scalar", "kind": "int", "value": 1}, "value": {"type": "string", "value": "Blog",    "length": 4, "binary": False, "truncated": 0}},
                                {"key": {"type": "scalar", "kind": "int", "value": 2}, "value": {"type": "string", "value": "Contact", "length": 7, "binary": False, "truncated": 0}},
                            ],
                        },
                    },
                ],
            },
        ],
    },
    {
        "timestamp": time.time() - 150,
        "source": {
            "file": "/home/testuser/ddev/sites/wordpress.test/wp-includes/class-wp-query.php",
            "line": 3721, "name": "class-wp-query.php",
        },
        "host": "wordpress.test",
        "nodes": [
            {"type": "scalar", "kind": "bool", "value": True},
        ],
    },
    {
        "timestamp": time.time() - 100,
        "source": {
            "file": "/home/testuser/ddev/sites/laravel.test/app/Services/PaymentService.php",
            "line": 156, "name": "PaymentService.php",
        },
        "host": "laravel.test",
        "nodes": [
            {
                "type": "array", "count": 4, "indexed": False, "truncated": 0,
                "children": [
                    {"key": {"type": "string", "value": "status",   "length": 6, "binary": False, "truncated": 0}, "value": {"type": "string", "value": "pending", "length": 7, "binary": False, "truncated": 0}},
                    {"key": {"type": "string", "value": "amount",   "length": 6, "binary": False, "truncated": 0}, "value": {"type": "scalar", "kind": "float", "value": 99.99}},
                    {"key": {"type": "string", "value": "currency", "length": 8, "binary": False, "truncated": 0}, "value": {"type": "string", "value": "USD", "length": 3, "binary": False, "truncated": 0}},
                    {"key": {"type": "string", "value": "gateway",  "length": 7, "binary": False, "truncated": 0}, "value": {"type": "string", "value": "stripe", "length": 6, "binary": False, "truncated": 0}},
                ],
            },
        ],
    },
    {
        "timestamp": time.time() - 50,
        "source": {
            "file": "/home/testuser/ddev/sites/statamic.test/resources/views/blog/show.antlers.html",
            "line": 18, "name": "show.antlers.html",
        },
        "host": "statamic.test",
        "nodes": [
            {"type": "scalar", "kind": "null", "value": None},
        ],
    },
]

try:
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.settimeout(5)
    s.connect(("127.0.0.1", 9912))
    for d in dumps:
        line = base64.b64encode(json.dumps(d).encode()) + b"\n"
        s.sendall(line)
        time.sleep(0.05)
    s.close()
    print(f"    Sent {len(dumps)} TCP dumps.")
except Exception as e:
    print(f"    Warning: could not send TCP dumps: {e}")

# ── Mail ──────────────────────────────────────────────────────────────────────
print("  Seeding mail messages...")

def send_mail(payload):
    data = json.dumps(payload).encode()
    req = urllib.request.Request(
        f"{API}/api/mail/api/v1/send", data=data,
        headers={"Content-Type": "application/json"}, method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status
    except urllib.error.HTTPError as e:
        return e.code
    except Exception as e:
        print(f"    Warning: mail send failed: {e}")
        return 0

emails = [
    {
        "From": {"Email": "noreply@laravel.test", "Name": "Laravel App"},
        "To": [{"Email": "dev@example.com", "Name": "Developer"}],
        "Subject": "Welcome to devctl!",
        "Text": "Your local PHP development environment is ready.\n\nVisit: http://127.0.0.1:4000",
        "HTML": "<h1>Welcome to devctl!</h1><p>Your local PHP development environment is ready.</p><p>Visit <a href='http://127.0.0.1:4000'>the dashboard</a> to get started.</p>",
    },
    {
        "From": {"Email": "orders@laravel.test", "Name": "Laravel Shop"},
        "To": [{"Email": "customer@example.com", "Name": "Customer"}],
        "Subject": "Order Confirmation #1042",
        "Text": "Thank you for your order!\n\nOrder: #1042\nItem: devctl Pro\nAmount: $99.99\nStatus: Processing",
        "HTML": "<h2>Order Confirmation #1042</h2><p>Item: devctl Pro — $99.99</p><p>Status: <strong>Processing</strong></p>",
    },
    {
        "From": {"Email": "forms@statamic.test", "Name": "Statamic Site"},
        "To": [{"Email": "admin@statamic.test", "Name": "Site Admin"}],
        "Subject": "Contact Form Submission",
        "Text": "Name: John Smith\nEmail: john@example.com\nMessage: Hi, interested in your services. Could you send more info?",
    },
    {
        "From": {"Email": "security@laravel.test", "Name": "Laravel App"},
        "To": [{"Email": "user@example.com", "Name": "User"}],
        "Subject": "Password Reset Request",
        "Text": "We received a request to reset your password.\n\nReset link: https://laravel.test/password/reset/abc123\n\nExpires in 60 minutes.",
    },
    {
        "From": {"Email": "notifications@wordpress.test", "Name": "WordPress"},
        "To": [{"Email": "author@example.com", "Name": "Author"}],
        "Subject": "New Comment on 'Getting Started with devctl'",
        "Text": "Bob Jones commented: Great article! devctl has really improved my workflow.\n\nhttps://wordpress.test/getting-started/#comment-42",
    },
    {
        "From": {"Email": "digest@laravel.test", "Name": "Laravel App"},
        "To": [{"Email": "dev@example.com", "Name": "Developer"}],
        "Subject": "Daily Digest — 3 new signups today",
        "Text": "New signups: 3\nNew orders: 7\nRevenue: $847.50\n\nView full report: https://laravel.test/admin/reports",
    },
]

for email in emails:
    status = send_mail(email)
    if status in (200, 201, 202):
        print(f"    Sent: {email['Subject']}")
    else:
        print(f"    Warning: '{email['Subject']}' returned {status}")

# ── SPX Profiles ──────────────────────────────────────────────────────────────
print("  Seeding SPX profiles...")

SPX_DIR = f"{SERVER_ROOT}/php/8.3/spx-data"
os.makedirs(SPX_DIR, exist_ok=True)

def make_trace_gz(functions, events):
    """Return gzipped SPX call trace bytes."""
    content = "[events]\n" + "\n".join(events) + "\n\n[functions]\n" + "\n".join(functions) + "\n"
    buf = io.BytesIO()
    with gzip.GzipFile(fileobj=buf, mode="wb", mtime=0) as gz:
        gz.write(content.encode())
    return buf.getvalue()

profiles = [
    {
        "key": "spx_demo_20240101_120000_a1b2c3d4",
        "meta": {
            "http_host": "laravel.test", "http_method": "GET", "http_request_uri": "/",
            "wall_time_ms": 45.32, "peak_memory_usage": 6291456,
            "called_function_count": 127, "exec_ts": 1704067200,
        },
        "functions": [
            "{main}",
            "Illuminate\\Foundation\\Http\\Kernel::handle",
            "Illuminate\\Routing\\Router::dispatch",
            "App\\Http\\Controllers\\HomeController::index",
            "Illuminate\\Database\\Query\\Builder::get",
            "Illuminate\\View\\Factory::make",
        ],
        "events": [
            "0 1 0", "1 1 500", "2 1 600", "2 0 3200",
            "3 1 3500", "4 1 5000", "4 0 18000",
            "5 1 20000", "5 0 35000", "3 0 38000",
            "1 0 44000", "0 0 45320",
        ],
    },
    {
        "key": "spx_demo_20240101_120500_e5f6a7b8",
        "meta": {
            "http_host": "laravel.test", "http_method": "POST", "http_request_uri": "/api/users",
            "wall_time_ms": 120.17, "peak_memory_usage": 8388608,
            "called_function_count": 312, "exec_ts": 1704067500,
        },
        "functions": [
            "{main}",
            "Illuminate\\Foundation\\Http\\Kernel::handle",
            "App\\Http\\Controllers\\Api\\UserController::store",
            "Illuminate\\Validation\\Validator::validate",
            "Illuminate\\Database\\Eloquent\\Model::save",
            "App\\Events\\UserRegistered::broadcast",
            "Illuminate\\Mail\\Mailer::send",
        ],
        "events": [
            "0 1 0", "1 1 300", "2 1 400",
            "3 1 600", "3 0 15000",
            "4 1 16000", "4 0 55000",
            "5 1 56000", "6 1 58000", "6 0 90000", "5 0 92000",
            "2 0 115000", "1 0 119000", "0 0 120170",
        ],
    },
    {
        "key": "spx_demo_20240101_121000_c9d0e1f2",
        "meta": {
            "http_host": "statamic.test", "http_method": "GET", "http_request_uri": "/blog",
            "wall_time_ms": 78.45, "peak_memory_usage": 5242880,
            "called_function_count": 198, "exec_ts": 1704067800,
        },
        "functions": [
            "{main}",
            "Statamic\\Http\\Kernel::handle",
            "Statamic\\StaticCaching\\Middleware\\Cache::handle",
            "Statamic\\View\\Antlers\\Language\\Runtime\\NodeProcessor::process",
            "Statamic\\Tags\\Collection\\Collection::index",
            "Illuminate\\Database\\Eloquent\\Builder::get",
        ],
        "events": [
            "0 1 0", "1 1 200", "2 1 400", "2 0 1500",
            "3 1 2000", "4 1 3000", "5 1 4000", "5 0 35000",
            "4 0 40000", "3 0 72000", "1 0 77500", "0 0 78450",
        ],
    },
]

for p in profiles:
    meta_path  = os.path.join(SPX_DIR, p["key"] + ".json")
    trace_path = os.path.join(SPX_DIR, p["key"] + ".txt.gz")
    with open(meta_path, "w") as f:
        json.dump(p["meta"], f)
    with open(trace_path, "wb") as f:
        f.write(make_trace_gz(p["functions"], p["events"]))
    print(f"    {p['meta']['http_method']} {p['meta']['http_host']}{p['meta']['http_request_uri']} ({p['meta']['wall_time_ms']}ms)")

os.system(f"chown -R testuser:testuser {SPX_DIR}")
print(f"    Created {len(profiles)} SPX profiles.")

# ── MaxIO demo bucket ──────────────────────────────────────────────────────────
print("  Seeding MaxIO demo bucket...")

def s3_put(path, body=b"", content_type="application/octet-stream"):
    url = f"{API}/api/maxio/s3{path}"
    req = urllib.request.Request(
        url, data=body,
        headers={"Content-Type": content_type, "Content-Length": str(len(body))},
        method="PUT",
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status
    except urllib.error.HTTPError as e:
        return e.code
    except Exception as e:
        print(f"    Warning: s3 PUT {path}: {e}")
        return 0

status = s3_put("/demo-files")
if status in (200, 204, 409):
    print("    Bucket 'demo-files' ready.")
    files = [
        ("/demo-files/README.md",
         b"# Demo Files\n\nSample asset storage for the devctl demo.\n",
         "text/plain"),
        ("/demo-files/config.json",
         json.dumps({"app": "laravel", "env": "local", "debug": True}, indent=2).encode(),
         "application/json"),
        ("/demo-files/exports/users.csv",
         b"id,name,email\n1,Alice,alice@example.com\n2,Bob,bob@example.com\n3,Carol,carol@example.com\n",
         "text/csv"),
        ("/demo-files/exports/report.txt",
         b"Monthly Report\n==============\nRevenue: $12,450\nOrders: 87\nNew users: 23\n",
         "text/plain"),
        ("/demo-files/uploads/logo.png",
         b"\x89PNG\r\n\x1a\n" + b"\x00" * 16,  # minimal PNG-like stub
         "image/png"),
    ]
    for fpath, body, ct in files:
        s = s3_put(fpath, body, ct)
        if s in (200, 204):
            print(f"    Uploaded {fpath}")
        else:
            print(f"    Warning: upload {fpath} returned {s}")
else:
    print(f"    Warning: bucket creation returned {status}.")

print("\nSeed data complete.")
SEED_SCRIPT

success "Demo data seeded."

# ─── Set up port proxy devices ────────────────────────────────────────────────
info "Setting up port forwarding..."
incus config device remove "$CONTAINER" port4001 2>/dev/null || true
incus config device remove "$CONTAINER" port8161 2>/dev/null || true

incus config device add "$CONTAINER" port4001 proxy \
  listen="tcp:127.0.0.1:${DEMO_PORT}" connect="tcp:127.0.0.1:4000"

incus config device add "$CONTAINER" port8161 proxy \
  listen="tcp:127.0.0.1:${WHODB_PORT}" connect="tcp:127.0.0.1:${WHODB_PORT}"

success "Port forwarding configured."

# Wait for proxy to be accessible from the host
info "Waiting for port ${DEMO_PORT} to become accessible on the host..."
TIMEOUT=15; ELAPSED=0
while true; do
  if curl -sf "http://127.0.0.1:${DEMO_PORT}/api/settings/resolved" >/dev/null 2>&1; then
    success "Dashboard accessible on http://127.0.0.1:${DEMO_PORT}."
    break
  fi
  [[ $ELAPSED -ge $TIMEOUT ]] && { error "Timed out waiting for port ${DEMO_PORT}."; exit 1; }
  sleep 1; ELAPSED=$((ELAPSED + 1))
done

# ─── Run screenshots ──────────────────────────────────────────────────────────
info "Running screenshots..."
[[ ! -d "${SCRIPT_DIR}/node_modules" ]] && (cd "$SCRIPT_DIR" && npm install)
BASE_URL="http://127.0.0.1:${DEMO_PORT}" node "${SCRIPT_DIR}/screenshots.js"
success "Screenshots saved to docs/."

# ─── Done ─────────────────────────────────────────────────────────────────────
echo ""
printf '%s─────────────────────────────────────────────────────────%s\n' "${BOLD}" "${RESET}"
printf '%s devctl demo container ready%s\n'                              "${BOLD}" "${RESET}"
printf ' Container : %s\n'                                               "${CONTAINER}"
printf ' Dashboard : %shttp://127.0.0.1:%s%s\n'                         "${CYAN}" "${DEMO_PORT}" "${RESET}"
printf ' WhoDB UI  : %shttp://127.0.0.1:%s%s\n'                         "${CYAN}" "${WHODB_PORT}" "${RESET}"
printf '
 Re-run screenshots without rebuilding:
'
printf '   bash scripts/demo.sh --screenshots-only
'
printf '\n Destroy demo container:\n'
printf '   incus delete --force %s\n'                                    "${CONTAINER}"
printf '%s─────────────────────────────────────────────────────────%s\n' "${BOLD}" "${RESET}"
