#!/usr/bin/env bash
# Prove that a glibc static-php-cli build loads a shared xdebug.so.
#
# Same mode as the Linux rows of .github/workflows/build-php.yml: static-php-cli
# 2.8.5 with SPC_LIBC=glibc (native gcc, glibc and libstdc++ dynamic, every
# other library static), PHP 8.4.23, and `--build-shared=xdebug` with Xdebug
# pinned to 3.5.3. Only the extension list is smaller, to keep the run short.
#
# Run as root on a disposable Ubuntu or Debian machine (x86_64 or aarch64).
# It installs build tools with apt and downloads the prebuilt spc binary.
#
#   sudo bash scripts/prove-glibc-xdebug.sh            # WORKDIR=/root/spc-glibc-proof
#   sudo WORKDIR=/tmp/p bash scripts/prove-glibc-xdebug.sh
#
# The last line is `PROOF: PASS` or `PROOF: FAIL`. The full log is
# $WORKDIR/proof.log; spc's own logs are in $WORKDIR/log/.

set -euo pipefail

SPC_VERSION=2.8.5
PHP_VERSION=8.4.23
XDEBUG_URL=https://pecl.php.net/get/xdebug-3.5.3.tgz
EXTS=ctype,opcache,phar,posix,tokenizer,zlib
WORKDIR="${WORKDIR:-/root/spc-glibc-proof}"

START=$(date +%s)
step() { echo; echo "==> [$(($(date +%s) - START))s] $*"; }
fail() { echo; echo "FAIL: $*"; exit 1; }
needed() { readelf -d "$1" | sed -n 's/.*(NEEDED).*\[\(.*\)\]/\1/p' | tr '\n' ' '; }

main() {
  trap 'echo "command failed (exit $?) at line $LINENO"' ERR
  trap '[ $? -eq 0 ] || echo "PROOF: FAIL"' EXIT

  [ "$(id -u)" -eq 0 ] || fail "run as root (apt-get install)"
  local arch
  case "$(uname -m)" in
    aarch64 | arm64) arch=aarch64 ;;
    x86_64 | amd64) arch=x86_64 ;;
    *) fail "unsupported arch $(uname -m)" ;;
  esac

  step "host: $(. /etc/os-release && echo "$PRETTY_NAME"), $(uname -m), $(ldd --version | head -1), $(nproc) CPUs, workdir $PWD"

  step "apt: build tools"
  export DEBIAN_FRONTEND=noninteractive
  for _ in $(seq 1 180); do
    pgrep -x apt-get >/dev/null || pgrep -x unattended-upgr >/dev/null || break
    sleep 5
  done
  ps -eo pid,etime,args | grep -E "apt|dpkg|unattended" | grep -v grep || true
  apt-get -o DPkg::Lock::Timeout=900 update -q
  apt-get -o DPkg::Lock::Timeout=900 install -y -q --no-install-recommends \
    autoconf automake autopoint binutils bison bzip2 ca-certificates cmake curl \
    file flex g++ gcc git libtool make patch re2c tar unzip xz-utils
  gcc --version | head -1

  step "spc $SPC_VERSION prebuilt binary (linux-$arch)"
  if [ ! -x ./spc ]; then
    curl -fsSL --retry 3 -o spc.tar.gz \
      "https://github.com/crazywhalecc/static-php-cli/releases/download/${SPC_VERSION}/spc-linux-${arch}.tar.gz"
    tar xzf spc.tar.gz spc
  fi
  ./spc --version

  export SPC_LIBC=glibc

  step "spc doctor --auto-fix"
  ./spc doctor --auto-fix

  step "spc download: PHP $PHP_VERSION, $EXTS, xdebug ($XDEBUG_URL)"
  ./spc download \
    --with-php="$PHP_VERSION" \
    --for-extensions="${EXTS},xdebug" \
    --custom-url "xdebug:${XDEBUG_URL}" \
    --retry=5

  step "spc build: cli + fpm, shared xdebug"
  ./spc build "$EXTS" --build-cli --build-fpm --build-shared=xdebug

  step "collect binaries"
  cp -f buildroot/bin/php buildroot/bin/php-fpm buildroot/modules/xdebug.so .
  file php php-fpm xdebug.so
  echo "php NEEDED:       $(needed php)"
  echo "php-fpm NEEDED:   $(needed php-fpm)"
  echo "xdebug.so NEEDED: $(needed xdebug.so)"
  echo "highest glibc symbol version: $(objdump -T php php-fpm xdebug.so | grep -o 'GLIBC_[0-9.]*' | sort -uV | tail -1)"
  case " $(needed php) " in
    *" libc.so.6 "*) ;;
    *) fail "php does not link glibc dynamically" ;;
  esac

  local x="$PWD/xdebug.so" out

  step "php -n -d zend_extension=$x -m"
  out=$(./php -n -d zend_extension="$x" -m)
  echo "$out"
  grep -ixq xdebug <<<"$out" || fail "xdebug missing from php -m"

  step "php -n -d zend_extension=$x -r 'var_dump(extension_loaded(\"xdebug\"), ini_get(\"xdebug.mode\"));'"
  out=$(./php -n -d zend_extension="$x" -r 'var_dump(extension_loaded("xdebug"), ini_get("xdebug.mode"));')
  echo "$out"
  # In develop mode Xdebug's var_dump adds a "Command line code:1:" line before each value.
  grep -qx 'bool(true)' <<<"$out" || fail 'extension_loaded("xdebug") is not true'
  grep -qx 'string(7) "develop"' <<<"$out" || fail 'xdebug.mode is not the default "develop"'

  step "php -v with Xdebug and OPcache"
  out=$(./php -n -d zend_extension="$x" -d opcache.enable_cli=1 -v)
  echo "$out"
  grep -q 'with Xdebug v3\.5\.3' <<<"$out" || fail "php -v does not report Xdebug v3.5.3"
  if grep -q 'with Zend OPcache' <<<"$out"; then echo "OPcache: loaded"; else echo "OPcache: not reported (not part of the proof)"; fi

  step "php-fpm -n -d zend_extension=$x -m"
  out=$(./php-fpm -n -d zend_extension="$x" -m)
  grep -ixq xdebug <<<"$out" || fail "xdebug does not load into php-fpm"
  echo "xdebug loads into php-fpm"

  step "done"
  echo "PROOF: PASS"
}

mkdir -p "$WORKDIR"
cd "$WORKDIR"
main 2>&1 | tee -a "$WORKDIR/proof.log"
exit "${PIPESTATUS[0]}"
