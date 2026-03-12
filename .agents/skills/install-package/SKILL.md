---
name: install-package
description: How to implement an idempotent APT-based service installer in devctl — the Installer interface, shared helpers, and registration in install.NewRegistry
license: MIT
compatibility: opencode
metadata:
  layer: backend
  concerns: install, apt, systemd
---

## Overview

The `install/` package provides idempotent install and purge routines for each service devctl manages. All operations run as root (devctl is a system service). Look at `install/redis.go` or `install/mailpit.go` as the simplest reference implementations.

## The `Installer` interface

`install/install.go`:

```go
type Installer interface {
    ServiceID() string
    IsInstalled() bool
    InstallW(ctx context.Context, w io.Writer) error  // streams output to w
    PurgeW(ctx context.Context, w io.Writer) error    // streams output to w
}
```

`InstallW`/`PurgeW` write progress text to `w` — this is forwarded to the browser as SSE output chunks by `api/services.go`. Use the shared `runShellW` (or `runShellInDirW`) helpers to run commands and stream their output.

## Minimal implementation template

Create `install/myservice.go`:

```go
package install

import (
    "context"
    "io"
)

type MyServiceInstaller struct{}

func (i *MyServiceInstaller) ServiceID() string { return "my-service" }

func (i *MyServiceInstaller) IsInstalled() bool {
    return fileExists("/usr/bin/my-service")
}

func (i *MyServiceInstaller) InstallW(ctx context.Context, w io.Writer) error {
    if i.IsInstalled() {
        return nil // idempotent: already present
    }
    if err := aptUpdateW(ctx, w); err != nil {
        return err
    }
    if err := aptInstallW(ctx, w, "my-service"); err != nil {
        return err
    }
    return enableAndStartW(ctx, w, "my-service")
}

func (i *MyServiceInstaller) PurgeW(ctx context.Context, w io.Writer) error {
    stopAndDisableW(ctx, w, "my-service") // best-effort, ignores errors
    if err := aptPurgeW(ctx, w, "my-service"); err != nil {
        return err
    }
    removeFiles(
        "/etc/apt/sources.list.d/my-service.list",
        "/usr/share/keyrings/my-service-keyring.gpg",
    )
    return nil
}
```

## Shared helpers (defined in `install/install.go` and `install/postgres.go`)

| Helper | Description |
|---|---|
| `runShellW(ctx, w, cmd)` | Run `sh -c cmd`, stream stdout+stderr to `w` |
| `runShellInDirW(ctx, w, dir, cmd)` | Same, but sets working directory to `dir` |
| `aptInstallW(ctx, w, pkgs...)` | `apt-get install -y --no-install-recommends`, streams output |
| `aptPurgeW(ctx, w, pkgs...)` | `apt-get purge -y` + `autoremove`, streams output |
| `aptUpdateW(ctx, w)` | `apt-get update`, streams output |
| `enableAndStartW(ctx, w, unit)` | `systemctl enable` then `systemctl start` |
| `stopAndDisableW(ctx, w, unit)` | `systemctl stop` + `systemctl disable` (best-effort) |
| `curlPipe(ctx, url, cmd, args...)` | `curl -fsSL url \| cmd args` — used for GPG key import |
| `curlDownload(ctx, url, dest)` | Download a file with curl |
| `writeFile(path, content, perm)` | Write a file with given permissions |
| `removeFiles(paths...)` | Remove files, ignoring "not found" errors |
| `fileExists(path)` | `os.Stat`-based existence check |
| `lsbReleaseName(ctx)` | Returns Ubuntu/Debian codename (e.g. `"noble"`) |
| `wrapOutput(label, err, out)` | Wraps an error with command label + output (defined in `install/postgres.go`) |

Timeouts: `aptTimeout = 10min`, `netTimeout = 5min`, `curlTimeout = 5min`. Applied internally by helpers.

## Register the installer

Installers are registered in `install.NewRegistry(...)` in `install/install.go`. There is **no** package-level `var Registry` — the registry is constructed at startup with dependencies injected:

```go
func NewRegistry(siteManager *sites.Manager, queries *dbq.Queries, supervisor *services.Supervisor) map[string]Installer {
    return map[string]Installer{
        "redis":     &RedisInstaller{},
        "my-service": &MyServiceInstaller{},
        // ...
        "reverb": &ReverbInstaller{
            siteManager: siteManager,
            queries:     queries,
            supervisor:  supervisor,
        },
    }
}
```

Add your installer to the map. If it needs dependencies, add fields to its struct and pass them from `NewRegistry`.

## Installers with dependencies (e.g. Reverb)

Some installers need access to `siteManager`, `queries`, or `supervisor`. See `install/reverb.go` as the reference:

```go
type ReverbInstaller struct {
    siteManager *sites.Manager
    queries     *dbq.Queries
    supervisor  *services.Supervisor
}
```

- Use `supervisor.Stop(id)` in `PurgeW` to stop a managed child process before removing files.
- Use `siteManager.Create(...)` / `siteManager.DeleteByDomain(...)` to register/remove Caddy vhosts.
- Use `runShellInDirW(ctx, w, dir, cmd)` for commands that must run in a specific directory (e.g. `php artisan`).

## Idempotency rules

- `InstallW()` **must** be safe to call when the service is already installed. Guard with `IsInstalled()` at the top.
- `PurgeW()` **must** be safe to call when the service is not installed. Use `stopAndDisableW()` (ignores errors) and `removeFiles()` (ignores not-found).
- Never prompt for input — use `DEBIAN_FRONTEND=noninteractive` (already set by `aptGet`).

## Checklist

- [ ] Create `install/myservice.go` implementing `Installer`
- [ ] `IsInstalled()` checks for the primary binary/file using `fileExists()`
- [ ] `InstallW()` is idempotent (guarded by `IsInstalled()`)
- [ ] `PurgeW()` is safe when not installed
- [ ] Register in `install.NewRegistry(...)` in `install/install.go`
- [ ] Set `Installable: true` in the service's entry in `config/defaults.go`
