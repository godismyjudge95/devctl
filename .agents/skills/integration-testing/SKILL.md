---
name: integration-testing
description: How to write and run devctl integration tests — where tests live, how to write failing tests first (TDD), and how to run them safely inside an Incus container without touching live host data.
---

# Skill: integration-testing

## The cardinal rule

**NEVER run integration tests on the host machine.**

The tests in `tests/api/`, `tests/integration/`, and `tests/e2e/` run against a **live devctl instance**. They mutate real state: they send emails, create sites, install services, change settings. Running them against the host devctl corrupts live data.

**All tests run INSIDE the Incus test container.** The host devctl is never stopped and port 4000 on the host is never touched. Multiple containers can run in parallel without conflicts.

## Test layers

| Layer | Location | Framework | Execution |
|---|---|---|---|
| Go API tests | `tests/api/` | `go test -tags integration` | Compiled to binary on host, pushed into container, run via `incus exec` |
| BATS tests | `tests/integration/` | bats-core | Pushed into container, run via `incus exec` (bats pre-baked in image) |
| Playwright e2e | `tests/e2e/` | Playwright + Chromium | Pushed into container, run via `incus exec` (Playwright + Chromium pre-baked in image) |

## TDD workflow — write the failing test FIRST

Follow Red → Green → Refactor. **No production code before a failing test exists.**

1. Write the test in the right file (see below).
2. Start the Incus container (see below).
3. Run just that test against the container — confirm it **fails for the right reason**.
4. Write the minimal production code fix.
5. Re-run — confirm it **passes**.
6. Run the full suite to confirm no regressions.

## Where to put Go API tests

| What you're testing | File |
|---|---|
| Mail / Mailpit tools | `tests/api/mail_test.go` |
| Dumps | `tests/api/` — add to existing dumps file or create `dumps_test.go` |
| Services (read-only) | `tests/api/services_test.go` |
| Services (mutating) | `tests/api/services_mutate_test.go` |
| Sites (read-only) | `tests/api/sites_test.go` |
| Sites (mutating) | `tests/api/sites_mutate_test.go` |
| Settings | `tests/api/settings_test.go` / `settings_mutate_test.go` |
| New area | Create `tests/api/<area>_test.go` |

Every file **must** start with:
```go
//go:build integration

package apitest
```

## Test helpers available in `tests/api/helpers_test.go`

| Helper | Signature | Use for |
|---|---|---|
| `httpGet` | `(t, path) → []byte` | GET, asserts 200 |
| `httpGetStatus` | `(t, path, wantStatus) → []byte` | GET with expected status |
| `httpPost` | `(t, path, body) → ([]byte, int)` | POST with optional JSON body |
| `httpPut` | `(t, path, body) → ([]byte, int)` | PUT with JSON body |
| `httpDelete` | `(t, path) → ([]byte, int)` | DELETE, no body |
| `decodeJSON[T]` | `(t, []byte) → T` | Unmarshal JSON, fatal on error |
| `pollServiceStatus` | `(t, id, wantStatus, timeout)` | Poll until service reaches status |

`baseURL()` reads `DEVCTL_BASE_URL` from the environment — when running inside the container this is set to `http://127.0.0.1:4000` (the container's own devctl).

## One-time image setup

Before the first test run, bake the base Incus image with all test tooling:

```sh
make test-env-setup
```

This installs Node.js 22, bats-core, Playwright, and Chromium into the `devctl-ubuntu-base` image. Re-run whenever you want to update the base image. Takes ~5-10 minutes once.

## Starting the Incus test container

```sh
# 1. Build the binary first
make build

# 2. Start an interactive container (Ctrl+C destroys it when done)
make test-env
```

`make test-env` will:
- Launch a fresh `devctl-ubuntu-base` Incus container
- Push the `./devctl` binary, write the systemd unit, start the service
- Compile the Go API test binary and push it in
- Push BATS tests and Playwright test files into the container
- Wait for devctl to respond at `127.0.0.1:4000` **inside** the container
- Export `DEVCTL_CONTAINER=devctl-test-<timestamp>` and block until Ctrl+C (then destroy it)
- **The host devctl is never touched** — its port 4000 remains yours

## Running tests against the container

`make test-env` blocks in one terminal. In a second terminal:

```sh
# Set the container name (shown in the test-env banner)
export DEVCTL_CONTAINER=devctl-test-1234567890

# Run BATS tests only
make test-bats

# Run Go API tests only
make test-api

# Run Playwright e2e tests only
make test-e2e

# Run all three layers at once
make test

# Run the full suite end-to-end (own container, auto-destroys)
make test-run
```

## Parallel test runs

Because no host ports are bound, you can run multiple containers simultaneously:

```sh
# Terminal 1
make build && make test-env
# → exports DEVCTL_CONTAINER=devctl-test-111

# Terminal 2 (completely separate test run)
make build && make test-env
# → exports DEVCTL_CONTAINER=devctl-test-222

# Each container is fully isolated — no port conflicts, no shared state
```

## Pushing a new binary and re-running tests

After changing Go code, rebuild and push without tearing down the container:

```sh
DEVCTL_CONTAINER=devctl-test-1234567890 make test-push
```

This runs `make build`, pushes the new binary, restarts devctl inside the container, waits for it to respond, then runs the full test suite.

## Running a single Go API test

```sh
# Compile with the test name filter and run inside the container
go test -c -tags=integration -o devctl.test ./tests/api/
incus file push devctl.test $DEVCTL_CONTAINER/tmp/devctl.test
incus exec $DEVCTL_CONTAINER -- chmod 755 /tmp/devctl.test
incus exec $DEVCTL_CONTAINER -- env DEVCTL_BASE_URL=http://127.0.0.1:4000 \
  /tmp/devctl.test -test.v -test.run TestDeleteAllEmails
```

## Finding the running container name

```sh
incus list
```

The test container is named `devctl-test-<timestamp>`. The currently running one will show STATE=RUNNING.

## Example: writing a failing Go API test

```go
//go:build integration

package apitest

import (
    "encoding/json"
    "testing"
)

func TestDeleteAllEmails_RemovesAllMessages(t *testing.T) {
    // Arrange — inject a message so there's something to delete.
    // (use Mailpit's /api/v1/send or send via SMTP)

    before := mailCount(t)  // helper you define in this file
    if before == 0 {
        t.Fatal("need at least one email to test deletion")
    }

    // Act — call the endpoint under test.
    _, status := httpDelete(t, "/api/mail/api/v1/messages")
    if status != 200 {
        t.Fatalf("expected 200, got %d", status)
    }

    // Assert — verify state changed.
    after := mailCount(t)
    if after != 0 {
        t.Errorf("expected 0 emails after delete-all, got %d", after)
    }
}
```

## Checklist

- [ ] Load this skill before writing any test or fix
- [ ] Run `make test-env-setup` if the base image is stale (first time or after tooling updates)
- [ ] Write the test file first — no production code yet
- [ ] Start `make build && make test-env` in a separate terminal
- [ ] Run just the failing test (see "Running a single Go API test" above)
- [ ] Confirm it **fails for the right reason**
- [ ] Write the minimal fix
- [ ] Run the test: confirm it **passes**
- [ ] Run the full suite (`make test` in the second terminal) to confirm no regressions
- [ ] Stop the container (Ctrl+C in the `make test-env` terminal)

## Gotchas and known patterns

### Never use `run bash -c "api_get ..."` in BATS tests

`api_get`, `api_post`, etc. are shell **functions** defined in `setup.bash`. They are not available in child processes created by `bash -c "..."`.

**Wrong (function not in subshell):**
```bash
run bash -c "api_get /api/services | jq '.[] | .installed'"
```

**Correct (inline curl):**
```bash
run bash -c "curl -sf '${BASE_URL}/api/services' | jq '.[] | .installed'"
```

`BASE_URL` is exported in `setup.bash` and is available in subshells.

### PHP settings tests require a PHP stub

The PHP settings tests (`test_api_php_mutate.bats`) require at least one PHP version to exist under `SERVER_ROOT/php/`. The `test-env.sh` script creates a PHP 8.4 stub (a no-op `php-fpm` binary + a real `php.ini` with the four tracked settings) in **Step 7a** so the read/write round-trip works.

If the stub is missing, `GET /api/php/settings` returns hardcoded defaults, `PUT /api/php/settings` silently no-ops (no PHP versions found to write to), and all settings tests will fail with unexpected values.

### MySQL creates wrapper scripts, not symlinks

MySQL's `InstallW` calls `WrapperScriptIntoBinDir` (not `LinkIntoBinDir`) for `mysql`, `mysqldump`, and `mysqladmin`. This writes **executable shell scripts** (not symlinks) into the shared `bin/` directory. Tests should check with `test -x` (executable), not `test -L` (symlink).
