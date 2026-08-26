#!/usr/bin/env bash
# Local (Docker) mirror of the GHA build-legacy job for PHP 7.0 / 7.2 / 7.4.
#
# Purpose: iterate on scripts/patch-spc-for-php7.sh without burning Actions minutes.
# GHA remains the publish path (release assets + php-binaries.json).
#
# Safety:
#   - Does NOT touch host PHP, systemd, or `make install`
#   - Works only under a dedicated workdir (default: ~/ddev/spc-php7-local)
#   - Never deletes the workdir or downloads cache unless you pass --wipe-build
#     (which only removes spc build intermediates, not downloads/)
#   - Never force-resets an existing git checkout
#
# Prerequisites: docker (same as CI's spc-alpine-docker), git, bash
#
# Usage:
#   scripts/local-build-php7.sh 7.0
#   scripts/local-build-php7.sh 7.2
#   scripts/local-build-php7.sh 7.4
#   PHP=7.2 WORKDIR=~/ddev/spc-php7-local scripts/local-build-php7.sh
#
# Flags:
#   --skip-download   reuse existing downloads/ (faster re-builds)
#   --skip-doctor     skip spc-alpine-docker doctor
#   --skip-patch      do not re-run patch-spc-for-php7.sh
#   --wipe-build      remove buildroot/ source/ build intermediates before build
#                     (keeps downloads/; refuses paths outside WORKDIR)
#   --dry-run         print steps only

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PATCH_SCRIPT="${ROOT}/scripts/patch-spc-for-php7.sh"

PHP="${PHP:-}"
WORKDIR="${WORKDIR:-$HOME/ddev/spc-php7-local}"
SPC_DIR=""
SKIP_DOWNLOAD=0
SKIP_DOCTOR=0
SKIP_PATCH=0
WIPE_BUILD=0
DRY_RUN=0

usage() {
  sed -n '2,35p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage 0 ;;
    --skip-download) SKIP_DOWNLOAD=1; shift ;;
    --skip-doctor) SKIP_DOCTOR=1; shift ;;
    --skip-patch) SKIP_PATCH=1; shift ;;
    --wipe-build) WIPE_BUILD=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    7.0|7.2|7.4)
      if [[ -n "$PHP" && "$PHP" != "$1" ]]; then
        echo "conflicting PHP versions: PHP=$PHP and $1" >&2
        exit 2
      fi
      PHP="$1"
      shift
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage 2
      ;;
  esac
done

if [[ -z "$PHP" ]]; then
  echo "usage: $0 <7.0|7.2|7.4> [--skip-download] [--wipe-build]" >&2
  exit 2
fi

case "$PHP" in
  7.0)
    EXT_LIST="bcmath,bz2,calendar,ctype,curl,dom,exif,fileinfo,filter,ftp,gd,gmp,iconv,mbstring,mysqli,mysqlnd,openssl,pcntl,pdo,pdo_mysql,pdo_pgsql,pdo_sqlite,phar,posix,readline,redis,session,simplexml,soap,sockets,sqlite3,tokenizer,xml,xmlreader,xmlwriter,zip,zlib"
    ;;
  7.2)
    EXT_LIST="bcmath,bz2,calendar,ctype,curl,dom,exif,fileinfo,filter,ftp,gd,gmp,iconv,mbregex,mbstring,mysqli,mysqlnd,openssl,pcntl,pdo,pdo_mysql,pdo_pgsql,pdo_sqlite,phar,posix,readline,redis,session,simplexml,soap,sockets,sodium,sqlite3,tokenizer,xml,xmlreader,xmlwriter,zip,zlib"
    ;;
  7.4)
    EXT_LIST="bcmath,bz2,calendar,ctype,curl,dom,exif,ffi,fileinfo,filter,ftp,gd,gmp,iconv,intl,mbregex,mbstring,mysqli,mysqlnd,openssl,pcntl,pdo,pdo_mysql,pdo_pgsql,pdo_sqlite,phar,posix,readline,redis,session,simplexml,soap,sockets,sodium,sqlite3,tokenizer,xml,xmlreader,xmlwriter,zip,zlib"
    ;;
  *)
    echo "unsupported PHP version: $PHP (want 7.0, 7.2, or 7.4)" >&2
    exit 2
    ;;
esac

# Same pins as .github/workflows/build-php.yml build-legacy
OPENSSL_URL="https://github.com/openssl/openssl/releases/download/OpenSSL_1_1_1w/openssl-1.1.1w.tar.gz"
CURL_URL="https://curl.se/download/curl-7.88.1.tar.gz"
LIBICONV_URL="https://ftpmirror.gnu.org/libiconv/libiconv-1.19.tar.gz"
ONIG_URL="https://github.com/kkos/oniguruma/releases/download/v6.9.4/onig-6.9.4.tar.gz"

run() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '+'
    printf ' %q' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 127
  }
}

need_cmd git
if [[ ! -f "$PATCH_SCRIPT" ]]; then
  echo "patch script not found: $PATCH_SCRIPT" >&2
  exit 1
fi

# Docker is only required for a real build (dry-run can print the plan without it).
if [[ "$DRY_RUN" -eq 0 ]]; then
  need_cmd docker
  if ! docker info >/dev/null 2>&1; then
    if sudo docker info >/dev/null 2>&1; then
      export SPC_USE_SUDO=yes
      echo "==> docker needs sudo; set SPC_USE_SUDO=yes"
    else
      echo "docker is not usable (start the daemon, or fix /var/run/docker.sock permissions)" >&2
      exit 1
    fi
  fi
fi

# Resolve workdir to an absolute path; create parents only (mkdir -p is additive).
mkdir -p "$WORKDIR"
WORKDIR="$(cd "$WORKDIR" && pwd)"
SPC_DIR="${WORKDIR}/static-php-cli"

echo "==> workdir: $WORKDIR"
echo "==> php:     $PHP"
echo "==> exts:    $EXT_LIST"

# Clone once. If the tree already exists, leave it alone (no reset / clean).
if [[ ! -d "$SPC_DIR/.git" ]]; then
  echo "==> cloning crazywhalecc/static-php-cli @ 2.3.0 (first time)"
  run git clone --branch 2.3.0 --depth 1 \
    https://github.com/crazywhalecc/static-php-cli.git "$SPC_DIR"
else
  echo "==> reusing existing checkout: $SPC_DIR"
  # Soft sanity check only — never checkout/reset.
  if [[ "$DRY_RUN" -eq 0 ]]; then
    branch="$(git -C "$SPC_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
    describe="$(git -C "$SPC_DIR" describe --tags --always 2>/dev/null || true)"
    echo "    HEAD: ${branch} (${describe})"
    if [[ "$describe" != "2.3.0" && "$branch" != "2.3.0" && "$describe" != v2.3.0 ]]; then
      echo "WARNING: expected tag/branch 2.3.0; continuing with existing tree (no reset)." >&2
    fi
  fi
fi

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "==> (dry-run) would cd $SPC_DIR and run patch / doctor / download / build"
  echo "==> dry-run complete"
  exit 0
fi

cd "$SPC_DIR"

if [[ "$SKIP_PATCH" -eq 0 ]]; then
  echo "==> applying $PATCH_SCRIPT"
  run bash "$PATCH_SCRIPT"
else
  echo "==> skipping patch (--skip-patch)"
fi

if [[ "$SKIP_DOCTOR" -eq 0 ]]; then
  echo "==> spc-alpine-docker doctor --auto-fix"
  run ./bin/spc-alpine-docker doctor --auto-fix
else
  echo "==> skipping doctor (--skip-doctor)"
fi

if [[ "$SKIP_DOWNLOAD" -eq 0 ]]; then
  echo "==> downloading sources (cached under $SPC_DIR/downloads)"
  run ./bin/spc-alpine-docker download \
    --with-php="$PHP" \
    --for-extensions="$EXT_LIST" \
    --custom-url "openssl:${OPENSSL_URL}" \
    --custom-url "curl:${CURL_URL}" \
    --custom-url "libiconv:${LIBICONV_URL}" \
    --custom-url "onig:${ONIG_URL}" \
    --ignore-cache-sources=php-src,openssl,curl,libiconv,onig \
    --retry=5
else
  echo "==> skipping download (--skip-download); using existing downloads/"
fi

if [[ "$WIPE_BUILD" -eq 1 ]]; then
  # Only remove known build outputs inside SPC_DIR — never downloads/, never WORKDIR root.
  # Docker runs as root, so buildroot/source are often root-owned; use sudo rm only then.
  echo "==> wiping build intermediates (keeping downloads/)"
  for d in buildroot source; do
    target="${SPC_DIR}/${d}"
    case "$target" in
      "${SPC_DIR}"/*) ;;
      *)
        echo "refusing to remove unexpected path: $target" >&2
        exit 1
        ;;
    esac
    if [[ -e "$target" ]]; then
      if [[ -w "$target" ]]; then
        run rm -rf "$target"
      else
        echo "    $d is not writable (likely root-owned from Docker); using sudo rm -rf"
        run sudo rm -rf "$target"
      fi
    fi
  done
fi

echo "==> building PHP $PHP (cli + fpm)"
run ./bin/spc-alpine-docker build \
  "$EXT_LIST" \
  --build-cli \
  --build-fpm \
  --debug

OUT_DIR="${WORKDIR}/dist"
run mkdir -p "$OUT_DIR"

CLI_OUT="${OUT_DIR}/php-${PHP}-cli-linux-x86_64"
FPM_OUT="${OUT_DIR}/php-${PHP}-fpm-linux-x86_64"

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "==> dry-run complete"
  exit 0
fi

test -x buildroot/bin/php
test -x buildroot/bin/php-fpm
cp -f buildroot/bin/php "$CLI_OUT"
cp -f buildroot/bin/php-fpm "$FPM_OUT"
chmod 755 "$CLI_OUT" "$FPM_OUT"

echo "==> smoke test"
"$CLI_OUT" -v
PHP_PATCH="$("$CLI_OUT" -v | awk 'NR==1 {print $2}')"
case "$PHP_PATCH" in
  "${PHP}".*) ;;
  *)
    echo "expected PHP ${PHP}.x, got ${PHP_PATCH}" >&2
    exit 1
    ;;
esac

MODS="$("$CLI_OUT" -m)"
for want in mysqli pdo_mysql pdo_pgsql pdo_sqlite; do
  echo "$MODS" | grep -qx "$want" || {
    echo "missing extension: $want" >&2
    echo "$MODS" >&2
    exit 1
  }
done
if [[ "$PHP" != "7.0" ]]; then
  echo "$MODS" | grep -qx sodium || {
    echo "missing extension: sodium" >&2
    echo "$MODS" >&2
    exit 1
  }
fi

echo
echo "OK: PHP ${PHP_PATCH}"
echo "  cli: $CLI_OUT"
echo "  fpm: $FPM_OUT"
echo
echo "Tip: after editing patch-spc-for-php7.sh, re-run with --wipe-build"
echo "     (and usually --skip-download) for a faster iteration."
