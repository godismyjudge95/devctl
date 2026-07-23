#!/usr/bin/env bash
# Patch a checked-out static-php-cli 2.3.0 tree so PHP 7.x can build in 2026.
# Expected CWD: root of the static-php-cli checkout.
set -euo pipefail

# 1) Alpine image: edge + php82 packages are gone; pin 3.17 + php81.
sed -i \
  -e 's|ALPINE_FROM=alpine:edge|ALPINE_FROM=alpine:3.17|g' \
  -e 's|ALPINE_FROM=multiarch/alpine:aarch64-edge|ALPINE_FROM=alpine:3.17|g' \
  -e 's|ALPINE_FROM=multiarch/alpine:x86_64-edge|ALPINE_FROM=alpine:3.17|g' \
  -e 's/php82/php81/g' \
  bin/spc-alpine-docker
sed -i 's/cwcc-spc-/cwcc-spc-a317-/g' bin/spc-alpine-docker

# 2) OpenSSL 1.1.1 rejects Configure flag "no-legacy" (OpenSSL 3 only).
for f in \
  src/SPC/builder/linux/library/openssl.php \
  src/SPC/builder/unix/library/openssl.php \
  src/SPC/builder/macos/library/openssl.php
do
  if [[ -f "$f" ]]; then
    sed -i 's/no-legacy //g' "$f"
    echo "stripped no-legacy from $f"
  fi
done

# 3) OpenSSL 1.1.1 does not install cmake/OpenSSL/OpenSSLConfig.cmake.
#    spc 2.3.0 always FileSystem::readFile()'s it after make install_sw.
python3 - <<'PY'
from pathlib import Path
p = Path("src/SPC/builder/linux/library/openssl.php")
text = p.read_text()
lines = []
skip_next_block = False
brace_depth = 0
i = 0
src = text.splitlines(keepends=True)
while i < len(src):
    line = src[i]
    # Drop any statement that reads OpenSSLConfig.cmake via FileSystem
    if "OpenSSLConfig.cmake" in line and "FileSystem::" in line:
        # Skip this line; if it starts an if (...), also skip the block body
        if line.lstrip().startswith("if "):
            # skip until matching closing brace at same indent roughly
            depth = 0
            while i < len(src):
                l = src[i]
                depth += l.count("{") - l.count("}")
                i += 1
                if depth <= 0 and i > 0:
                    break
            lines.append("        // skipped OpenSSLConfig.cmake block (OpenSSL 1.1)\n")
            continue
        else:
            lines.append("        // skipped OpenSSLConfig.cmake line (OpenSSL 1.1)\n")
            i += 1
            continue
    lines.append(line)
    i += 1
p.write_text("".join(lines))
print("patched openssl.php cmake references")
PY

echo "---- alpine docker pins ----"
grep -nE 'ALPINE_FROM|php81|php82|cwcc-spc' bin/spc-alpine-docker | head -40
echo "---- openssl build tail ----"
tail -n 35 src/SPC/builder/linux/library/openssl.php
