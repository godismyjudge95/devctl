#!/usr/bin/env bash
# PHP 8.x static-php-cli extension lists.
# Sourced by scripts/local-build-php8.sh and .github/workflows/build-php.yml.
#
# This is the original custom compile set (before the static-php.dev "common"
# rehost dropped mysqli, spx, ffi, imagick, and the rest). sodium, spx, and
# pcov stay on every 8.x build. swoole requires PHP >= 8.2.
#
# 8.1 (no swoole) also compiles pdo_pgsql and pdo_sqlite. 8.2+ cannot: the
# swoole pgsql/sqlite hooks already provide those PDO drivers, and static-php-cli
# refuses the combination.
#
# PHP 8.0 compiles with this list (minus protobuf and opentelemetry:
# current PECL of those needs PHP 8.1+ IS_MIXED). 8.0.30 pins older
# libxml2/libxslt/icu via scripts/patch-spc-for-php80.sh.

php8_exts_for() {
  local minor="${1:?php8_exts_for: minor required}"
  local before="apcu,bcmath,brotli,bz2,calendar,ctype,curl,dba,dom,event,exif,ffi,fileinfo,filter,ftp,gd,gmp,iconv,imagick,intl,ldap,libxml,mbregex,mbstring,mysqli,mysqlnd,opcache,openssl,opentelemetry,pcntl,pcov,pdo,pdo_mysql,pgsql,phar,posix,protobuf,readline,redis,session,shmop,simplexml,soap,sockets,sodium,sqlite3"
  local after="sysvmsg,sysvsem,sysvshm,tokenizer,xml,xmlreader,xmlwriter,xsl,xz,zip,zlib,zstd,spx"
  case "$minor" in
    8.0)
      # Drop protobuf + opentelemetry (PECL needs PHP >= 8.1).
      printf '%s,pdo_pgsql,pdo_sqlite,%s\n' \
        "apcu,bcmath,brotli,bz2,calendar,ctype,curl,dba,dom,event,exif,ffi,fileinfo,filter,ftp,gd,gmp,iconv,imagick,intl,ldap,libxml,mbregex,mbstring,mysqli,mysqlnd,opcache,openssl,pcntl,pcov,pdo,pdo_mysql,pgsql,phar,posix,readline,redis,session,shmop,simplexml,soap,sockets,sodium,sqlite3" \
        "$after"
      ;;
    8.1)
      printf '%s,pdo_pgsql,pdo_sqlite,%s\n' "$before" "$after"
      ;;
    8.[2-5])
      printf '%s,swoole,swoole-hook-mysql,swoole-hook-pgsql,swoole-hook-sqlite,%s\n' "$before" "$after"
      ;;
    *)
      echo "php8_exts_for: unsupported PHP minor: $minor" >&2
      return 1
      ;;
  esac
}

php8_exts_must_include() {
  local list="$1"
  shift
  local want
  for want in "$@"; do
    case ",$list," in
      *",$want,"*) ;;
      *)
        echo "php8_exts: missing required extension: $want" >&2
        echo "list: $list" >&2
        return 1
        ;;
    esac
  done
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  for m in 8.0 8.1 8.2 8.3 8.4 8.5; do
    list="$(php8_exts_for "$m")"
    php8_exts_must_include "$list" mysqli sodium spx pcov ffi intl imagick openssl redis
    case "$m" in
      8.0 | 8.1)
        case ",$list," in
          *,swoole,*)
            echo "php8_exts: $m must not include swoole" >&2
            exit 1
            ;;
        esac
        php8_exts_must_include "$list" pdo_pgsql pdo_sqlite
        if [[ "$m" == "8.0" ]]; then
          case ",$list," in
            *,protobuf,* | *,opentelemetry,*)
              echo "php8_exts: 8.0 must not include protobuf/opentelemetry (PECL needs PHP 8.1+)" >&2
              exit 1
              ;;
          esac
        fi
        ;;
      *)
        php8_exts_must_include "$list" swoole swoole-hook-mysql
        case ",$list," in
          *,pdo_pgsql,* | *,pdo_sqlite,*)
            echo "php8_exts: $m must not list pdo_pgsql/pdo_sqlite (swoole hooks provide them)" >&2
            exit 1
            ;;
        esac
        ;;
    esac
  done
  echo "OK: php8-exts lists include mysqli, sodium, spx, pcov"
fi
