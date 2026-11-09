#!/usr/bin/env bash
# Local (Docker) mirror of the GHA `package` Linux compile cells for PHP 8.0–8.5.
#
# Purpose: prove the custom extension set (plus sodium, spx, pcov, and the
# shared xdebug.so) before burning Actions minutes. GHA remains the publish
# path (release assets + php-binaries.json).
#
# Like CI, spc runs natively with SPC_LIBC=glibc, here inside an ubuntu:24.04
# container (the CI runner's glibc). All commands run in the container as your
# user, with WORKDIR mounted at the same path.
#
# PHP 8.0.30 compiles with scripts/patch-spc-for-php80.sh (older libxml2,
# libxslt, ICU, ImageMagick 7.1.2-30, imagick 3.8.1). It omits protobuf
# and opentelemetry (current PECL needs 8.1+).
#
# Safety:
#   - Does NOT touch host PHP, systemd, or `make install`
#   - Works only under a dedicated workdir (default: ~/ddev/spc-php8-local)
#   - Never deletes the workdir or downloads cache unless you pass --wipe-build
#     (which only removes spc build intermediates, not downloads/)
#   - Never force-resets an existing git checkout
#   - A workdir from the older Alpine (musl) builds needs --wipe-build once,
#     and `sudo chown -R "$USER" WORKDIR` if Docker left root-owned files
#
# Prerequisites: docker, git, bash
#
# Usage:
#   scripts/local-build-php8.sh 8.4
#   scripts/local-build-php8.sh 8.4.23
#   PHP=8.0 WORKDIR=~/ddev/spc-php8-local scripts/local-build-php8.sh
#
# Flags:
#   --skip-download   reuse existing downloads/ (faster re-builds)
#   --skip-doctor     skip spc doctor
#   --wipe-build      remove buildroot/ source/ build intermediates before build
#                     (keeps downloads/; refuses paths outside WORKDIR)
#   --debug           pass --debug to spc (huge gcc logs; default off)
#   --dry-run         print steps only

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PATCH_SCRIPT="${ROOT}/scripts/patch-spc-for-php8.sh"
PATCH_SCRIPT_80="${ROOT}/scripts/patch-spc-for-php80.sh"
# Keep in lockstep with .github/workflows/build-php.yml `package` compile cells.
# Override with PHP8_EXTS=... if needed; default comes from scripts/php8-exts.sh.
# shellcheck source=php8-exts.sh
source "${ROOT}/scripts/php8-exts.sh"

SPC_REF="${SPC_REF:-2.8.5}"
PHP8_EXTS="${PHP8_EXTS:-}"

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

DOCKER=(docker)
if [[ "$DRY_RUN" -eq 0 ]]; then
  need_cmd docker
  if ! docker info >/dev/null 2>&1; then
    if sudo docker info >/dev/null 2>&1; then
      DOCKER=(sudo docker)
      echo "==> docker needs sudo"
    else
      echo "docker is not usable (start the daemon, or fix /var/run/docker.sock permissions)" >&2
      exit 1
    fi
  fi
fi

if [[ -z "$PHP8_EXTS" ]]; then
  PHP8_EXTS="$(php8_exts_for "$MINOR")"
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
  echo "==> (dry-run) would cd $SPC_DIR, patch libaom pin, then doctor / download / build"
  echo "==> dry-run complete"
  exit 0
fi

cd "$SPC_DIR"

SPC_IMAGE="devctl-spc-glibc:ubuntu-24.04"
if ! "${DOCKER[@]}" image inspect "$SPC_IMAGE" >/dev/null 2>&1; then
  echo "==> building $SPC_IMAGE (first time)"
  "${DOCKER[@]}" build -t "$SPC_IMAGE" - <<'DOCKERFILE'
FROM ubuntu:24.04
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    autoconf automake autopoint binutils bison bzip2 ca-certificates cmake curl \
    flex g++ gcc git libtool make patch re2c tar unzip xz-utils \
 && rm -rf /var/lib/apt/lists/*
DOCKERFILE
fi

box() {
  "${DOCKER[@]}" run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp \
    -e SPC_LIBC=glibc -e GITHUB_TOKEN \
    -v "${WORKDIR}:${WORKDIR}" -w "$SPC_DIR" "$SPC_IMAGE" "$@"
}

spc() {
  box ./bin/php bin/spc "$@"
}

if [[ ! -x bin/php || ! -f vendor/autoload.php ]]; then
  echo "==> installing the spc PHP runtime and Composer deps (first time)"
  run box sh bin/setup-runtime
  run box ./bin/php bin/composer install --no-dev --classmap-authoritative --no-interaction --prefer-dist --no-plugins
fi

echo "==> patching static-php-cli for PHP 8.x (libaom pin, pcov static)"
run bash "$PATCH_SCRIPT" .

if [[ "$MINOR" == "8.0" ]]; then
  echo "==> pinning libxml2 2.12.10, libxslt 1.1.39, ICU 70.1, ImageMagick 7.1.2-30, and imagick 3.8.1 for PHP 8.0"
  run bash "$PATCH_SCRIPT_80" .
fi

if [[ "$SKIP_DOCTOR" -eq 0 ]]; then
  echo "==> spc doctor --auto-fix"
  run spc doctor --auto-fix
else
  echo "==> skipping doctor (--skip-doctor)"
fi

if [[ "$SKIP_DOWNLOAD" -eq 0 ]]; then
  echo "==> downloading sources (cached under $SPC_DIR/downloads)"
  IGNORE_CACHE="php-src,libaom,libevent,pcov,swoole"
  if [[ "$MINOR" == "8.0" ]]; then
    IGNORE_CACHE="${IGNORE_CACHE},libxml2,libxslt,icu,imagemagick,ext-imagick"
  fi
  run spc download \
    --with-php="$PATCH" \
    --for-extensions="${PHP8_EXTS},xdebug" \
    --ignore-cache-sources="$IGNORE_CACHE" \
    --retry=5
else
  echo "==> skipping download (--skip-download); using existing downloads/"
fi

if [[ "$MINOR" == "8.0" ]]; then
  php8_neutralize_php80_stubs "$SPC_DIR/downloads"
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
BUILD_ARGS=("$PHP8_EXTS" --build-cli --build-fpm --build-shared=xdebug --with-added-patch=config/spc-pcov-static.php)
if [[ "$DEBUG" -eq 1 ]]; then
  BUILD_ARGS+=(--debug)
fi
run spc build "${BUILD_ARGS[@]}"

OUT_DIR="${WORKDIR}/dist"
run mkdir -p "$OUT_DIR"

CLI_OUT="${OUT_DIR}/php-${MINOR}-cli-linux-x86_64"
FPM_OUT="${OUT_DIR}/php-${MINOR}-fpm-linux-x86_64"
XDEBUG_OUT="${OUT_DIR}/php-${MINOR}-xdebug-linux-x86_64.so"

test -x buildroot/bin/php
test -x buildroot/bin/php-fpm
test -f buildroot/modules/xdebug.so
cp -f buildroot/bin/php "$CLI_OUT"
cp -f buildroot/bin/php-fpm "$FPM_OUT"
cp -f buildroot/modules/xdebug.so "$XDEBUG_OUT"
chmod 755 "$CLI_OUT" "$FPM_OUT"

echo "==> smoke test (in the container, so the host glibc does not matter)"
box "$CLI_OUT" -v
PHP_PATCH_GOT="$(box "$CLI_OUT" -v | awk 'NR==1 {print $2}')"
case "$PHP_PATCH_GOT" in
  "${MINOR}".*) ;;
  *)
    echo "expected PHP ${MINOR}.x, got ${PHP_PATCH_GOT}" >&2
    exit 1
    ;;
esac

MODS="$(box "$CLI_OUT" -m)"
# php -m uses canonical names (SPX, FFI); compare case-insensitively.
for want in mysqli sodium spx ffi openssl; do
  echo "$MODS" | grep -ixq "$want" || {
    echo "missing extension: $want" >&2
    echo "$MODS" >&2
    exit 1
  }
done
if [[ "$MINOR" != "8.0" ]]; then
  echo "$MODS" | grep -ixq pcov || {
    echo "missing extension: pcov" >&2
    echo "$MODS" >&2
    exit 1
  }
fi
if [[ "$MINOR" != "8.1" && "$MINOR" != "8.0" ]]; then
  echo "$MODS" | grep -ixq swoole || {
    echo "missing extension: swoole" >&2
    echo "$MODS" >&2
    exit 1
  }
fi

for bin in "$CLI_OUT" "$FPM_OUT"; do
  XMODS=$(box "$bin" -n -d zend_extension="$XDEBUG_OUT" -m)
  echo "$XMODS" | grep -ixq xdebug || {
    echo "xdebug.so does not load into $bin" >&2
    box "$bin" -n -d zend_extension="$XDEBUG_OUT" -v >&2
    exit 1
  }
done

echo
echo "OK: PHP ${PHP_PATCH_GOT} (mysqli, sodium, spx loaded; xdebug.so loads)"
echo "  cli:    $CLI_OUT"
echo "  fpm:    $FPM_OUT"
echo "  xdebug: $XDEBUG_OUT"
echo
echo "Tip: re-run with --wipe-build (and usually --skip-download) after a failed compile."
