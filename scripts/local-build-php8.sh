#!/usr/bin/env bash
# Local (Docker) mirror of the GHA `package` compile cells for PHP 8.1–8.5.
#
# Purpose: prove the common+sodium static build before burning Actions minutes.
# GHA remains the publish path (release assets + php-binaries.json).
#
# PHP 8.0.30 is accepted by this script but is known to fail under spc 2.8.5:
# current libxml2 breaks ext/libxml (ATTRIBUTE_UNUSED). CI rehosts 8.0 from
# static-php.dev common instead of compiling it.
#
# Safety:
#   - Does NOT touch host PHP, systemd, or `make install`
#   - Works only under a dedicated workdir (default: ~/ddev/spc-php8-local)
#   - Never deletes the workdir or downloads cache unless you pass --wipe-build
#     (which only removes spc build intermediates, not downloads/)
#   - Never force-resets an existing git checkout
#
# Prerequisites: docker (same as CI's spc-alpine-docker), git, bash
#
# Usage:
#   scripts/local-build-php8.sh 8.4
#   scripts/local-build-php8.sh 8.4.23
#   PHP=8.0 WORKDIR=~/ddev/spc-php8-local scripts/local-build-php8.sh
#
# Flags:
#   --skip-download   reuse existing downloads/ (faster re-builds)
#   --skip-doctor     skip spc-alpine-docker doctor
#   --wipe-build      remove buildroot/ source/ build intermediates before build
#                     (keeps downloads/; refuses paths outside WORKDIR)
#   --debug           pass --debug to spc (huge gcc logs; default off)
#   --dry-run         print steps only

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Keep in lockstep with .github/workflows/build-php.yml `package` job.
SPC_REF="${SPC_REF:-2.8.5}"
PHP8_EXTS="${PHP8_EXTS:-bcmath,bz2,calendar,ctype,curl,dom,exif,fileinfo,filter,ftp,gd,gmp,iconv,xml,mbstring,mbregex,mysqlnd,openssl,pcntl,pdo,pdo_mysql,pdo_sqlite,pdo_pgsql,pgsql,phar,posix,redis,session,simplexml,soap,sockets,sodium,sqlite3,tokenizer,xmlwriter,xmlreader,zlib,zip}"

declare -A PHP8_PATCH=(
  [8.0]=8.0.30
  [8.1]=8.1.34
  [8.2]=8.2.32
  [8.3]=8.3.32
  [8.4]=8.4.23
  [8.5]=8.5.8
)

PHP="${PHP:-}"
WORKDIR="${WORKDIR:-$HOME/ddev/spc-php8-local}"
SPC_DIR=""
SKIP_DOWNLOAD=0
SKIP_DOCTOR=0
WIPE_BUILD=0
DEBUG=0
DRY_RUN=0

usage() {
  sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage 0 ;;
    --skip-download) SKIP_DOWNLOAD=1; shift ;;
    --skip-doctor) SKIP_DOCTOR=1; shift ;;
    --wipe-build) WIPE_BUILD=1; shift ;;
    --debug) DEBUG=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    8.[0-5]|8.[0-5].*)
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
  echo "usage: $0 <8.0|8.1|8.2|8.3|8.4|8.5> [--skip-download] [--wipe-build]" >&2
  exit 2
fi

MINOR=""
PATCH=""
case "$PHP" in
  8.[0-5])
    MINOR="$PHP"
    PATCH="${PHP8_PATCH[$PHP]:-}"
    if [[ -z "$PATCH" ]]; then
      echo "unsupported PHP version: $PHP" >&2
      exit 2
    fi
    ;;
  8.[0-5].*)
    MINOR="${PHP%.*}"
    PATCH="$PHP"
    if [[ -z "${PHP8_PATCH[$MINOR]:-}" ]]; then
      echo "unsupported PHP version: $PHP" >&2
      exit 2
    fi
    ;;
  *)
    echo "unsupported PHP version: $PHP (want 8.0–8.5)" >&2
    exit 2
    ;;
esac

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

mkdir -p "$WORKDIR"
WORKDIR="$(cd "$WORKDIR" && pwd)"
SPC_DIR="${WORKDIR}/static-php-cli"

echo "==> workdir: $WORKDIR"
echo "==> php:     $MINOR (pin $PATCH)"
echo "==> spc:     $SPC_REF"
echo "==> exts:    $PHP8_EXTS"

if [[ ! -d "$SPC_DIR/.git" ]]; then
  echo "==> cloning crazywhalecc/static-php-cli @ $SPC_REF (first time)"
  run git clone --branch "$SPC_REF" --depth 1 \
    https://github.com/crazywhalecc/static-php-cli.git "$SPC_DIR"
else
  echo "==> reusing existing checkout: $SPC_DIR"
  if [[ "$DRY_RUN" -eq 0 ]]; then
    branch="$(git -C "$SPC_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
    describe="$(git -C "$SPC_DIR" describe --tags --always 2>/dev/null || true)"
    echo "    HEAD: ${branch} (${describe})"
    if [[ "$describe" != "$SPC_REF" && "$branch" != "$SPC_REF" && "$describe" != "v${SPC_REF}" ]]; then
      echo "WARNING: expected tag/branch $SPC_REF; continuing with existing tree (no reset)." >&2
    fi
  fi
fi

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "==> (dry-run) would cd $SPC_DIR and run doctor / download / build"
  echo "==> dry-run complete"
  exit 0
fi

cd "$SPC_DIR"

if [[ "$SKIP_DOCTOR" -eq 0 ]]; then
  echo "==> spc-alpine-docker doctor --auto-fix"
  run ./bin/spc-alpine-docker doctor --auto-fix
else
  echo "==> skipping doctor (--skip-doctor)"
fi

if [[ "$SKIP_DOWNLOAD" -eq 0 ]]; then
  echo "==> downloading sources (cached under $SPC_DIR/downloads)"
  run ./bin/spc-alpine-docker download \
    --with-php="$PATCH" \
    --for-extensions="$PHP8_EXTS" \
    --ignore-cache-sources=php-src \
    --retry=5
else
  echo "==> skipping download (--skip-download); using existing downloads/"
fi

if [[ "$WIPE_BUILD" -eq 1 ]]; then
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

echo "==> building PHP $PATCH (cli + fpm)"
BUILD_ARGS=("$PHP8_EXTS" --build-cli --build-fpm)
if [[ "$DEBUG" -eq 1 ]]; then
  BUILD_ARGS+=(--debug)
fi
run ./bin/spc-alpine-docker build "${BUILD_ARGS[@]}"

OUT_DIR="${WORKDIR}/dist"
run mkdir -p "$OUT_DIR"

CLI_OUT="${OUT_DIR}/php-${MINOR}-cli-linux-x86_64"
FPM_OUT="${OUT_DIR}/php-${MINOR}-fpm-linux-x86_64"

test -x buildroot/bin/php
test -x buildroot/bin/php-fpm
cp -f buildroot/bin/php "$CLI_OUT"
cp -f buildroot/bin/php-fpm "$FPM_OUT"
chmod 755 "$CLI_OUT" "$FPM_OUT"

echo "==> smoke test"
"$CLI_OUT" -v
PHP_PATCH_GOT="$("$CLI_OUT" -v | awk 'NR==1 {print $2}')"
case "$PHP_PATCH_GOT" in
  "${MINOR}".*) ;;
  *)
    echo "expected PHP ${MINOR}.x, got ${PHP_PATCH_GOT}" >&2
    exit 1
    ;;
esac

MODS="$("$CLI_OUT" -m)"
for want in sodium pdo_sqlite openssl; do
  echo "$MODS" | grep -qx "$want" || {
    echo "missing extension: $want" >&2
    echo "$MODS" >&2
    exit 1
  }
done

echo
echo "OK: PHP ${PHP_PATCH_GOT} (sodium loaded)"
echo "  cli: $CLI_OUT"
echo "  fpm: $FPM_OUT"
echo
echo "Tip: re-run with --wipe-build (and usually --skip-download) after a failed compile."
