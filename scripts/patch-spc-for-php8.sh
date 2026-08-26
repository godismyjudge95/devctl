#!/usr/bin/env bash
# Patch static-php-cli 2.8.5 so the full PHP 8.x extension set still compiles.
#
# libaom: spc tracks git "main". Current main moved the ENABLE_EXAMPLES block,
# so libaom_posix_implict.patch fails on Alpine. Pin v3.12.1 (imagick/AVIF).
#
# libevent: spc's ghrel matcher currently lands on 2.2.2-alpha, which breaks
# pecl-event 3.1.4 (const char ** vs char **). Pin 2.1.12-stable.
#
# pcov: spc marks it shared-only (same as xdebug) and extracts it to source/pcov
# (phpize/.so). Fully static musl PHP cannot load .so modules, so allow a static
# compile and extract into php-src/ext/pcov. Upstream config.m4 calls php-config
# and overwrites PHP_VERSION; the added-patch script fixes that after extract.
# pcov.enabled defaults to 0.
#
# Applied to the spc checkout (cwd or $1). Idempotent.

set -euo pipefail

SPC_DIR="${1:-.}"
SRC="${SPC_DIR}/config/source.json"
EXT="${SPC_DIR}/config/ext.json"

if [[ ! -f "$SRC" ]]; then
  echo "patch-spc-for-php8: missing $SRC" >&2
  exit 1
fi
if [[ ! -f "$EXT" ]]; then
  echo "patch-spc-for-php8: missing $EXT" >&2
  exit 1
fi

python3 - "$SRC" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text()
changed = False

def replace_once(label, old, new):
    global text, changed
    if new in text:
        print(f"{label} already applied")
        return
    if old not in text:
        raise SystemExit(f"patch-spc-for-php8: could not find {label} in source.json")
    text = text.replace(old, new, 1)
    changed = True
    print(f"applied {label}")

replace_once(
    "libaom v3.12.1 pin",
    '    "libaom": {\n        "type": "git",\n        "rev": "main",',
    '    "libaom": {\n        "type": "git",\n        "rev": "v3.12.1",',
)
replace_once(
    "libevent 2.1.12 pin",
    '    "libevent": {\n        "type": "ghrel",\n        "repo": "libevent/libevent",\n        "match": "libevent.+\\\\.tar\\\\.gz",\n        "prefer-stable": true,',
    '    "libevent": {\n        "type": "url",\n        "url": "https://github.com/libevent/libevent/releases/download/release-2.1.12-stable/libevent-2.1.12-stable.tar.gz",',
)
replace_once(
    "pcov extract into php-src/ext",
    '    "pcov": {\n        "type": "url",\n        "url": "https://pecl.php.net/get/pcov",\n        "filename": "pcov.tgz",',
    '    "pcov": {\n        "type": "url",\n        "url": "https://pecl.php.net/get/pcov",\n        "path": "php-src/ext/pcov",\n        "filename": "pcov.tgz",',
)

if changed:
    path.write_text(text)
PY

python3 - "$EXT" <<'PY'
from pathlib import Path
import json
import sys

path = Path(sys.argv[1])
data = json.loads(path.read_text())
pcov = data.get("pcov")
if not isinstance(pcov, dict):
    raise SystemExit("patch-spc-for-php8: pcov missing from ext.json")
target = pcov.get("target", ["static", "shared"])
if "static" in target:
    print("pcov static already applied")
else:
    pcov["target"] = ["static", "shared"]
    path.write_text(json.dumps(data, indent=4) + "\n")
    print("applied pcov static target")
PY

# In-tree static pcov: do not call php-config (it does not exist yet) and do not
# overwrite PHP_VERSION (swoole configure needs it). Written into config/ so
# spc-alpine-docker mounts it (the spc tree root is not bind-mounted).
cat > "${SPC_DIR}/config/spc-pcov-static.php" <<'PHP'
<?php

declare(strict_types=1);

if (patch_point() !== 'after-exts-extract') {
    return;
}

$file = SOURCE_PATH . '/php-src/ext/pcov/config.m4';
if (!is_file($file)) {
    return;
}

$src = file_get_contents($file);
if ($src === false) {
    throw new RuntimeException("cannot read {$file}");
}
if (str_contains($src, 'PCOV_PHP_VERNUM')) {
    return;
}

$old = '  PHP_VERSION=$($PHP_CONFIG --vernum)';
$new = <<<'M4'
  dnl php-config is for phpize. In-tree static builds must use PHP_VERSION_ID
  dnl and must not overwrite PHP_VERSION (swoole configure needs it).
  if test -n "$PHP_VERSION_ID"; then
    PCOV_PHP_VERNUM=$PHP_VERSION_ID
  else
    PCOV_PHP_VERNUM=$($PHP_CONFIG --vernum)
  fi
M4;

if (!str_contains($src, $old)) {
    throw new RuntimeException('pcov config.m4: expected php-config version probe not found');
}
$src = str_replace($old, $new, $src);
$src = str_replace('test $PHP_VERSION', 'test "$PCOV_PHP_VERNUM"', $src);
if (file_put_contents($file, $src) === false) {
    throw new RuntimeException("cannot write {$file}");
}
logger()->info('patched pcov config.m4 for in-tree static compile');
PHP
echo "wrote ${SPC_DIR}/config/spc-pcov-static.php"

