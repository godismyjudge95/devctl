# Xdebug spike (plan item 2.2)

Date: 2026-11-08. Binaries inspected: release `php-binaries-20261007.1` (static-php-cli 2.8.5, PHP 8.4.23).

## Result

| Platform | Shared `xdebug.so` | Static Xdebug | Path chosen |
|---|---|---|---|
| macOS aarch64 | Works | Needs php-src and Xdebug patches | Ship `xdebug.so` next to each PHP version |
| Linux x86_64 | Impossible (static musl) | Needs php-src and Xdebug patches, unproven | Not in this change. Toggle reports "not available" |

## 1. Can the current builds load a shared extension?

### macOS: yes

```sh
gh release download php-binaries-20261007.1 -R godismyjudge95/devctl -p php-8.4-cli-macos-aarch64
otool -L php-8.4-cli-macos-aarch64        # libSystem, libresolv, libc++, 3 frameworks. Dynamic, so dyld is present.
./php-8.4-cli-macos-aarch64 -n -i | grep -i dynamic
# Dynamic Library Support => enabled
./php-8.4-cli-macos-aarch64 -n -d zend_extension=/nonexistent.so -r 'echo 1;'
# Failed loading /nonexistent.so:  dlopen(/nonexistent.so, 0x0009): tried: '/nonexistent.so' (no such file) ...
```

The error comes from a real `dlopen` call, so dyld tried to load the file.

The binary also exports the Zend API that an extension links against at load time.
`nm -gU` lists 1011 exported `_zend_*` symbols, including `_zend_register_extension` and `_zend_execute_ex`.

Cross-check against a real Xdebug build (Homebrew bottle `shivammathur/extensions/xdebug@8.4` 3.5.3, `arm64_tahoe`):

- `otool -L xdebug.so` lists only `/usr/lib/libSystem.B.dylib` and `/usr/lib/libz.1.dylib`.
- All 111 `_zend_*`, `_php_*`, and `_executor_globals`-style symbols that `xdebug.so` imports (`nm -u`) are exported by the devctl binary. The 79 symbols that the binary does not export are all libc or zlib (`_stat`, `_glob`, `_flock`, ...).
- Both build IDs match: `API20240924,NTS` and `API420240924,NTS` appear in both files.

Not done: I did not load that `xdebug.so` into the binary. The session's permission policy blocked running third-party code. The owner can run it:

```sh
./php-8.4-cli-macos-aarch64 -n -d zend_extension=$PWD/xdebug.so -v   # expect "with Xdebug v3.5.3"
```

static-php-cli documents this route for macOS: "When using Linux/glibc or macOS, you can compile Xdebug as a shared extension using `--build-shared="xdebug"`" ([extension notes, 2.8.5](https://github.com/crazywhalecc/static-php-cli/blob/2.8.5/docs/en/guide/extension-notes.md#xdebug)).

### Linux: no

The Linux rows build with `./bin/spc-alpine-docker` (Alpine, musl).

```sh
gh release download php-binaries-20261007.1 -R godismyjudge95/devctl -p php-8.4-cli-linux-x86_64
file php-8.4-cli-linux-x86_64
# ELF 64-bit LSB executable, x86-64, statically linked, stripped
strings php-8.4-cli-linux-x86_64 | grep 'Dynamic loading not supported'
# Dynamic loading not supported      (musl's dlopen stub in a static binary)
```

Two separate blockers. A static musl binary has no dynamic loader, so `dlopen` always fails. The binary is also stripped, so a `.so` would have no Zend symbols to bind to.

spc refuses the combination outright ([`BuildPHPCommand.php`, 2.8.5, line 68](https://github.com/crazywhalecc/static-php-cli/blob/2.8.5/src/SPC/command/BuildPHPCommand.php#L68)): "Linux does not support dynamic extension loading with fully static builds, please build with a shared C runtime target!"

Commands to confirm in the Linux VM (x86_64, or arm64 with x86_64 emulation, because only `linux-x86_64` binaries ship):

```sh
PHPBIN=~/ddev/sites/server/php/8.4/php      # devctl CLI binary for 8.4
file "$PHPBIN"                               # expect: statically linked, stripped
"$PHPBIN" -n -i | grep -i 'dynamic library'
"$PHPBIN" -n -d zend_extension=/nonexistent.so -r 'echo "ok\n";' 2>&1
# expect: Failed loading /nonexistent.so:  Dynamic loading not supported
#         (not "No such file", which would mean dlopen ran)
touch /tmp/empty.so && "$PHPBIN" -n -d zend_extension=/tmp/empty.so -r 'echo "ok\n";' 2>&1
# expect the same "Dynamic loading not supported"
```

## 2. Can spc build Xdebug statically, like pcov?

Not by flipping the target. pcov works static because it is a normal PHP module. Xdebug is a Zend extension, and its startup only runs when PHP loads it through `zend_extension=`.

spc 2.8.5 config ([`config/ext.json`](https://github.com/crazywhalecc/static-php-cli/blob/2.8.5/config/ext.json)):

```json
"xdebug": { "type": "external", "source": "xdebug", "target": ["shared"],
            "support": { "Darwin": "partial", "Linux": "partial" }, "zend-extension": true }
```

spc's note says "Xdebug is only buildable as a shared extension."

Xdebug 3.5.3 source ([`xdebug.c`](https://github.com/xdebug/xdebug/blob/3.5.3/xdebug.c)):

- `xdebug_zend_startup()` (line 789) sets `zend_xdebug_initialised = 1`, runs `xdebug_library_zend_startup()` and `xdebug_debugger_zend_startup()`, and then calls `zend_startup_module(&xdebug_module_entry)` itself.
- `PHP_MINIT_FUNCTION(xdebug)` (line 602) warns `Xdebug MUST be loaded as a Zend extension` when that startup did not run.
- The step debugger needs `zend_extension_entry.statement_handler = xdebug_statement_call` (line 842). PHP only calls it for a registered Zend extension.

PHP runs startup in this order ([`main/main.c`, 8.4.23, lines 2271 to 2292](https://github.com/php/php-src/blob/php-8.4.23/main/main.c#L2271-L2292)): internal (static) modules, then `zend_extension=` entries from php.ini, then all module MINITs, then Zend extension startups. A static Xdebug runs only as a module. Its Zend extension half never starts, so breakpoints never fire.

Also, Xdebug's `config.m4` (line 17) calls `${PHP_CONFIG} --vernum`, which only exists for phpize builds. pcov had the same problem and needed the in-tree fix in `scripts/patch-spc-for-php8.sh`.

A static build is possible in principle. spc already does it for OPcache with phpmicro's [`static_opcache_84.patch`](https://github.com/static-php/phpmicro/blob/master/patches/static_opcache_84.patch). That patch drops the module from the static module list (`build/order_by_dep.awk`), renames its `zend_extension_entry` symbol, and calls `zend_register_extension()` from `main.c` before modules start. Xdebug would need the same three changes, plus a rename of `extension_version_info`, which OPcache also defines. That is a custom patch on php-src and Xdebug for each PHP minor (8.0 to 8.5). I could not compile or run it here, so it is not part of this change.

## 3. Cost of Xdebug that is loaded but off

[Xdebug settings, `xdebug.mode`](https://xdebug.org/docs/all_settings#mode): "off: Nothing is enabled. Xdebug does no work besides checking whether functionality is enabled. Use this setting if you want close to 0 overhead."

The source agrees. `PHP_RINIT_FUNCTION(xdebug)` returns at once when the mode is off (`xdebug.c` line 655).

Any other mode costs every request, even with `start_with_request=trigger` and no trigger. RINIT always adds `ZEND_COMPILE_EXTENDED_STMT` (line 691), so every compiled statement gets an extra opcode and the statement handler runs. "On" therefore slows normal requests a little. That is why the toggle exists and why the plan's stretch (a second FPM pool for `XDEBUG_SESSION` requests) is the real fix for "always on".

`xdebug.mode` is a startup setting. It cannot be set with `php_admin_value` in an FPM pool (same docs page). The toggle must write php.ini and restart the pool.

On macOS, "off" means devctl removes the `zend_extension` line, so Xdebug is not loaded at all and costs nothing.

## Decision

- macOS: build `xdebug.so` with `--build-shared=xdebug` in the macOS rows of `build-php.yml`, ship it as `php-<minor>-xdebug-macos-aarch64.so`, list it in `php-binaries.json`, and download it with the PHP version.
- devctl: the toggle writes `zend_extension`, `xdebug.mode`, and `xdebug.start_with_request=trigger` into the php.ini managed block for one version and restarts that pool. It reports "not available in this PHP build" when `xdebug.so` is missing (all current installs, and Linux).
- Linux: follow-up. Prove the static patch with `scripts/local-build-php8.sh` first, or move Linux to a glibc (shared C runtime) target. Both change every Linux PHP binary, so the owner decides.

## Not proven yet (check on the first release with Xdebug)

- The macOS `--build-shared=xdebug` build in `build-php.yml` has not run. The workflow step fails the row when `xdebug.so` does not load (`php -n -d zend_extension=… -m | grep -ix xdebug`). Prove it first with `scripts/local-build-php8-macos.sh 8.4`.
- Load order with OPcache. Xdebug's [install docs](https://xdebug.org/docs/install) say Xdebug must load after OPcache. spc's static OPcache patch registers OPcache after the php.ini `zend_extension` lines, so Xdebug comes first. PHP 8.5 (OPcache always static) has the same order, and Xdebug 3.5 supports 8.5, so I expect it to work (inferred, not tested). Check breakpoints with `opcache.enable=1` on the first build.
- `spc --build-shared` turns on the embed SAPI build (`BUILD_TARGET_EMBED`, [`BuildPHPCommand.php` line 275](https://github.com/crazywhalecc/static-php-cli/blob/2.8.5/src/SPC/command/BuildPHPCommand.php#L275)). That adds build time to the macOS rows.

## Follow-up: Linux on glibc

The owner chose the shared C runtime route and accepted a recent-distro floor. The Linux 8.x rows of `build-php.yml` now run `./bin/spc` natively on `ubuntu-24.04` with `SPC_LIBC=glibc`. In spc 2.8.5 that selects `GccNativeToolchain` ([`ToolchainManager.php`](https://github.com/crazywhalecc/static-php-cli/blob/2.8.5/src/SPC/toolchain/ToolchainManager.php)), so `SPCTarget::isStatic()` is false ([`SPCTarget.php`](https://github.com/crazywhalecc/static-php-cli/blob/2.8.5/src/SPC/util/SPCTarget.php)) and `--build-shared=xdebug` is allowed. glibc and libstdc++ link dynamically. Every other library stays static. The binaries need the runner's glibc (2.39) or newer. PHP 7.x is unchanged (static musl, no Xdebug). `scripts/prove-glibc-xdebug.sh` builds a small PHP 8.4 the same way and checks that `xdebug.so` loads.
