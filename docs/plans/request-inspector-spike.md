# Request inspector spike (plan item 3.1)

Status: spike done on 2026-11-08. Built in plan item 3.1 (steps 1 to 4, 6, 7, and 8 below). Step 5, the Laravel fixture app check, and the overhead measurement are not done.

Files:

- `php/inspector.php` is the PHP agent. devctl embeds it and writes it to `{serverRoot}/devctl/inspector.php`.
- `docs/plans/request-inspector-spike/sanity.php` is the behavioral check to run in the test VM.

## Goal

One view for each request. The view shows SQL queries (SQL, bindings, duration, caller file and line), dumps, logs, exceptions, jobs, cache events, outgoing HTTP, and views. It flags N+1 queries, duplicate queries, slow queries, and slow requests. The app code does not change.

## Decision

Build a PHP userland agent that `prepend.php` loads. The agent gets its hooks from the `opentelemetry` extension, which the devctl PHP 8.1 to 8.5 static builds already include. Do not write a devctl C extension.

The reason is a fact about the current binaries. `scripts/php8-exts.sh` puts `opentelemetry` in the extension list for every 8.1+ build on Linux and macOS (`.github/workflows/build-php.yml` uses that list for both). `CHANGELOG.md` line 46 says the same. That extension exposes `OpenTelemetry\Instrumentation\hook($class, $function, $pre, $post)`, which is a userland interface to the Zend observer API. Option 2 in the plan asks for a small C extension that uses the observer API. That extension is already in the binaries, upstream maintains it, and using it needs no PHP binaries release.

Coverage that follows from this choice:

| PHP | Laravel apps | Other apps (plain PDO, mysqli, curl) |
|---|---|---|
| 8.2 to 8.5 | Full | Queries, outgoing HTTP, fatal errors |
| 8.1 | Full, except non-Laravel HTTP clients outside the Laravel `Http` facade | Nothing. PHP 8.1 cannot observe internal functions |
| 8.0, 7.x | Nothing. No `opentelemetry` in these builds | Nothing |

Laravel 11 and 12 need PHP 8.2, and Laravel 10 needs 8.1, so the gap at 8.0 and 7.x affects only old apps.

## Options compared

### Option 1. Pure userland agent from `prepend.php`

The plan's idea is to hook Laravel when it boots, through a booted callback or a service provider, with a generic PDO fallback.

The premise fails at the hook moment. Userland PHP has no event for "the app now exists". The obvious trick is an autoloader that `prepend.php` registers with `prepend: true`, which would see the first framework class load after `new Application`. Composer breaks that trick. `ClassLoader::register(true)` also prepends, and it runs after `prepend.php`, so Composer resolves every framework class before the observer loader is asked. The observer then sees only classes that Composer cannot find.

The only userland workaround is for `prepend.php` to `require` the app's `vendor/autoload.php` first, then prepend the observer loader. Composer caches its loader in `ComposerAutoloaderInit*::$loader`, so the app's own `require` returns the same loader. This works, but it changes the include order of every request, and it depends on the layout (`public/index.php` next to `../vendor`). It fails for custom public directories, Octane workers, and `artisan` run from another directory.

The generic PDO fallback is not possible in pure userland. Nothing in userland PHP can intercept `PDOStatement::execute` on a `PDO` object that the app creates.

What it would capture: Laravel events only (queries, logs, jobs, cache, views, HTTP client). It works on PHP 7.x and 8.0, which is its one advantage.

### Option 2. A devctl C extension in the static binaries

Hook points are `zend_observer_fcall_register` (PHP 8.0+, internal functions from 8.2+) or a `zend_execute_internal` override, which also works on 7.x, 8.0, and 8.1.

What it captures: any function call, for any framework, with the lowest overhead. It still needs framework knowledge for most of the target list. Jobs, cache events, views, log levels, and the start and end of a unit (request, job, Octane request) are Laravel concepts. The extension would hard-code Laravel method names in C or call into a PHP layer, and that PHP layer is Option 3 again.

Costs:

- New C source in the repo, plus a static-php-cli custom extension patch (like the `pcov` patch in `scripts/patch-spc-for-php8.sh`).
- Six minors on two platforms in CI. Zend internals change between minors.
- A bug is a segfault in an FPM worker, and every fix needs a `php-binaries-*` release and a reinstall by each user.
- The owner must approve each binaries release.

### Option 3 (chosen). Userland agent with `opentelemetry` hooks

The agent is a PHP file. It uses `hook()` for two jobs:

1. It attaches to Laravel. A post hook on `Illuminate\Foundation\Application::__construct` receives the app instance on every Laravel version, any layout, Octane, and `artisan`, and it does not change include order. The hook is registered by name before the class loads.
2. It is the generic fallback. Post hooks on `PDO::exec`, `PDO::query`, `PDOStatement::execute`, `PDOStatement::bindValue`, `mysqli_query`, `mysqli::query`, and `curl_exec` cover WordPress (mysqli), plain PDO apps, Guzzle, and SDKs that use curl. These need PHP 8.2+.

Laravel data comes from Laravel's own events, because they carry the connection name, the bindings, and the timing that Laravel measured.

### Comparison

| | Option 1, pure userland | Option 2, devctl C extension | Option 3, userland + `opentelemetry` |
|---|---|---|---|
| Laravel hook point | Early Composer load plus an observer autoloader. Layout-dependent | C hook on a Laravel method, or a PHP shim | Post hook on `Application::__construct` |
| Non-Laravel SQL | No | Yes, all versions | Yes, 8.2+ |
| Jobs, cache, views, logs | Laravel events | Needs a PHP layer | Laravel events |
| Overhead per hooked call | Laravel listener call | Lowest (C) | One closure call (guess: 1 to 3 µs) |
| Bug impact | PHP exception, caught by the agent | Segfault in the worker | PHP exception, caught by the agent |
| Fix delivery | devctl release (file is embedded) | `php-binaries-*` release and reinstall | devctl release |
| PHP versions | 7.x to 8.5 | 7.x to 8.5 (with `zend_execute_internal`) | 8.1 to 8.5 |
| New build work | None | C source, spc patch, CI matrix | None |

Option 3 gets the reach of Option 2 for the versions that matter, at the maintenance cost of Option 1. Option 2 remains the path if the owner later wants 8.0 and 7.x coverage. In that case the C extension only has to supply a `hook()`-compatible function. The agent stays the same.

## How the agent works

### Loading and coexistence with item 2.3

The agent is its own file, `{serverRoot}/devctl/inspector.php` (next to `prepend.php`, path from the `paths` package). Item 2.3 owns `prepend.php`. The integration needs one line at the end of `prepend.php`:

```php
if (PHP_VERSION_ID >= 80100 && is_file(__DIR__ . '/inspector.php')) { require __DIR__ . '/inspector.php'; }
```

The version gate keeps PHP 7.x and 8.0 from parsing the file. To turn the inspector off, devctl deletes `inspector.php`. FPM needs no restart, because `prepend.php` runs on each request.

The agent defines the global function `devctl_request_id(): ?string`. The 2.3 dump sender adds `'rid' => function_exists('devctl_request_id') ? devctl_request_id() : null` to the dump payload. The Go side then links the dump to its request. This is the only change 2.3 needs, and it works in either merge order.

Everything else in the agent is in the `Devctl\Inspector` namespace, so it cannot collide with app code.

### Units

A unit is one request, one job, or one command. Each unit has a random 16-hex-character id. In FPM, the agent also sends the id in the `X-Devctl-Request-Id` response header, so the browser network tab can link to it.

| Process | Unit | Begins | Ends |
|---|---|---|---|
| PHP-FPM request | `http` | Agent boot | The last shutdown function |
| `artisan` command | `command` | Agent boot | The last shutdown function |
| `queue:work`, `horizon`, `schedule:work`, `octane:*`, `reverb:start`, `pail` | None for the command itself | Laravel `CommandStarting` drops the command unit | |
| Job on a queue worker | `job`, `parent` = the request that queued it | `JobProcessing` | `JobProcessed` or `JobExceptionOccurred` |
| Job on the `sync` connection | Events in the current unit | | |
| Octane worker (`swoole-server`, `roadrunner-worker`) | `http` per request | Octane `RequestReceived` | Octane `RequestTerminated` |
| `composer`, `phpunit`, `pest`, `wp`, any other CLI script | The agent does not start | | |

The parent link uses `Queue::createPayloadUsing()`, which adds `devctl_rid` to every queued job payload.

The CLI rule is a table (`CLI_ENTRYPOINTS`) keyed by the script name. PHPUnit and Composer are off, so a test run with thousands of queries does not fill the buffer.

### Event sources

| Kind | Laravel (8.1+) | Other apps (8.2+) |
|---|---|---|
| `query` | `QueryExecuted` | `PDO::exec`, `PDO::query`, `PDOStatement::execute` (+ `bindValue`), `mysqli_query`, `mysqli::query` |
| `log` | `MessageLogged` | None |
| `exception` | `MessageLogged` with `context.exception`, `JobExceptionOccurred` | Fatal error from `error_get_last()` at shutdown |
| `job` | `JobQueued`, `JobProcessing`, `JobProcessed` (sync jobs) | None |
| `cache` | `CacheHit`, `CacheMissed`, `KeyWritten`, `KeyForgotten` | None |
| `http` | `curl_exec` hook on 8.2+. `Http` client events on 8.1 | `curl_exec` |
| `view` | `composing:*` wildcard | None |
| request meta | `RequestHandled` adds the route name and action | `$_SERVER`, `http_response_code()` |

When the Laravel hook fires, the agent sets a flag, and the PDO and mysqli hooks stop recording. Each query is recorded once.

Reported exceptions reach the log through `MessageLogged`, so they show. Exceptions in `dontReport` (404, validation) do not show, but the response status does.

### Caller file and line

The agent walks `debug_backtrace(DEBUG_BACKTRACE_IGNORE_ARGS)` and returns the first frame that is not in `vendor/` and not in the devctl directory. If all frames are in `vendor/`, it returns the first frame that is not devctl's. Compiled Blade files (`storage/framework/views/`) are returned as they are. The build step maps them back to `.blade.php` with the same logic that `prepend.php` uses for dumps.

### Send timing

The agent buffers events in memory. It sends a batch when the buffer reaches 200 events, and it sends the last batch at the end of the unit.

The shutdown function registers a second shutdown function, and PHP runs that one after all the app's shutdown functions. In Laravel under FPM, Symfony's `Response::send()` calls `fastcgi_finish_request()`, so the browser has the response before the agent sends anything. For other apps the send adds one localhost TCP connect to the end of the request.

### Limits

| Limit | Value | When it is reached |
|---|---|---|
| Events per unit | 5000 | Later events are counted in `meta.dropped` |
| Events per batch | 200 | The agent sends a batch and continues |
| SQL length | 10,000 bytes | Clipped, with `…` at the end |
| Bindings per query | 50 | One extra key `"…"` holds the count left out |
| One string binding | 1024 bytes | Clipped. Invalid UTF-8 becomes `<binary N bytes>` |
| TCP connect timeout | 0.2 s | The batch is lost. The app continues |

### Failure modes

- The daemon is down. The connect to 127.0.0.1 fails at once, and the agent drops the batch.
- A listener or hook throws. `guard()` catches every `Throwable`, and the app does not see it.
- A hook declares a return type. The `opentelemetry` extension then replaces the hooked function's return value with the hook's value. The agent's hooks have no return types. The `guard()` docblock records this rule.
- `opentelemetry.conflicts` disables the extension (Blackfire, SourceGuardian). The agent then gets no hooks. A later `doctor` check can report this.
- Swoole coroutines run hooked calls in parallel. The timer stack can then pair the wrong start and end, and durations are wrong. Octane runs one request at a time in each worker, so this affects only coroutine code that the app starts itself.
- A parse error in `inspector.php` breaks every PHP request. The step 4 integration test runs a real request through FPM with the agent loaded.

## Wire format

The agent uses the existing dump receiver (`dumps/tcp.go`, the `dump_tcp_port` setting, default 9912). The receiver chooses the decoder by the first byte of the line:

- `{` is a JSON frame with a `type` field. The base64 alphabet has no `{`, so the choice is exact.
- Any other first byte is a legacy dump line (base64 JSON). Nothing changes for current dumps.

Frames are plain JSON, not base64. That saves 33% of the size and an encode step.

### Frame

One frame is one line.

```json
{
  "v": 1,
  "type": "unit",
  "rid": "9f2c4e1a7b3d5c60",
  "seq": 0,
  "final": true,
  "meta": {
    "kind": "http",
    "method": "GET",
    "url": "https://shop.test/orders",
    "host": "shop.test",
    "route": "orders.index",
    "action": "App\\Http\\Controllers\\OrderController@index",
    "status": 200,
    "parent": null,
    "pid": 4182,
    "php": "8.4.23",
    "sapi": "fpm-fcgi",
    "started_at": 1794150000.1234,
    "duration_ms": 182.4,
    "memory_peak": 25165824,
    "dropped": 0
  },
  "events": [
    {
      "kind": "query",
      "t": 41.2,
      "sql": "select * from `users` where `users`.`id` = ? limit 1",
      "bindings": [7],
      "ms": 0.61,
      "conn": "mysql",
      "driver": "mysql",
      "caller": {"file": "/home/u/ddev/sites/shop/app/Http/Controllers/OrderController.php", "line": 31}
    }
  ]
}
```

- `seq` starts at 0 and counts up for each batch of the unit.
- Every frame carries the full `meta`. The last value wins. `meta` grows during the unit (for example `route` arrives with `RequestHandled`).
- `final: true` marks the last frame. Only the final frame has `duration_ms`, `memory_peak`, `dropped`, and (for HTTP) `status`.
- `meta.kind` is `http`, `command`, or `job`. Command units add `command` and `artisan`. Job units add `job`, `queue`, `connection`, `job_id`, and `attempt`.
- `t` is milliseconds from the start of the unit to the moment the agent recorded the event.

### Event kinds

| `kind` | Fields |
|---|---|
| `query` | `sql`, `bindings` (list or map), `ms`, `conn`, `driver` (Laravel), `error` (generic hooks) |
| `log` | `level`, `message` |
| `exception` | `class`, `message`, `file`, `line`, `trace` (up to 20 frames: `file`, `line`, `fn`) |
| `job` | `phase` (`queued`, `processing`, `processed`), `name`, `connection`, `id` |
| `cache` | `op` (`hit`, `miss`, `write`, `forget`), `key`, `store` |
| `http` | `method`, `url`, `status`, `ms`, `error` |
| `view` | `name`, `path` |

All events have `kind` and `t`. Events that the app caused also have `caller`.

### Dumps

A dump line gets an optional `rid`. The receiver stores the dump in SQLite as it does now. If `rid` is set, it also adds the dump id to that unit. A dump can arrive before the unit's first frame, because dumps are sent at once and frames at the end. The ring buffer therefore creates a unit on the first message of any type.

## Go side data shape

### An in-memory ring buffer, not SQLite

Store units in a ring buffer in the daemon. The limits are a unit count (`inspector_max_units`, default 200) and a byte budget (64 MB of frame JSON). When either limit is reached, the oldest unit goes.

Reasons:

- The data is short-lived debug data. A developer looks at a request within minutes. Telescope prunes for the same reason.
- Event shapes differ by kind and will grow. A SQLite schema would need a goose migration for each new field. JSON blobs in SQLite would give the cost of disk writes without useful queries.
- A busy page can send hundreds of queries in each request, and Livewire polling repeats that every few seconds. In memory, there are no disk writes for each request on the developer's machine.
- Every read is by recency or by id. A map plus an ordered ring serves both.

The cost: units are lost when the daemon restarts (for example on a devctl update). Dumps stay in SQLite as now. If the owner wants persistence later, add an "export unit as JSON" action, not a table.

### Types

```go
package inspector

type Unit struct {
	ID        string
	Meta      Meta            // merged from every frame, last value wins
	Events    []Event
	Final     bool            // the final frame arrived
	Bytes     int             // frame JSON size, for the byte budget
	DumpIDs   []int64
	Groups    map[QueryKey]*QueryGroup // filled at ingest
	UpdatedAt time.Time
}

type Event struct {
	Kind   string
	T      float64
	Caller *Caller
	Query  *Query           // set only when Kind == "query"
	Raw    json.RawMessage  // the event as received, passed to the API as it is
}

type Query struct {
	SQL        string
	Normalized string  // computed once at ingest
	Bindings   json.RawMessage
	Ms         float64
}

type QueryKey struct{ Normalized, File string; Line int }

type QueryGroup struct {
	Count   int
	TotalMs float64
	First   int // index into Events
}

type Ring struct {
	mu    sync.Mutex
	order []string          // unit ids, oldest first
	units map[string]*Unit
	bytes int
}
```

Go reads only the fields it needs for analysis (`query` events, the counts for the list view). The UI gets the other fields from `Raw`. This keeps the Go code small when PHP adds an event field.

`Ring.Ingest(frame)` creates or updates the unit, appends the events, normalizes the SQL, updates `Groups`, and evicts old units. `Analyze(unit, rules)` is a pure function that applies the thresholds at read time. A threshold change in settings therefore re-flags units that are already in the buffer.

### Problem rules

| Problem | Rule | Setting (default) |
|---|---|---|
| N+1 | The same normalized SQL from the same caller file and line, `count >= N` | `inspector_n_plus_one_min` (3) |
| Duplicate | The same raw SQL with the same bindings, `count >= 2`, any caller | None |
| Slow query | `ms >= threshold` | `inspector_slow_query_ms` (100) |
| Slow request | `meta.duration_ms >= threshold` | `inspector_slow_request_ms` (1000) |

An N+1 problem reports the normalized SQL, the caller, the count, the total time, and the first event index, so the UI can jump to it. Duplicates that are also N+1 by the rule show as N+1 only.

### SQL normalizer

A single-pass byte scanner in Go. It is a small state machine with these states: code, single-quoted string, double-quoted identifier, backtick identifier, line comment (`--`, `#`), block comment, and PostgreSQL dollar quote (`$tag$…$tag$`). It does not use regular expressions. It writes a new string:

- Each string literal becomes `?`.
- Each number that is not part of an identifier becomes `?`.
- `$1` (PostgreSQL) and `:name` (PDO named) become `?`. `::` casts stay.
- Comments go.
- Each run of whitespace becomes one space.
- A list of placeholders in parentheses becomes one: `in (?, ?, ?)` becomes `in (?)`.
- Repeated value groups become one: `values (?, ?), (?, ?)` becomes `values (?, ?)`.
- Quoted identifiers and keyword case stay as they are. One caller sends the same text each time, and keyword case cannot separate two callers.

The list rules matter for Laravel. Eager loads with integer keys inline the ids (`whereIntegerInRaw`), so `where id in (1, 2, 3)` and `where id in (4, 5)` must normalize to the same string. Test the normalizer with a table of input and output pairs.

## Build plan

Each step is one reviewable change with its tests. Steps 1 to 3 need no PHP. Each step builds on the one before it.

1. **Go package `inspector`.** Frame parsing, the SQL normalizer, the ring buffer, and `Analyze`. Unit tests: the normalizer table, N+1 and duplicate and slow rules, eviction by count and by bytes, frames out of `seq` order, a dump that arrives before its unit, and a unit with no final frame. No wiring.
2. **Receiver routing.** `dumps/tcp.go` sends lines that start with `{` to `inspector.Ring.Ingest`. Legacy lines are unchanged. The line limit goes from 1 MB to 4 MB. Dump payloads with `rid` call `Ring.LinkDump`. Unit tests for the routing. Also change the listener from `":9912"` to `127.0.0.1:9912` (see open decisions).
3. **API and live updates.** `GET /api/requests` (filters `site`, `kind`, `problems`, `limit`), `GET /api/requests/{id}`, `DELETE /api/requests`, `GET /ws/requests` (same pattern as `dumps.Hub`). Settings keys with defaults in `api/settings.go`. Integration test in `tests/api/` that writes a synthetic frame to the TCP port in the test machine and reads it back through the API, with the N+1 flag. No PHP needed.
4. **Agent install.** Move `inspector.php` to `php/inspector.php`, embed it, and write it from `InstallPrepend` with the configured TCP address (replace the default `tcp://127.0.0.1:9912`). Delete it when `inspector_enabled` is off. Add the gate line to `prepend.php` and `rid` to the dump payload, rebased on item 2.3. Map compiled Blade callers back to `.blade.php`. Go unit test for `InstallPrepend`. Integration test with a plain PHP fixture site that runs an N+1 loop through FPM, then checks the API. Measure overhead here: one request with 100 queries, with the agent on and off.
5. **Laravel check.** A Laravel fixture app in the test machine (`laravel new`, SQLite or MySQL). One route with an N+1 loop, a cache call, a queued job, a log line, and a view. Check every event kind, the job unit with its `parent`, and a `queue:work` run that produces only job units.
6. **CLI.** `requests:list`, `requests:show <id>`, `requests:watch --json` (NDJSON), `requests:problems`, all with `--json`. Update the registry, the README CLI table, and the `devctl devctl:skill` source. Add the `requests` tool group to MCP if item 2.1 is merged.
7. **UI.** A Requests page: a list with problem badges, and a detail view with tabs (Queries, Dumps, Logs, Exceptions, Jobs, Cache, HTTP, Views). A setting for the editor URL scheme (`vscode://file/{file}:{line}`, `phpstorm://open?file={file}&line={line}`). Playwright test in `tests/e2e/` that sends a synthetic frame and opens the detail view.
8. **Docs.** README feature section, the comparison table row, and a CHANGELOG `## Unreleased` line.

## Verification done in the spike

- `php -l` (Homebrew PHP 8.4.23) passes on `inspector.php`, `sanity.php`, and the three workloads inside `sanity.php`.
- `mago lint -s` reports no issues on both files.
- The prototype has not run against a devctl PHP binary. The rules for this task do not allow that on the host.

### Behavioral check for the test VM

Copy `sanity.php` into the Linux test VM, then run it with each installed devctl PHP 8.1+ binary. Pass the agent that devctl installed:

```sh
~/ddev/sites/server/php/8.4/php sanity.php ~/ddev/sites/server/devctl/inspector.php
```

If the build has no PDO SQLite driver, the generic part prints `SKIP`. Point it at the devctl MySQL instead:

```sh
SANITY_DSN='mysql:host=127.0.0.1;port=3306;dbname=test' SANITY_USER=root SANITY_PASS=... ~/ddev/sites/server/php/8.4/php sanity.php ~/ddev/sites/server/devctl/inspector.php
```

The script starts a TCP listener on a random port. It runs two workloads, each in a file named `artisan`, with the agent as `auto_prepend_file`. Then it checks the frames that arrive.

These lines prove the design on PHP 8.2+:

- `opentelemetry loaded: yes …` shows that the hooks are available in the shipped binary.
- `PASS generic: 4 identical queries from the same caller line` shows that the `PDOStatement::execute` hook works and that the caller resolves to the app line.
- `PASS generic: bindValue() captured` and `PASS generic: failed curl_exec recorded with error` show the binding and HTTP hooks.
- `PASS generic: workload result unchanged` shows that the hooks do not change return values.
- `PASS laravel: Application::__construct hook attached listeners (3 QueryExecuted)` shows the Laravel attach path.
- `PASS laravel: caller skips vendor/ frames` shows the caller rule.
- `PASS laravel: raw PDO not double-captured` shows that the PDO hooks stop when Laravel takes over.
- The last line is `RESULT: PASS`.

On PHP 8.1 the generic part prints `SKIP` and the Laravel part must pass. A `FAIL` on `opentelemetry loaded` or on the Laravel attach check invalidates the decision. In that case, go back to Option 2.

## Open decisions and risks

- **The dump port listens on all interfaces.** `main.go` builds the address as `":" + dump_tcp_port`. Any host on the LAN can send dumps now, and could send fake request frames after this change. Step 2 changes it to `127.0.0.1`. This is a behavior change, so the owner decides. Default in this plan is to change it.
- **`prepend.php` hardcodes port 9912**, but `dump_tcp_port` is a setting. The agent gets the address from devctl at install (step 4). The dump sender in `prepend.php` has the same problem, and item 2.3 owns it.
- **Default on or off.** This plan sets `inspector_enabled` on by default. devctl is a development tool, and the overhead is small (guess, measured in step 4). The owner could choose off by default with a toggle on the Requests page.
- **No coverage for PHP 8.0 and 7.x**, and no generic SQL capture on 8.1. A devctl C extension with a `zend_execute_internal` hook could add this later without changes to the agent.
- **Units are lost on daemon restart.** This is the ring buffer trade-off above.
- **The `opentelemetry` extension must stay in the 8.1+ builds.** Add a note to the `create-release` skill so that nobody removes it from `scripts/php8-exts.sh`.
- **Untested hook behavior.** Three facts come from the `opentelemetry` docs and are not yet observed: post hooks on internal methods inherited by the PHP 8.4 driver subclasses (`Pdo\Mysql`), hook validation (`opentelemetry.validate_hook_functions`) with untyped closures, and the swoole PDO drivers in 8.2+ builds. `sanity.php` covers hook validation. Steps 4 and 5 cover the subclasses and the swoole drivers.
