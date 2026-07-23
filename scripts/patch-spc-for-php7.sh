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

# 4) OpenSSL 1.1.1 ships "LICENSE", not "LICENSE.txt". LicenseDumper fatals
#    after a successful PHP build if the path is wrong.
python3 - <<'PY'
import json
from pathlib import Path
p = Path("config/source.json")
d = json.loads(p.read_text())
lic = d.get("openssl", {}).get("license")
if isinstance(lic, dict) and lic.get("path") == "LICENSE.txt":
    lic["path"] = "LICENSE"
    p.write_text(json.dumps(d, indent=4) + "\n")
    print("openssl license path -> LICENSE")
else:
    print(f"openssl license unchanged: {lic}")
PY

# 4b) PHP 7.0/7.2 AC_CHECK_LIB(ssl, ...) only adds -lssl before LIBS, so static
#     libssl.a fails the configure link test (needs -lcrypto -lz). Put those on
#     SPC_CMD_VAR_PHP_CONFIGURE_LIBS. Do NOT install libssl.so GROUP linker
#     scripts: libzip's OpenSSL-enabled ossfuzz targets then pick them up and
#     fail with "cannot find -lz" (regression on php-binaries-20260723.14).
python3 - <<'PY'
from pathlib import Path
p = Path("src/SPC/util/GlobalEnvManager.php")
t = p.read_text()
old = "'SPC_CMD_VAR_PHP_CONFIGURE_LIBS' => '-ldl -lpthread -lm',"
new = "'SPC_CMD_VAR_PHP_CONFIGURE_LIBS' => '-lcrypto -lz -ldl -lpthread -lm',"
if "-lcrypto -lz -ldl" in t:
    print("SPC_CMD_VAR_PHP_CONFIGURE_LIBS already includes -lcrypto -lz")
elif old in t:
    p.write_text(t.replace(old, new, 1))
    print("patched SPC_CMD_VAR_PHP_CONFIGURE_LIBS for static OpenSSL")
else:
    print("WARNING: could not find SPC_CMD_VAR_PHP_CONFIGURE_LIBS default to patch")
PY

# 5) If license dump still fails for any source, don't abort a finished build.
#    Soften LicenseDumper to warn instead of throw on missing license files.
if [[ -f src/SPC/util/LicenseDumper.php ]]; then
  sed -i \
    's/throw new RuntimeException(sprintf('\''source \[%s\] license file \[%s\] not exist'\''/logger()->warning(sprintf('\''source [%s] license file [%s] not exist (continuing)'\''/' \
    src/SPC/util/LicenseDumper.php || true
  # If sed didn't match the exact string, inject a softer fallback by replacing
  # the RuntimeException for license missing with a warning+return.
  if grep -q 'license file .* not exist' src/SPC/util/LicenseDumper.php; then
    python3 - <<'PY'
from pathlib import Path
p = Path("src/SPC/util/LicenseDumper.php")
t = p.read_text()
# Make missing license non-fatal
t2 = t.replace(
    "throw new RuntimeException(sprintf('source [%s] license file [%s] not exist'",
    "logger()->warning(sprintf('source [%s] license file [%s] not exist; skipping'",
)
if t2 != t:
    # Need to close the throw as a statement; the original ends with ); 
    # after replacing throw with logger()->warning the rest of the sprintf args remain.
    # Also remove the exception so execution continues — add return after warning if needed.
    t2 = t2.replace(
        "logger()->warning(sprintf('source [%s] license file [%s] not exist; skipping', $source_name, $filename));",
        "logger()->warning(sprintf('source [%s] license file [%s] not exist; skipping', $source_name, $filename));\n            return;",
    )
    # handle multi-line throw forms
    import re
    t2 = re.sub(
        r"throw new RuntimeException\(\s*sprintf\(\s*'source \[%s\] license file \[%s\] not exist'\s*,\s*\$source_name\s*,\s*\$filename\s*\)\s*\);",
        "logger()->warning(sprintf('source [%s] license file [%s] not exist; skipping', $source_name, $filename));\n            return;",
        t2,
    )
    p.write_text(t2)
    print("softened LicenseDumper missing-file handling")
else:
    print("LicenseDumper already soft or pattern not found")
PY
  fi
fi

echo "---- alpine docker pins ----"
grep -nE 'ALPINE_FROM|php81|php82|cwcc-spc' bin/spc-alpine-docker | head -40
echo "---- PHP configure LIBS ----"
grep -n "SPC_CMD_VAR_PHP_CONFIGURE_LIBS" src/SPC/util/GlobalEnvManager.php | head -5
echo "---- openssl build tail ----"
tail -n 25 src/SPC/builder/linux/library/openssl.php
