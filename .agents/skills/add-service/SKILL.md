---
name: add-service
description: How to add a new managed dev service to devctl — writing the ServiceDefinition, registering it in config/defaults.go, and wiring up the optional installer
license: MIT
compatibility: opencode
metadata:
  layer: backend
  concerns: services, config
---

## Overview

Service definitions are **static Go code** in `config/defaults.go`. There is no `services.yaml` at runtime — it was removed. `services.NewRegistry(config.DefaultServices())` converts the slice to an in-memory `*Registry` at startup. To add a new built-in service, add an entry to `DefaultServices()`.

## The `Definition` struct

`services/definition.go`:

```go
type Definition struct {
    ID           string
    Label        string
    Start        string
    Stop         string
    Restart      string
    Status       string
    StatusRegex  string
    Version      string
    VersionRegex string
    Log          string
    Installable  bool
    // Managed = true means devctl supervises this as a child process (not systemctl)
    Managed      bool
    ManagedCmd   string // e.g. "php"
    ManagedArgs  string // e.g. "artisan reverb:start --host=127.0.0.1 --port=7383"
}
```

| Field | Notes |
|---|---|
| `ID` | Lowercase, unique, used as the API path param (e.g. `"redis"`) |
| `Label` | Human-readable display name (e.g. `"Redis"`) |
| `Start/Stop/Restart` | Shell commands run via `sh -c` (only for non-Managed services) |
| `Status` | Command whose stdout is matched against `StatusRegex` |
| `StatusRegex` | Named capture `(?P<status>active|inactive|failed)` |
| `Version` | Command whose stdout is matched against `VersionRegex` |
| `VersionRegex` | Named capture `(?P<version>[\d.]+)` |
| `Log` | Absolute path to a log file (tailed for the log SSE endpoint) |
| `Installable` | `true` if an `Installer` is registered for this ID via `install.NewRegistry(...)` |
| `Managed` | `true` if devctl runs this as a supervised child process (not systemctl) |
| `ManagedCmd` | Executable for supervised services (e.g. `"php"`) |
| `ManagedArgs` | Args string for supervised services (e.g. `"artisan reverb:start ..."`) |

## Example — standard systemctl service

In `config/defaults.go`, append to the slice returned by `DefaultServices()`:

```go
{
    ID:           "my-service",
    Label:        "My Service",
    Start:        "systemctl start my-service",
    Stop:         "systemctl stop my-service",
    Restart:      "systemctl restart my-service",
    Status:       "systemctl is-active my-service",
    StatusRegex:  `(?P<status>active|inactive|failed)`,
    Version:      "my-service --version",
    VersionRegex: `(?P<version>[\d.]+)`,
    Log:          "/var/log/my-service/my-service.log",
    Installable:  true,  // only if you also add an Installer (see install-package skill)
},
```

## Example — supervised child process (Managed)

For services that run as child processes of devctl (like Laravel Reverb):

```go
{
    ID:          "reverb",
    Label:       "Laravel Reverb",
    Managed:     true,
    ManagedCmd:  "php",
    ManagedArgs: "artisan reverb:start --host=127.0.0.1 --port=7383",
    Log:         "",  // expanded at runtime: $HOME/sites/reverb/storage/logs/laravel.log
    Installable: true,
},
```

- Leave `Start`, `Stop`, `Restart`, `Status`, `StatusRegex` empty — the supervisor handles these.
- Working dir is `$HOME/sites/<id>` by convention (set automatically by the supervisor).
- The supervisor auto-starts installed managed services on devctl startup.

## PHP-FPM services (auto-generated)

PHP-FPM services are **not** in `defaults.go`. They are auto-generated when a PHP version is installed, keyed as `php8.3-fpm`, `php8.2-fpm`, etc. See `php/installer.go` for that logic — don't add PHP-FPM entries manually.

## Checklist when adding a new service

- [ ] Add an entry to `DefaultServices()` in `config/defaults.go`
- [ ] If `Managed: true`, set `ManagedCmd` and `ManagedArgs`; leave systemctl fields empty
- [ ] If `Installable: true`, also add an `Installer` in the `install/` package (see `install-package` skill) and register it in `install.NewRegistry(...)`
- [ ] If not Managed: verify `StatusRegex` captures `active`, `inactive`, or `failed` correctly
- [ ] Verify `VersionRegex` captures the version string (or leave `Version`/`VersionRegex` empty)
- [ ] If the service has a log file, set `Log` to its absolute path
