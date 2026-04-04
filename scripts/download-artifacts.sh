#!/usr/bin/env bash
# scripts/download-artifacts.sh — Download all devctl service binaries/archives
# into the devctl-test-artifacts Incus storage volume so test runs don't need
# to hit the internet. Run once; re-run to refresh stale files.
#
# The volume must already exist (created by test-env-setup.sh).
# Must be run as root (or via sudo) since it writes into the Incus storage pool.
set -euo pipefail

if [ -t 1 ] && command -v tput &>/dev/null && tput colors &>/dev/null && [ "$(tput colors)" -ge 8 ]; then
  GREEN="$(tput setaf 2)"; CYAN="$(tput setaf 6)"; RED="$(tput setaf 1)"; YELLOW="$(tput setaf 3)"; RESET="$(tput sgr0)"
else
  GREEN="" CYAN="" RED="" YELLOW="" RESET=""
fi
info()    { printf '%s→ %s%s\n' "${CYAN}"   "$*" "${RESET}"; }
success() { printf '%s✓ %s%s\n' "${GREEN}"  "$*" "${RESET}"; }
skip()    { printf '%s⊘ %s%s\n' "${YELLOW}" "$*" "${RESET}"; }
error()   { printf '%s✗ %s%s\n' "${RED}"    "$*" "${RESET}" >&2; }

# ─── Resolve the volume's filesystem path ─────────────────────────────────────
POOL="default"
VOLUME="devctl-test-artifacts"

CACHE_DIR="$(incus storage volume get "$POOL" "$VOLUME" volatile.rootfs.path 2>/dev/null || true)"
if [[ -z "$CACHE_DIR" ]]; then
  # Fallback: dir driver keeps volumes at a predictable location.
  # The dir driver prepends the pool name: <pool>_<volume>
  CACHE_DIR="/var/lib/incus/storage-pools/${POOL}/custom/${POOL}_${VOLUME}"
  # Also try without the pool prefix in case the layout differs
  if [[ ! -d "$CACHE_DIR" ]]; then
    CACHE_DIR="/var/lib/incus/storage-pools/${POOL}/custom/${VOLUME}"
  fi
fi
if [[ ! -d "$CACHE_DIR" ]]; then
  error "Cache directory '$CACHE_DIR' does not exist."
  error "Run 'sudo make test-env-setup' first to create the Incus storage volume."
  exit 1
fi

info "Downloading artifacts to: ${CACHE_DIR}"

# ─── Helper: download if not already cached ───────────────────────────────────
download() {
  local name="$1"  # human-readable label
  local url="$2"   # source URL
  local dest="$3"  # destination filename (basename only, saved to CACHE_DIR)

  local full_dest="${CACHE_DIR}/${dest}"
  if [[ -f "$full_dest" ]]; then
    skip "${name} — already cached (${dest})"
    return 0
  fi
  info "Downloading ${name}..."
  local tmp_dest="${full_dest}.tmp"
  if curl -fsSL -o "$tmp_dest" "$url"; then
    mv "$tmp_dest" "$full_dest"
    success "${name} → ${dest}"
  else
    rm -f "$tmp_dest"
    error "Failed to download ${name} from: ${url}"
    return 1
  fi
}

# ─── Caddy ────────────────────────────────────────────────────────────────────
CADDY_VERSION="v2.10.0"
download "Caddy ${CADDY_VERSION}" \
  "https://github.com/caddyserver/caddy/releases/download/${CADDY_VERSION}/caddy_2.10.0_linux_amd64.tar.gz" \
  "caddy_2.10.0_linux_amd64.tar.gz"

# ─── Valkey (noble + jammy) ───────────────────────────────────────────────────
VALKEY_VERSION="9.0.3"
download "Valkey ${VALKEY_VERSION} (noble/Ubuntu 24.04)" \
  "https://download.valkey.io/releases/valkey-${VALKEY_VERSION}-noble-x86_64.tar.gz" \
  "valkey-${VALKEY_VERSION}-noble-x86_64.tar.gz"
download "Valkey ${VALKEY_VERSION} (jammy/Ubuntu 22.04)" \
  "https://download.valkey.io/releases/valkey-${VALKEY_VERSION}-jammy-x86_64.tar.gz" \
  "valkey-${VALKEY_VERSION}-jammy-x86_64.tar.gz"

# ─── Mailpit ──────────────────────────────────────────────────────────────────
MAILPIT_VERSION="v1.29.2"
download "Mailpit ${MAILPIT_VERSION}" \
  "https://github.com/axllent/mailpit/releases/download/${MAILPIT_VERSION}/mailpit-linux-amd64.tar.gz" \
  "mailpit-linux-amd64.tar.gz"
# Update artifact: same tarball, filename matches the -o dest used by MailpitInstaller.UpdateW
download "Mailpit ${MAILPIT_VERSION} (update artifact)" \
  "https://github.com/axllent/mailpit/releases/download/${MAILPIT_VERSION}/mailpit-linux-amd64.tar.gz" \
  "mailpit-update-linux-amd64.tar.gz"

# ─── Meilisearch ──────────────────────────────────────────────────────────────
MEILISEARCH_VERSION="v1.37.0"
download "Meilisearch ${MEILISEARCH_VERSION}" \
  "https://github.com/meilisearch/meilisearch/releases/download/${MEILISEARCH_VERSION}/meilisearch-linux-amd64" \
  "meilisearch-linux-amd64"

# ─── Typesense ────────────────────────────────────────────────────────────────
TYPESENSE_VERSION="30.1"
download "Typesense ${TYPESENSE_VERSION}" \
  "https://dl.typesense.org/releases/${TYPESENSE_VERSION}/typesense-server-${TYPESENSE_VERSION}-linux-amd64.tar.gz" \
  "typesense-server-${TYPESENSE_VERSION}-linux-amd64.tar.gz"

# ─── WhoDB ────────────────────────────────────────────────────────────────────
WHODB_VERSION="0.100.0"
download "WhoDB ${WHODB_VERSION}" \
  "https://github.com/clidey/whodb/releases/download/${WHODB_VERSION}/whodb-${WHODB_VERSION}-linux-amd64" \
  "whodb-${WHODB_VERSION}-linux-amd64"

# ─── MaxIO ────────────────────────────────────────────────────────────────────
# MaxIO releases a versioned tar.gz; the installer queries the GitHub API to
# find the latest release URL.  Cache the latest known release here and update
# this URL whenever the version changes.
MAXIO_VERSION="v0.0.10"
download "MaxIO ${MAXIO_VERSION}" \
  "https://github.com/coollabsio/maxio/releases/download/${MAXIO_VERSION}/maxio-linux-amd64.tar.gz" \
  "maxio-linux-amd64.tar.gz"

# ─── PostgreSQL (Percona) ─────────────────────────────────────────────────────
POSTGRES_VERSION="18.3"
POSTGRES_MAJOR="18"
download "PostgreSQL ${POSTGRES_VERSION} (Percona tarball)" \
  "https://downloads.percona.com/downloads/postgresql-distribution-${POSTGRES_MAJOR}/${POSTGRES_VERSION}/binary/tarball/percona-postgresql-${POSTGRES_VERSION}-ssl3-linux-x86_64.tar.gz" \
  "percona-postgresql-${POSTGRES_VERSION}-ssl3-linux-x86_64.tar.gz"

# ─── MySQL 8.4 (.deb packages) ────────────────────────────────────────────────
MYSQL_VERSION="8.4.8"
MYSQL_BASE="https://repo.mysql.com/apt/ubuntu/pool/mysql-8.4-lts/m/mysql-community"
for pkg in "mysql-community-server-core" "mysql-community-client-core" "mysql-community-client"; do
  download "MySQL ${pkg} ${MYSQL_VERSION}" \
    "${MYSQL_BASE}/${pkg}_${MYSQL_VERSION}-1ubuntu24.04_amd64.deb" \
    "${pkg}_${MYSQL_VERSION}-1ubuntu24.04_amd64.deb"
done

echo ""
success "All artifacts downloaded to ${CACHE_DIR}"
echo ""
du -sh "${CACHE_DIR}"
