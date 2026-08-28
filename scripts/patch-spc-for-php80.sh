#!/usr/bin/env bash
# Pin libraries so PHP 8.0.30 still compiles under static-php-cli 2.8.5.
#
# libxml2 2.12.10: current libxml2 (2.15) hits ATTRIBUTE_UNUSED in ext/libxml.
#   2.12 matches spc's spc_fix_libxml2_12_php80.patch.
# libxslt 1.1.39: current libxslt 1.1.45 requires libxml2 >= 2.15.1.
# ICU 70.1: current ICU 77 headers need C++17 features PHP 8.0 intl does not use.
# ImageMagick 7.1.2-30 + imagick 3.8.1: spc tracks unversioned PECL / latest
#   ImageMagick tarballs. A cache miss on CI can pull a combo that fails the
#   8.0 imagick compile. Pin the pair that compiled locally.
#
# Applied to the spc checkout (cwd or $1). Idempotent. 8.1–8.5 must not use this.
# static-php-cli already strips OPENSSL_SSLV23_PADDING for PHP < 8.1 before make.

set -euo pipefail

SPC_DIR="${1:-.}"
SRC="${SPC_DIR}/config/source.json"

if [[ ! -f "$SRC" ]]; then
  echo "patch-spc-for-php80: missing $SRC" >&2
  exit 1
fi

python3 - "$SRC" <<'PY'
from pathlib import Path
import json
import sys

path = Path(sys.argv[1])
data = json.loads(path.read_text())
pins = {
    "libxml2": {
        "url": "https://download.gnome.org/sources/libxml2/2.12/libxml2-2.12.10.tar.xz",
    },
    "libxslt": {
        "url": "https://download.gnome.org/sources/libxslt/1.1/libxslt-1.1.39.tar.xz",
    },
    "icu": {
        "url": "https://github.com/unicode-org/icu/releases/download/release-70-1/icu4c-70_1-src.tgz",
    },
    "imagemagick": {
        "url": "https://github.com/ImageMagick/ImageMagick/archive/refs/tags/7.1.2-30.tar.gz",
    },
    "ext-imagick": {
        "url": "https://pecl.php.net/get/imagick-3.8.1.tgz",
        "path": "php-src/ext/imagick",
        "filename": "imagick.tgz",
    },
}
changed = False
for name, extra in pins.items():
    cur = data.get(name, {})
    want_url = extra["url"]
    if cur.get("type") == "url" and cur.get("url") == want_url:
        if name != "ext-imagick" or (
            cur.get("path") == extra.get("path") and cur.get("filename") == extra.get("filename")
        ):
            print(f"{name} pin already applied")
            continue
    license_block = cur.get("license", {"type": "file", "path": "Copyright"})
    entry = {
        "type": "url",
        "url": want_url,
        "provide-pre-built": False,
        "license": license_block,
    }
    if "path" in extra:
        entry["path"] = extra["path"]
    if "filename" in extra:
        entry["filename"] = extra["filename"]
    data[name] = entry
    changed = True
    print(f"applied {name} pin")
if changed:
    path.write_text(json.dumps(data, indent=4) + "\n")
PY
