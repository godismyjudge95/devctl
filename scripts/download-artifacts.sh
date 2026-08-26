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

# ─── devctl self-update binary ──────────────────────────────────────────────
# Used by the self-update tests. The curl shim serves this when the update
# handler downloads the binary to a temp file named "devctl".
# Always download the current binary so tests can exercise the full update
# flow even without a real new release.
DEVCTL_VERSION="v0.3.0"
download "devctl ${DEVCTL_VERSION} (self-update artifact)" \
  "https://github.com/godismyjudge95/devctl/releases/download/${DEVCTL_VERSION}/devctl" \
  "devctl"

# ─── PHP static binaries + manifest fixtures ─────────────────────────────────
# Cache deterministic tagged PHP release fixtures so tests can verify installs
# and upgrades against exact release tags rather than the old mutable latest tag.
# OLD = pre-upgrade baseline (8.3/8.4 only).
# NEW = current Latest (includes legacy 7.0/7.2 + modern 8.x).
PHP_RELEASE_OLD="php-binaries-20260421.1"
PHP_RELEASE_NEW="php-binaries-20260723.24"

for PHP_VERSION in 8.3 8.4; do
  PHP_BASE="https://github.com/godismyjudge95/devctl/releases/download/${PHP_RELEASE_OLD}"
  download "PHP ${PHP_VERSION} FPM (${PHP_RELEASE_OLD})" \
    "${PHP_BASE}/php-${PHP_VERSION}-fpm-linux-x86_64" \
    "${PHP_RELEASE_OLD}-php-${PHP_VERSION}-fpm-linux-x86_64"
  download "PHP ${PHP_VERSION} CLI (${PHP_RELEASE_OLD})" \
    "${PHP_BASE}/php-${PHP_VERSION}-cli-linux-x86_64" \
    "${PHP_RELEASE_OLD}-php-${PHP_VERSION}-cli-linux-x86_64"
done

for PHP_VERSION in 7.0 7.2 8.3 8.4; do
  PHP_BASE="https://github.com/godismyjudge95/devctl/releases/download/${PHP_RELEASE_NEW}"
  download "PHP ${PHP_VERSION} FPM (${PHP_RELEASE_NEW})" \
    "${PHP_BASE}/php-${PHP_VERSION}-fpm-linux-x86_64" \
    "${PHP_RELEASE_NEW}-php-${PHP_VERSION}-fpm-linux-x86_64"
  download "PHP ${PHP_VERSION} CLI (${PHP_RELEASE_NEW})" \
    "${PHP_BASE}/php-${PHP_VERSION}-cli-linux-x86_64" \
    "${PHP_RELEASE_NEW}-php-${PHP_VERSION}-cli-linux-x86_64"
done

cat > "${CACHE_DIR}/${PHP_RELEASE_OLD}-php-binaries.json" <<'EOF'
{
  "release_tag": "php-binaries-20260421.1",
  "built_at": "2026-04-21T21:07:11Z",
  "php_versions": {
    "8.3": "8.3.21",
    "8.4": "8.4.18"
  },
  "assets": {
    "8.3": {
      "cli": "php-8.3-cli-linux-x86_64",
      "fpm": "php-8.3-fpm-linux-x86_64"
    },
    "8.4": {
      "cli": "php-8.4-cli-linux-x86_64",
      "fpm": "php-8.4-fpm-linux-x86_64"
    }
  }
}
EOF

cat > "${CACHE_DIR}/${PHP_RELEASE_NEW}-php-binaries.json" <<'EOF'
{
  "release_tag": "php-binaries-20260723.24",
  "built_at": "2026-07-23T20:08:57Z",
  "php_versions": {
    "7.0": "7.0.33",
    "7.2": "7.2.34",
    "7.4": "7.4.33",
    "8.0": "8.0.30",
    "8.1": "8.1.34",
    "8.2": "8.2.32",
    "8.3": "8.3.32",
    "8.4": "8.4.23",
    "8.5": "8.5.8"
  },
  "assets": {
    "7.0": {
      "cli": "php-7.0-cli-linux-x86_64",
      "fpm": "php-7.0-fpm-linux-x86_64"
    },
    "7.2": {
      "cli": "php-7.2-cli-linux-x86_64",
      "fpm": "php-7.2-fpm-linux-x86_64"
    },
    "7.4": {
      "cli": "php-7.4-cli-linux-x86_64",
      "fpm": "php-7.4-fpm-linux-x86_64"
    },
    "8.0": {
      "cli": "php-8.0-cli-linux-x86_64",
      "fpm": "php-8.0-fpm-linux-x86_64"
    },
    "8.1": {
      "cli": "php-8.1-cli-linux-x86_64",
      "fpm": "php-8.1-fpm-linux-x86_64"
    },
    "8.2": {
      "cli": "php-8.2-cli-linux-x86_64",
      "fpm": "php-8.2-fpm-linux-x86_64"
    },
    "8.3": {
      "cli": "php-8.3-cli-linux-x86_64",
      "fpm": "php-8.3-fpm-linux-x86_64"
    },
    "8.4": {
      "cli": "php-8.4-cli-linux-x86_64",
      "fpm": "php-8.4-fpm-linux-x86_64"
    },
    "8.5": {
      "cli": "php-8.5-cli-linux-x86_64",
      "fpm": "php-8.5-fpm-linux-x86_64"
    }
  }
}
EOF
success "PHP manifest fixtures cached."

# ─── Caddy ────────────────────────────────────────────────────────────────────
CADDY_VERSION="v2.10.0"
download "Caddy ${CADDY_VERSION}" \
  "https://github.com/caddyserver/caddy/releases/download/${CADDY_VERSION}/caddy_2.10.0_linux_amd64.tar.gz" \
  "caddy-linux-amd64.tar.gz"

# ─── Valkey (noble + jammy) ───────────────────────────────────────────────────
VALKEY_VERSION="9.0.3"
# Noble (Ubuntu 24.04) — cached with the shim-interceptable name the installer uses.
# The installer downloads to /tmp/valkey-{version}.tar.gz so the basename must match.
download "Valkey ${VALKEY_VERSION} (noble/Ubuntu 24.04)" \
  "https://download.valkey.io/releases/valkey-${VALKEY_VERSION}-noble-x86_64.tar.gz" \
  "valkey-${VALKEY_VERSION}.tar.gz"
# Also keep the full distro-named copy so test_env_setup humans can identify it.
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
# Installer downloads directly to {meiliDir}/meilisearch (basename: meilisearch).
download "Meilisearch ${MEILISEARCH_VERSION}" \
  "https://github.com/meilisearch/meilisearch/releases/download/${MEILISEARCH_VERSION}/meilisearch-linux-amd64" \
  "meilisearch"

# ─── Typesense ────────────────────────────────────────────────────────────────
TYPESENSE_VERSION="30.1"
download "Typesense ${TYPESENSE_VERSION}" \
  "https://dl.typesense.org/releases/${TYPESENSE_VERSION}/typesense-server-${TYPESENSE_VERSION}-linux-amd64.tar.gz" \
  "typesense-server-${TYPESENSE_VERSION}-linux-amd64.tar.gz"

# ─── MaxIO ────────────────────────────────────────────────────────────────────
# The GitHub asset is named maxio-linux-amd64-{version}.tar.gz but the installer
# downloads to /tmp/maxio-{version}-linux-amd64.tar.gz, so the cached filename
# must match the installer temp file (the shim matches on basename of -o dest).
MAXIO_VERSION="0.3.2"
download "MaxIO ${MAXIO_VERSION}" \
  "https://github.com/coollabsio/maxio/releases/download/v${MAXIO_VERSION}/maxio-linux-amd64-${MAXIO_VERSION}.tar.gz" \
  "maxio-${MAXIO_VERSION}-linux-amd64.tar.gz"

# ─── ClickHouse ───────────────────────────────────────────────────────────────
# Installer downloads to /tmp/clickhouse-common-static-amd64.tgz (InstallW)
# or clickhouse-common-static-update.tgz (UpdateW) — basenames are version-
# independent so the curl shim matches regardless of GitHub's "latest" tag.
CLICKHOUSE_VERSION="25.8.28.1"
download "ClickHouse ${CLICKHOUSE_VERSION}" \
  "https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-${CLICKHOUSE_VERSION}-amd64.tgz" \
  "clickhouse-common-static-amd64.tgz"
download "ClickHouse ${CLICKHOUSE_VERSION} (update artifact)" \
  "https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-${CLICKHOUSE_VERSION}-amd64.tgz" \
  "clickhouse-common-static-update.tgz"

# ─── PostgreSQL (Percona) ─────────────────────────────────────────────────────
# Must be ≥ 18.4 for TimescaleDB packages built against PG 18.4 (-1804).
POSTGRES_VERSION="18.4"
POSTGRES_MAJOR="18"
download "PostgreSQL ${POSTGRES_VERSION} (Percona tarball)" \
  "https://downloads.percona.com/downloads/postgresql-distribution-${POSTGRES_MAJOR}/${POSTGRES_VERSION}/binary/tarball/percona-postgresql-${POSTGRES_VERSION}-ssl3-linux-x86_64.tar.gz" \
  "percona-postgresql-${POSTGRES_VERSION}-ssl3-linux-x86_64.tar.gz"

# ─── TimescaleDB Community Edition (extension debs for Postgres) ──────────────
# Extracted into {serverRoot}/postgres/{lib,share/extension}/ — not installed via APT.
# Filenames match the /tmp DEST basenames used by install/timescaledb.go so the
# curl shim in the test container can serve them from cache.
TS_VERSION="2.28.3"
TS_PKG_TAG="2.28.3~ubuntu22.04-1804"
TS_ARCH="amd64"
TS_LOADER="timescaledb-2-loader-postgresql-${POSTGRES_MAJOR}_${TS_PKG_TAG}_${TS_ARCH}.deb"
TS_COMMUNITY="timescaledb-2-${TS_VERSION}-postgresql-${POSTGRES_MAJOR}_${TS_PKG_TAG}_${TS_ARCH}.deb"
TS_BASE="https://packagecloud.io/timescale/timescaledb/ubuntu/pool/jammy/main/t"
download "TimescaleDB loader ${TS_VERSION}" \
  "${TS_BASE}/timescaledb-2-${TS_VERSION}-postgresql-${POSTGRES_MAJOR}/${TS_LOADER}" \
  "${TS_LOADER}"
download "TimescaleDB Community ${TS_VERSION}" \
  "${TS_BASE}/timescaledb-2-${TS_VERSION}-postgresql-${POSTGRES_MAJOR}/${TS_COMMUNITY}" \
  "${TS_COMMUNITY}"

# ─── pg_clickhouse (prebuilt deb extracted into Percona tree) ─────────────────
# Basename must match install/pg_clickhouse.go curl DEST for the test shim.
# Debs are published under the rolling customer-testdeb tag (pg18/amd64).
PG_CLICKHOUSE_VERSION="0.2.0"
PG_CLICKHOUSE_DEB="pg-clickhouse-${PG_CLICKHOUSE_VERSION}-pg${POSTGRES_MAJOR}-amd64.deb"
download "pg_clickhouse ${PG_CLICKHOUSE_VERSION}" \
  "https://github.com/ClickHouse/pg_clickhouse/releases/download/customer-testdeb/${PG_CLICKHOUSE_DEB}" \
  "${PG_CLICKHOUSE_DEB}"

# ─── MySQL 8.4 (.deb packages) ────────────────────────────────────────────────
# The MySQL installer downloads to /tmp/mysql-{pkg}-{version}.deb (e.g.
# mysql-mysql-community-server-core-8.4.8.deb), so the cache filenames must
# match that pattern.
MYSQL_VERSION="8.4.8"
MYSQL_BASE="https://repo.mysql.com/apt/ubuntu/pool/mysql-8.4-lts/m/mysql-community"
for pkg in "mysql-community-server-core" "mysql-community-client-core" "mysql-community-client"; do
  download "MySQL ${pkg} ${MYSQL_VERSION}" \
    "${MYSQL_BASE}/${pkg}_${MYSQL_VERSION}-1ubuntu24.04_amd64.deb" \
    "mysql-${pkg}-${MYSQL_VERSION}.deb"
done

# ─── Composer ─────────────────────────────────────────────────────────────────
# Installer saves to {binDir}/composer (basename: composer).
download "Composer (stable)" \
  "https://getcomposer.org/composer-stable.phar" \
  "composer"

# ─── WP-CLI ───────────────────────────────────────────────────────────────────
# Helper (tools.WPCLI) and PHP install save the phar as {binDir}/wp.
# curl dest is a temp file; the shim matches this GitHub TAG-ASSET cache key.
WPCLI_VERSION="2.12.0"
download "WP-CLI ${WPCLI_VERSION}" \
  "https://github.com/wp-cli/wp-cli/releases/download/v${WPCLI_VERSION}/wp-cli-${WPCLI_VERSION}.phar" \
  "v${WPCLI_VERSION}-wp-cli-${WPCLI_VERSION}.phar"

echo ""
success "All artifacts downloaded to ${CACHE_DIR}"
echo ""
du -sh "${CACHE_DIR}"
