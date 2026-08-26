#!/usr/bin/env bash
# Pin libraries so PHP 8.0.30 still compiles under static-php-cli 2.8.5.
#
# libxml2 2.12.10: current libxml2 (2.15) hits ATTRIBUTE_UNUSED in ext/libxml.
#   2.12 matches spc's spc_fix_libxml2_12_php80.patch.
# libxslt 1.1.39: current libxslt 1.1.45 requires libxml2 >= 2.15.1.
# ICU 70.1: current ICU 77 headers need C++17 features PHP 8.0 intl does not use.
#
# Applied to the spc checkout (cwd or $1). Idempotent. 8.1–8.5 must not use this.

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
    "libxml2": "https://download.gnome.org/sources/libxml2/2.12/libxml2-2.12.10.tar.xz",
    "libxslt": "https://download.gnome.org/sources/libxslt/1.1/libxslt-1.1.39.tar.xz",
    "icu": "https://github.com/unicode-org/icu/releases/download/release-70-1/icu4c-70_1-src.tgz",
}
changed = False
for name, url in pins.items():
    cur = data.get(name, {})
    if cur.get("type") == "url" and cur.get("url") == url:
        print(f"{name} pin already applied")
        continue
    license_block = cur.get("license", {"type": "file", "path": "Copyright"})
    data[name] = {
        "type": "url",
        "url": url,
        "provide-pre-built": False,
        "license": license_block,
    }
    changed = True
    print(f"applied {name} pin")
if changed:
    path.write_text(json.dumps(data, indent=4) + "\n")
PY
