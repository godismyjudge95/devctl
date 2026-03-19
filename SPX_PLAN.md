# SPX Profiler — Implementation Plan

Reference this file while implementing. Check off each item as it is completed.

---

## Context

- devctl is a local PHP dev environment dashboard (Go backend + Vue 3 frontend)
- PHP binaries are **fully static** (musl libc, no dynamic extension loading)
- SPX must be compiled in at build time — it cannot be loaded as a `.so`
- SPX has **zero overhead when not activated** — load it globally per PHP version
- SPX activation is per-request via cookies (`SPX_ENABLED=1; SPX_KEY=dev`) or query params
- SPX data key: hardcoded `dev` (`spx.http_key=dev`)
- SPX data dir: per-PHP-version, `{serverRoot}/php/{ver}/spx-data/`
- Repository: `godismyjudge95/devctl` on GitHub

---

## Decisions Made

| Topic | Decision |
|---|---|
| PHP binary source | Custom builds published as devctl release assets |
| Release trigger | On devctl release publish (`release: types: [published]`) + `workflow_dispatch` |
| Architectures | x86_64 only |
| PHP versions | 8.1, 8.2, 8.3, 8.4 |
| SPX scope | Global per PHP version (no per-site FPM pools needed) |
| SPX activation | Per-request via cookies; DB flag controls UI visibility only |
| Static-php-cli variant | Official `crazywhalecc/static-php-cli` (supports `spx` natively via modified fork at `static-php/php-spx`) |
| CI approach | Copy upstream `build-unix.yml` pattern using `./bin/spc-alpine-docker` (no PHP needed on runner) |
| Binary format | Raw binaries (no tar), named `php-{ver}-cli-linux-x86_64` and `php-{ver}-fpm-linux-x86_64` |

---

## Extension List

```
apcu,bcmath,brotli,bz2,calendar,ctype,curl,dba,dom,event,exif,fileinfo,filter,ftp,gd,gmp,iconv,imagick,intl,ldap,libxml,mbregex,mbstring,mysqli,mysqlnd,opcache,openssl,opentelemetry,pcntl,pdo,pdo_mysql,pgsql,phar,posix,protobuf,readline,redis,session,shmop,simplexml,soap,sockets,sodium,sqlite3,swoole,swoole-hook-mysql,swoole-hook-pgsql,swoole-hook-sqlite,sysvmsg,sysvsem,sysvshm,tokenizer,xml,xmlreader,xmlwriter,xsl,xz,zip,zlib,zstd,spx
```

---

## SPX Profile File Format

SPX writes two files per profiling session to `spx.data_dir`:

- `{key}.json` — metadata (site, URL, wall time, memory, function count, timestamps, enabled metrics)
- `{key}.txt.gz` — gzip-compressed call trace (event log)

### Metadata JSON fields (relevant)

```json
{
  "_http_host": "mysite.test",
  "_http_method": "GET",
  "_http_request_uri": "/some/page",
  "wall_time_ms": 123.4,
  "peak_memory_usage": 8388608,
  "called_func_count": 142,
  "recorded_func_count": 142,
  "timestamp": 1710000000,
  "metrics": ["wt", "zm"]
}
```

### Call trace `.txt.gz` format

Newline-delimited. Each line:
```
+{depth} {func_name}   # function enter (with cumulative wall-time in last field)
-{depth} {func_name}   # function exit
```

Actual format from SPX source — each line is tab-separated:
```
{event_type}\t{depth}\t{func_name}\t{metric_value}...
```
Where `event_type` is `+` (enter) or `-` (exit).

---

## Files to Create / Modify

### Create
- [ ] `.github/workflows/build-php.yml` — CI workflow
- [ ] `api/spx.go` — Go backend handlers
- [ ] `frontend/src/views/SpxView.vue` — profile viewer UI
- [ ] `frontend/src/stores/spx.ts` — Pinia store

### Modify
- [ ] `php/installer.go` — update download URLs, remove resolveFullVersion
- [ ] `php/config.go` — add SPX ini settings in WriteConfigs
- [ ] `api/sites.go` — fix handleSPXEnable/Disable to rewrite config + restart FPM
- [ ] `api/server.go` — register SPX API routes
- [ ] `frontend/src/lib/api.ts` — add SPX types + API functions
- [ ] `frontend/src/router/index.ts` — add /spx route
- [ ] `frontend/src/App.vue` — add Profiler nav item (conditional on spx_enabled site)
- [ ] `README.md` — document SPX feature
- [ ] `CHANGELOG.md` — add unreleased entry
- [ ] `TODO.md` — move SPX from Backlog to Completed

---

## Step 1: `.github/workflows/build-php.yml`

Based on upstream `crazywhalecc/static-php-cli/.github/workflows/build-unix.yml`.

Key points:
- Triggers: `release: types: [published]` and `workflow_dispatch`
- Matrix: `{php: ["8.1","8.2","8.3","8.4"]}`
- Runner: `ubuntu-latest` (x86_64)
- Uses `./bin/spc-alpine-docker` wrapper — downloads static-php-cli, runs build inside Alpine Docker container (no PHP required on runner)
- `--prefer-pre-built` to reuse pre-built library tarballs (~20 min vs ~90 min)
- Builds both `--build-cli` and `--build-fpm`
- Downloads spc binary from `https://dl.static-php.dev/static-php-cli/spc-bin/nightly/spc-linux-x86_64`
- Source cache key: `spc-sources-${{ matrix.php }}-linux-x86_64`
- Uploads assets to release: `php-${{ matrix.php }}-cli-linux-x86_64` and `php-${{ matrix.php }}-fpm-linux-x86_64`

How `spc-alpine-docker` works:
- It's a shell script that mounts the current directory into an Alpine container and runs `./bin/spc` inside it
- We need to download `spc` itself (the PHP phar wrapped in a micro binary) from dl.static-php.dev
- The download + build commands follow the same pattern as the upstream workflow

Asset upload: use `softprops/action-gh-release@v2` with `files:` pointing to `buildroot/bin/php` and `buildroot/bin/php-fpm`, renamed to the target asset names.

---

## Step 2: `php/installer.go` changes

Remove:
- `const staticPHPIndex`
- `type staticPHPEntry`
- `func resolveFullVersion`
- `extractFromTar` usage for fpm/cli binaries (keep the function for now in case it's used elsewhere, or remove if unused)

Add:
```go
const (
    ghReleaseBase   = "https://github.com/godismyjudge95/devctl/releases/latest/download/"
    downloadTimeout = 10 * time.Minute
)
```

New `Install` flow (steps 3 and 4):
```go
// 3. Download FPM binary directly.
fpmURL := ghReleaseBase + "php-" + ver + "-fpm-linux-x86_64"
if err := curlDownload(ctx, fpmURL, fpmBin); err != nil { ... }
os.Chmod(fpmBin, 0755)

// 4. Download CLI binary directly.
cliURL := ghReleaseBase + "php-" + ver + "-cli-linux-x86_64"
if err := curlDownload(ctx, cliURL, cliBin); err != nil { ... }
os.Chmod(cliBin, 0755)
```

---

## Step 3: `php/config.go` — `WriteConfigs` SPX additions

In the `ini` template string, append:
```ini
; SPX profiler — loaded globally, zero-overhead until activated via cookies/query params
spx.http_enabled=1
spx.http_key=dev
spx.http_ip_whitelist=127.0.0.1
spx.data_dir=%s
```
Pass `spxDataDir` as `filepath.Join(phpDir, "spx-data")`.

In the `conf` (php-fpm.conf) `[www]` pool section, append:
```ini
php_admin_value[spx.data_dir] = {spxDataDir}
```

Also `os.MkdirAll(spxDataDir, 0755)` before writing files.

---

## Step 4: `api/sites.go` — fix SPX enable/disable

`handleSPXEnable`:
1. Get site from DB to read `php_version`
2. `s.queries.SetSiteSPX(...)` — set flag
3. If site has a `php_version`:
   - `php.WriteConfigs(ver, s.serverRoot, s.siteUser)` — regenerate ini
   - `s.supervisor.Restart(s.phpFPMServiceDef(ver))` — apply changes

`handleSPXDisable`: same pattern.

---

## Step 5: `api/spx.go` — profile API

### Routes to register in `api/server.go`
```
GET  /api/spx/profiles          — list profiles (query: ?domain=, ?ver=)
GET  /api/spx/profiles/{key}    — get full profile detail
DELETE /api/spx/profiles/{key}  — delete one profile
DELETE /api/spx/profiles        — clear profiles (query: ?domain=)
```

### SpxProfile struct (listing)
```go
type SpxProfile struct {
    Key              string  `json:"key"`
    PHPVersion       string  `json:"php_version"`
    Domain           string  `json:"domain"`
    Method           string  `json:"method"`
    URI              string  `json:"uri"`
    WallTimeMs       float64 `json:"wall_time_ms"`
    PeakMemoryBytes  int64   `json:"peak_memory_bytes"`
    CalledFuncCount  int     `json:"called_func_count"`
    Timestamp        int64   `json:"timestamp"`
}
```

### SpxProfileDetail struct (detail)
```go
type SpxProfileDetail struct {
    SpxProfile
    Functions []SpxFunction `json:"functions"`   // flat profile
    Events    []SpxEvent    `json:"events"`       // for flamegraph / timeline
}

type SpxFunction struct {
    Name          string  `json:"name"`
    Calls         int     `json:"calls"`
    InclusiveMs   float64 `json:"inclusive_ms"`
    ExclusiveMs   float64 `json:"exclusive_ms"`
    InclusivePct  float64 `json:"inclusive_pct"`
    ExclusivePct  float64 `json:"exclusive_pct"`
}

type SpxEvent struct {
    Depth     int     `json:"depth"`
    Name      string  `json:"name"`
    StartMs   float64 `json:"start_ms"`
    DurationMs float64 `json:"duration_ms"`
}
```

### Listing logic
- `InstalledVersions(serverRoot)` to get all PHP versions
- For each version, scan `{phpDir}/spx-data/*.json`
- Parse each JSON file into `SpxProfile`
- Filter by `domain` query param if provided
- Sort by timestamp descending

### Detail logic
- Find which PHP version's spx-data dir contains `{key}.json`
- Parse metadata JSON → `SpxProfile`
- Open `{key}.txt.gz`, gunzip, parse line by line
- Build flat profile (aggregate per function name) and events array
- Return `SpxProfileDetail`

### Call trace parsing
The SPX `.txt.gz` format (from `spx_reporter_full.c`):
- Header lines starting with `#`
- Data lines: `{+|-}\t{depth}\t{func_key}\t{wt_value}[\t{other_metrics}]`
- The `{func_key}` is an index into a function table defined in the header
- Header format: `# func {idx} {func_name}`

Parse strategy:
1. First pass: build function index map from `# func {idx} {name}` header lines
2. Second pass: process `+`/`-` events, track enter times on a stack, compute durations on exit

---

## Step 6: Frontend — SPX types and API functions

Add to `frontend/src/lib/api.ts`:

```ts
export interface SpxProfile {
  key: string
  php_version: string
  domain: string
  method: string
  uri: string
  wall_time_ms: number
  peak_memory_bytes: number
  called_func_count: number
  timestamp: number
}

export interface SpxFunction {
  name: string
  calls: number
  inclusive_ms: number
  exclusive_ms: number
  inclusive_pct: number
  exclusive_pct: number
}

export interface SpxEvent {
  depth: number
  name: string
  start_ms: number
  duration_ms: number
}

export interface SpxProfileDetail extends SpxProfile {
  functions: SpxFunction[]
  events: SpxEvent[]
}

export const getSpxProfiles = (domain?: string) =>
  request<SpxProfile[]>('GET', `/api/spx/profiles${domain ? `?domain=${encodeURIComponent(domain)}` : ''}`)

export const getSpxProfile = (key: string) =>
  request<SpxProfileDetail>('GET', `/api/spx/profiles/${key}`)

export const deleteSpxProfile = (key: string) =>
  request<void>('DELETE', `/api/spx/profiles/${key}`)

export const clearSpxProfiles = (domain?: string) =>
  request<void>('DELETE', `/api/spx/profiles${domain ? `?domain=${encodeURIComponent(domain)}` : ''}`)
```

---

## Step 7: `frontend/src/stores/spx.ts`

```ts
defineStore('spx', () => {
  const profiles = ref<SpxProfile[]>([])
  const selected = ref<SpxProfileDetail | null>(null)
  const loading = ref(false)
  const unreadCount = ref(0)

  async function load(domain?: string) { ... }
  async function select(key: string) { ... }
  async function remove(key: string) { ... }
  async function clear(domain?: string) { ... }

  return { profiles, selected, loading, unreadCount, load, select, remove, clear }
})
```

---

## Step 8: `frontend/src/views/SpxView.vue`

Full-width layout (like Mail). Two-panel:

**Left panel (profile list)**
- Filter by domain dropdown (populated from `sitesStore.sites` where `spx_enabled == 1`)
- Refresh button
- Profile cards: domain + URI, wall time, memory, func count, relative timestamp
- Click to select

**Right panel (profile detail)**
- Header: domain, URI, full timestamp, wall time, memory, func count
- Delete button
- 4 tabs:
  - **Flat Profile** — `<table>` sorted by exclusive_ms desc: rank, function name, calls, incl ms, excl ms, incl%, excl%
  - **Flamegraph** — `<canvas>` element, rendered with simple HTML5 canvas drawing (no library); x=time, y=depth; colored by depth; hover tooltip; click to zoom in
  - **Timeline** — horizontal bars on canvas showing each event's start+duration relative to request start; sorted by start time
  - **Metadata** — two-column key/value table from the profile fields

**Empty state**: if no profiles, show instructions on how to use SPX (enable on a site, then add `?SPX_KEY=dev&SPX_UI_URI=/` to a request URL, or set cookies).

---

## Step 9: `App.vue` + `router/index.ts`

`App.vue`:
- Import `useSpxStore` (or check `sitesStore.sites.some(s => s.spx_enabled == 1)`)
- Add nav item: `{ path: '/spx', label: 'Profiler', icon: Activity, requiresSpx: true }`
- `requiresSpx` filter: `sitesStore.sites.some(s => s.spx_enabled == 1)`
- Show unread badge from `spxStore.unreadCount`

`router/index.ts`:
- Import `SpxView`
- Add `{ path: '/spx', component: SpxView, meta: { fullWidth: true } }`

---

## Step 10: README + CHANGELOG + TODO.md

`README.md`:
- Add SPX Profiler to Features list
- Add "PHP Profiler (SPX)" section under Debugging section

`CHANGELOG.md`:
- Add to `# Unreleased`: "Add SPX profiler support with native profile viewer (flat profile, flamegraph, timeline)"

`TODO.md`:
- Move "SPX profiler" from Backlog to Completed

---

## Implementation Order

1. `.github/workflows/build-php.yml`
2. `php/installer.go`
3. `php/config.go`
4. `api/sites.go`
5. `api/spx.go` + register routes in `api/server.go`
6. `frontend/src/lib/api.ts`
7. `frontend/src/stores/spx.ts`
8. `frontend/src/views/SpxView.vue`
9. `frontend/src/App.vue` + `frontend/src/router/index.ts`
10. `README.md` + `CHANGELOG.md` + `TODO.md`
11. `make install` → `sudo systemctl restart devctl` → browser test

---

## Notes / Gotchas

- `spc-alpine-docker` requires Docker on the runner — `ubuntu-latest` has Docker pre-installed.
- The `spc-alpine-docker` wrapper is part of the static-php-cli repo checkout. We need to `git clone` static-php-cli into the working directory, or use a simpler approach: download just the `spc` binary from the nightly release and run it directly (the alpine-docker variant is only needed for reproducible musl builds — `ubuntu-latest` is not Alpine).
- **Better approach**: Download the pre-built `spc-linux-x86_64` binary from `dl.static-php.dev/static-php-cli/spc-bin/nightly/spc-linux-x86_64`, then run `./spc download ... && ./spc build ...`. This runs directly on the Ubuntu runner using the spc binary (which itself is a statically linked PHP+phar). The Linux build target in spc produces musl/Alpine output by using Docker internally when `./bin/spc-alpine-docker` is called, but we can also run inside a Docker container ourselves.
- **Simplest correct approach**: Use the upstream workflow pattern exactly — `git clone crazywhalecc/static-php-cli`, then run `./bin/spc-alpine-docker download ...` and `./bin/spc-alpine-docker build ...`. This is what upstream does and it works on `ubuntu-latest`.
- The binary output paths from spc: `buildroot/bin/php` (CLI) and `buildroot/bin/php-fpm` (FPM)
- When uploading to release, rename them to `php-{ver}-cli-linux-x86_64` and `php-{ver}-fpm-linux-x86_64` in a `cp` step before upload
- `softprops/action-gh-release@v2` with `tag_name: ${{ github.ref_name }}` will attach assets to the current release
- The `GITHUB_TOKEN` has write permission on releases only if `permissions: contents: write` is set in the workflow

## SPX call trace — exact format from source

From [SPX source `spx_reporter_full.c`](https://github.com/NoiseByNorthwest/php-spx/blob/master/src/spx_reporter_full.c):

The `.txt.gz` format header:
```
# spx-version {ver}
# php-version {ver}
# enabled-metrics wt zm
# func {idx} {file}:{line} {func_name}
```

Data lines (after header):
```
{+|-} {depth} {func_idx} {wt_value} [{zm_value}]
```
- `+` = function enter
- `-` = function exit  
- `{wt_value}` = wall time in microseconds (cumulative from request start)
- `{zm_value}` = memory usage in bytes (if zm metric enabled)

To compute duration: `exit_wt - enter_wt` for each function call.
To build flat profile: accumulate per function name.
To build flamegraph events: each enter/exit pair becomes `{name, depth, start_ms: enter_wt/1000, duration_ms: (exit_wt-enter_wt)/1000}`.
