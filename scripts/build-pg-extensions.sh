#!/usr/bin/env bash
# Build TimescaleDB, pgvector, and pg_clickhouse against the exact PostgreSQL
# build devctl downloads, package them, then prove the package loads in a
# fresh copy of that PostgreSQL.
#
# Usage: scripts/build-pg-extensions.sh <platform> <out-dir>
#   platform: linux-x86_64 | linux-aarch64 | macos-aarch64
#
# Versions come from the environment (set in build-pg-extensions.yml):
#   PG_VERSION TIMESCALEDB_VERSION PGVECTOR_VERSION PG_CLICKHOUSE_VERSION
#
# The package is pg-extensions-pg${PG_VERSION}-${platform}.tar.gz:
#   lib/        files for pg_config --pkglibdir
#   extension/  files for pg_config --sharedir /extension
set -euo pipefail

PLATFORM="${1:?platform}"
OUT="$(mkdir -p "${2:?out-dir}" && cd "$2" && pwd)"
: "${PG_VERSION:?}" "${TIMESCALEDB_VERSION:?}" "${PGVECTOR_VERSION:?}" "${PG_CLICKHOUSE_VERSION:?}"
PG_MAJOR="${PG_VERSION%%.*}"
ASSET="pg-extensions-pg${PG_VERSION}-${PLATFORM}.tar.gz"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"

case "$PLATFORM" in
  linux-x86_64 | linux-aarch64) OS=linux ;;
  macos-aarch64) OS=darwin ;;
  *) echo "unknown platform $PLATFORM" >&2; exit 1 ;;
esac

fetch() {
  curl -fsSL --retry 5 --retry-delay 3 -o "$2" "$1"
}

ncpu() {
  if [ "$OS" = darwin ]; then sysctl -n hw.ncpu; else nproc; fi
}

# extract_pg <dest>: unpack the PostgreSQL build devctl installs.
extract_pg() {
  local dest="$1"
  mkdir -p "$dest"
  if [ "$OS" = linux ]; then
    local tmp
    tmp="$(mktemp -d)"
    tar -xzf pg.tar.gz -C "$tmp" "./percona-postgresql${PG_MAJOR}" 2>/dev/null ||
      tar -xzf pg.tar.gz -C "$tmp" "percona-postgresql${PG_MAJOR}"
    cp -a "$tmp/percona-postgresql${PG_MAJOR}/." "$dest/"
    rm -rf "$tmp"
  else
    local tmp
    tmp="$(mktemp -d)"
    unzip -q pg.zip -d "$tmp"
    cp -a "$tmp/pgsql/." "$dest/"
    rm -rf "$tmp"
  fi
}

echo "==> download PostgreSQL ${PG_VERSION} (${PLATFORM})"
if [ "$OS" = linux ]; then
  arch="${PLATFORM#linux-}"
  fetch "https://downloads.percona.com/downloads/postgresql-distribution-${PG_MAJOR}/${PG_VERSION}/binary/tarball/percona-postgresql-${PG_VERSION}-ssl3-linux-${arch}.tar.gz" pg.tar.gz
else
  fetch "https://get.enterprisedb.com/postgresql/postgresql-${PG_VERSION}-1-osx-binaries.zip" pg.zip
fi

BUILD_PG="$WORK/build-pg"
extract_pg "$BUILD_PG"
PG_CONFIG="$BUILD_PG/bin/pg_config"
MAKE_ARGS=()
CMAKE_ARGS=()

if [ "$OS" = darwin ]; then
  # EDB pg_config names MacOSX14.sdk and /opt/local paths from its own build
  # host. Point -isysroot at the runner SDK.
  SDK="$(xcrun --show-sdk-path)"
  mkdir -p "$WORK/shim"
  cat >"$WORK/shim/pg_config" <<EOF
#!/bin/sh
'$BUILD_PG/bin/pg_config' "\$@" | /usr/bin/sed 's|-isysroot [^ ]*|-isysroot $SDK|g'
EOF
  chmod 755 "$WORK/shim/pg_config"
  PG_CONFIG="$WORK/shim/pg_config"
  MAKE_ARGS+=("PG_SYSROOT=$SDK")
  CMAKE_ARGS+=("-DCMAKE_OSX_SYSROOT=$SDK" "-DCMAKE_OSX_ARCHITECTURES=arm64")
  export SDKROOT="$SDK"
  # pg_clickhouse links OpenSSL, lz4, and zstd. Use the copies EDB ships.
  export CPATH="$BUILD_PG/include"
  export LIBRARY_PATH="$BUILD_PG/lib"
fi

PKGLIBDIR="$("$PG_CONFIG" --pkglibdir)"
EXTDIR="$("$PG_CONFIG" --sharedir)/extension"
echo "pkglibdir: $PKGLIBDIR"
echo "extension: $EXTDIR"

# make install sets a new mtime. The extracted tree keeps the archive mtimes.
touch "$WORK/marker"
sleep 1

echo "==> pgvector ${PGVECTOR_VERSION}"
fetch "https://github.com/pgvector/pgvector/archive/refs/tags/v${PGVECTOR_VERSION}.tar.gz" pgvector.tar.gz
mkdir pgvector && tar -xzf pgvector.tar.gz -C pgvector --strip-components=1
# OPTFLAGS="" keeps the build portable. Percona's own vector.so uses
# -march=native and crashes on CPUs without AVX-512.
make -C pgvector -j"$(ncpu)" PG_CONFIG="$PG_CONFIG" OPTFLAGS="" ${MAKE_ARGS[@]+"${MAKE_ARGS[@]}"}
make -C pgvector install PG_CONFIG="$PG_CONFIG" OPTFLAGS="" ${MAKE_ARGS[@]+"${MAKE_ARGS[@]}"}

echo "==> TimescaleDB ${TIMESCALEDB_VERSION}"
fetch "https://github.com/timescale/timescaledb/archive/refs/tags/${TIMESCALEDB_VERSION}.tar.gz" timescaledb.tar.gz
mkdir timescaledb && tar -xzf timescaledb.tar.gz -C timescaledb --strip-components=1
(
  cd timescaledb
  # USE_OPENSSL=OFF: no libssl dependency. OpenSSL only serves telemetry.
  ./bootstrap -DCMAKE_BUILD_TYPE=Release -DPG_CONFIG="$PG_CONFIG" \
    -DREGRESS_CHECKS=OFF -DTAP_CHECKS=OFF -DWARNINGS_AS_ERRORS=OFF -DLINTER=OFF \
    -DUSE_OPENSSL=OFF ${CMAKE_ARGS[@]+"${CMAKE_ARGS[@]}"}
  if [ "$OS" = darwin ]; then
    { grep -rlE '/Library/Developer/CommandLineTools/SDKs/MacOSX[0-9][^/[:space:]"]*\.sdk' build || true; } | while read -r f; do
      /usr/bin/sed -i '' -E "s#/Library/Developer/CommandLineTools/SDKs/MacOSX[0-9][^/[:space:]\"]*\.sdk#$SDK#g" "$f"
    done
  fi
  make -C build -j"$(ncpu)"
  make -C build install
)

echo "==> pg_clickhouse ${PG_CLICKHOUSE_VERSION}"
git clone --quiet --depth 1 --recurse-submodules --branch "v${PG_CLICKHOUSE_VERSION}" \
  https://github.com/ClickHouse/pg_clickhouse.git pg_clickhouse
make -C pg_clickhouse -j"$(ncpu)" PG_CONFIG="$PG_CONFIG" ${MAKE_ARGS[@]+"${MAKE_ARGS[@]}"}
make -C pg_clickhouse install PG_CONFIG="$PG_CONFIG" ${MAKE_ARGS[@]+"${MAKE_ARGS[@]}"}


echo "==> package $ASSET"
STAGE="$WORK/stage"
mkdir -p "$STAGE/lib" "$STAGE/extension"
rel_pkglib="${PKGLIBDIR#"$BUILD_PG"/}"
rel_ext="${EXTDIR#"$BUILD_PG"/}"
(cd "$BUILD_PG" && find . -newer "$WORK/marker" \( -type f -o -type l \)) | sed 's#^\./##' | while read -r f; do
  case "$f" in
    "$rel_pkglib"/*) dest="$STAGE/lib/${f#"$rel_pkglib"/}" ;;
    "$rel_ext"/*) dest="$STAGE/extension/${f#"$rel_ext"/}" ;;
    *) echo "skip $f"; continue ;;
  esac
  mkdir -p "$(dirname "$dest")"
  cp -a "$BUILD_PG/$f" "$dest"
done

shlib=so
[ "$OS" = darwin ] && shlib=dylib

# Each library may only link the OS, or libraries next to PostgreSQL.
for lib in "$STAGE"/lib/*."$shlib"; do
  if [ "$OS" = darwin ]; then
    for dep in $(otool -L "$lib" | tail -n +2 | awk '{print $1}'); do
      case "$dep" in
        */*) ;;
        *) continue ;;
      esac
      case "$dep" in
        /usr/lib/* | /System/*) ;;
        @loader_path/*) ;;
        */lib/*.dylib)
          base="$(basename "$dep")"
          [ -f "$BUILD_PG/lib/$base" ] || { echo "$lib links $dep, which PostgreSQL does not ship" >&2; exit 1; }
          # EDB ids are absolute (/Library/PostgreSQL/<major>/lib). The
          # extension lives in lib/postgresql, so the copy is one level up.
          install_name_tool -change "$dep" "@loader_path/../$base" "$lib"
          ;;
        *) echo "$lib links $dep" >&2; exit 1 ;;
      esac
    done
    codesign --force --sign - "$lib"
    otool -L "$lib"
  else
    # pg_clickhouse needs libcurl.so.4. Percona ships it next to the
    # extensions, so a minimal system without libcurl still loads it.
    patchelf --set-rpath '$ORIGIN' "$lib"
    readelf -d "$lib" | grep -E 'NEEDED|RUNPATH'
    for dep in $(readelf -d "$lib" | awk '/NEEDED/ {gsub(/[\[\]]/, "", $5); print $5}'); do
      case "$dep" in
        libc.so.* | libm.so.* | libpthread.so.* | libdl.so.* | librt.so.* | libgcc_s.so.* | libstdc++.so.*) ;;
        libssl.so.3 | libcrypto.so.3 | liblz4.so.1 | libzstd.so.1 | libuuid.so.1 | libz.so.1) ;;
        *)
          [ -e "$BUILD_PG/lib/$dep" ] || { echo "$lib needs $dep, which is not in the allowed list" >&2; exit 1; }
          ;;
      esac
    done
    objdump -T "$lib" | grep -o 'GLIBC_[0-9.]*' | sort -uV | tail -1 | sed "s#^#$(basename "$lib") needs #"
  fi
done

tar -czf "$OUT/$ASSET" -C "$STAGE" lib extension
tar -tzf "$OUT/$ASSET"

echo "==> smoke test in a fresh PostgreSQL"
FRESH="$WORK/fresh-pg"
extract_pg "$FRESH"
FRESH_PKGLIB="$FRESH/${rel_pkglib}"
FRESH_EXT="$FRESH/${rel_ext}"
tar -xzf "$OUT/$ASSET" -C "$WORK" lib extension
cp -a "$WORK/lib/." "$FRESH_PKGLIB/"
cp -a "$WORK/extension/." "$FRESH_EXT/"

DATA="$WORK/data"
PORT=55439
"$FRESH/bin/initdb" -D "$DATA" -U postgres --auth=trust --no-locale -E UTF8 >/dev/null
{
  echo "port = $PORT"
  echo "listen_addresses = '127.0.0.1'"
  echo "unix_socket_directories = '$WORK'"
  echo "shared_preload_libraries = 'timescaledb'"
  echo "timescaledb.telemetry_level = off"
} >>"$DATA/postgresql.conf"
# devctl starts postgres with no LD_LIBRARY_PATH or DYLD_LIBRARY_PATH.
env -u LD_LIBRARY_PATH -u DYLD_LIBRARY_PATH "$FRESH/bin/pg_ctl" -D "$DATA" -l "$WORK/pg.log" -w start || {
  cat "$WORK/pg.log"
  exit 1
}
psql_run() {
  "$FRESH/bin/psql" -h 127.0.0.1 -p "$PORT" -U postgres -d postgres -v ON_ERROR_STOP=1 -tA -c "$1"
}
status=0
psql_run "CREATE EXTENSION timescaledb" &&
  psql_run "CREATE TABLE m (t timestamptz NOT NULL, v double precision); SELECT create_hypertable('m', 't'); ALTER TABLE m SET (timescaledb.compress); SELECT count(*) FROM timescaledb_information.hypertables" &&
  psql_run "CREATE EXTENSION vector" &&
  psql_run "SELECT '[1,2,3]'::vector <-> '[3,2,1]'::vector" &&
  psql_run "CREATE EXTENSION pg_clickhouse" &&
  psql_run "SELECT extname || ' ' || extversion FROM pg_extension ORDER BY 1" || status=$?
"$FRESH/bin/pg_ctl" -D "$DATA" -m fast stop >/dev/null || true
if [ "$status" -ne 0 ]; then
  cat "$WORK/pg.log"
  exit "$status"
fi
echo "OK $OUT/$ASSET"
