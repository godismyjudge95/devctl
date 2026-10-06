# Grounding: how devctl starts, supervises, installs, and networks processes (Linux today)

Explorers: supervisor aa8a240b, installers bc642880, elevate 3128f0bd, paths caaf3990.
Reconcile against source if they disagree. Skills under `.agents/skills/` are stale (they still say root daemon and systemctl-managed children).

## Process model

- systemd owns **only** `devctl.service`. `User=` site user. `AmbientCapabilities=CAP_NET_BIND_SERVICE`. `ExecStart=… daemon`. Default `KillMode=control-group`.
- `devctl daemon` refuses root (`main.go` `run()`).
- Every catalog service in `config.DefaultServices` is `Managed: true`. Children are `exec.CommandContext` under `services.Supervisor`. DNS is `RunFunc` in-process. PHP-FPM registered at runtime as `php-fpm-{ver}` with `--nodaemonize`.
- `Manager` still has a shell/`systemctl` branch for `Managed == false`. Nothing in defaults uses it.
- `install.enableAndStart` / `stopAndDisable` have **no callers**.
- Supervisor does **not** `Setpgid`. Stop cancels `CommandContext` (Go default = SIGKILL on Unix). No process-group kill. FPM workers are grandchildren.
- ClickHouse sets `CLICKHOUSE_WATCHDOG_ENABLE=0` so it does not double-fork.
- `ManagedUser` Credential drop is a no-op: daemon already is the site user. `CapabilityBoundingSet` is only `CAP_NET_BIND_SERVICE` (no CAP_SETUID).
- `reexec` (`syscall.Exec` same PID) does **not** stop children. Callers pass nil AfterStop (restart) or only tool refresh (update).
- `run()` does not join `supervisor.stopAll` after HTTP shutdown.

## Install

- Services download into `{serverRoot}/<id>/`. No `apt-get install <service>`.
- APT leftovers: `libreadline-dev` (postgres), `libnuma1` (mysql), `build-essential` (pgvector compile), `libnss3-tools` (NSS, mostly dead UI path).
- MySQL = Ubuntu 24.04 **amd64** `.deb` extract via `dpkg-deb`.
- Postgres = Percona **linux** tarball + Timescale/pg_clickhouse `.deb` extract. pgvector = source + `make`.
- Valkey = jammy/noble **x86_64** glibc tarball via `lsb_release`.
- Caddy/Mailpit/Meilisearch/Typesense/MaxIO = hardcoded `linux-amd64`. ClickHouse tarball `amd64` + **ELF magic** in `IsInstalled`.
- PHP assets parsed with suffix `-linux-x86_64` only. CI `build-php.yml` is linux-x86_64 Alpine SPC.
- CLI tools (`tools/`): linux-only URL strings (yq, mago, fnm, phpantom, sqlite3). Composer/WP-CLI phars are OS-neutral.
- Reverb = Composer Laravel app, not a binary.
- `runtime.GOOS` unused in install/tools/php. `GOARCH` only on postgres/timescale/pg_clickhouse.

## Elevate / network

- Helper ops: `install-resolver`, `uninstall-resolver`, `install-ca`, `uninstall-ca`, `write-unit`, `chown-tree`, `apt-install`, `systemctl`.
- Resolver: systemd-resolved drop-in `DNS=127.0.0.1:5354` `Domains=~test`. Skip if resolved missing. No `/etc/resolver`, no pf, no nft, no `/etc/hosts`.
- Embedded DNS listens `":"+port` (all interfaces). A records = LAN IPv4. Upstream from `/run/systemd/resolve/resolv.conf` then `/etc/resolv.conf`.
- Caddy child listens `:80` and `:443` (all ifaces) via inherited ambient cap. Admin API `localhost:2019`. Config is Admin API JSON, **no Caddyfile**.
- CA: `update-ca-certificates` / `update-ca-trust`. Caddy `HOME={caddyDir}` so internal CA is under server root.
- `POST /api/tls/trust` always 403 (daemon not root). Elevate trust does not update NSS.
- FastCGI: unix socket `{serverRoot}/php/{ver}/php-fpm.sock`.

## Paths

- All owned paths from `DEVCTL_SERVER_ROOT` via `paths` package.
- Fallback server root `{home}/ddev/sites/server`. `cli/skill.go` hardcodes `/home/` + SUDO_USER.
- `devctl open` reads `/etc/systemd/system/devctl.service` and calls `xdg-open`.
- Makefile `getent passwd`, unit dir `/etc/systemd/system`.
- Tests: Incus + systemd only. Artifact cache filenames are linux-amd64.

## Constraints for the macOS port (operator)

- Binary downloads only. No Homebrew. No install scripts. No compile on the machine.
- All service processes nested under the devctl parent. systemd/launchd start **only** the parent.
- Linux must keep working.
- Confirmed this session (HTTP HEAD): Caddy `mac_arm64.tar.gz`, Mailpit darwin-arm64, Meilisearch macos-apple-silicon, Typesense darwin-arm64, MaxIO macos-arm64, ClickHouse `builds.clickhouse.com/master/macos-aarch64/clickhouse`, MySQL `mysql-8.4.11-macos15-arm64.tar.gz`, EDB postgres `postgresql-18.4-1-osx-binaries.zip`. Valkey: no darwin vendor binary. PHP GitHub assets: linux-x86_64 only; static-php.dev has macos-aarch64 common builds.

## Defaults already chosen (autonomy)

- Linux: keep ambient cap, Caddy `:80/:443`. Darwin: pf rdr 80→8080, 443→8443; Caddy listens high ports.
- DNS: Darwin `/etc/resolver/<tld>`. Linux keep resolved drop-in. Both talk to in-process DNS :5354.
- CA: Darwin `security add-trusted-cert` system keychain.
- Parent: Darwin LaunchAgent (user session analog of the unit). Linux keep systemd.
- Skip Timescale/pgvector/Valkey on Darwin until a real binary exists.
- PHP Darwin: static-php.dev until php-binaries CI ships darwin assets.
