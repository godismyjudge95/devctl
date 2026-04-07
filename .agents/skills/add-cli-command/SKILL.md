---
name: add-cli-command
description: How to add a new CLI command to devctl — writing the Cmd struct, registering it with init(), adding Client methods, using output helpers, and documenting it in README.md
license: MIT
compatibility: opencode
metadata:
  layer: cli
  concerns: cli, commands, readme
---

## Overview

The devctl binary doubles as a CLI. Every CLI command is a `*cli.Cmd` registered via `init()` in a per-namespace file under `cli/`. Commands communicate with the running daemon through the thin HTTP client in `cli/client.go`. After adding a command you **must** update `README.md`.

## File layout

```
cli/
├── registry.go      # Cmd struct, Register(), Dispatch(), help rendering
├── client.go        # HTTP client + all API wrapper methods + shared types
├── output.go        # output helpers: PrintOK, PrintErr, PrintJSON, Table, KV, Header, styles
├── services.go      # services:* commands
├── sites.go         # sites:* commands
├── php.go           # php:* commands
├── logs.go          # logs:* commands
├── dumps.go         # dumps:* commands
├── spx.go           # spx:* commands
├── mail.go          # mail:* commands
├── dns.go           # dns:* commands
├── tls.go           # tls:* commands
├── settings.go      # settings:* commands
├── devctl.go        # devctl:* commands (update, skill)
└── skill.go         # devctl:skill generator — auto-regenerates ~/.agents/skills/devctl-cli/SKILL.md
```

One file per namespace. Add commands for an **existing** namespace to the matching file. For a **new** namespace, create a new file `cli/<namespace>.go`.

## The `Cmd` struct

Defined in `cli/registry.go`:

```go
type Cmd struct {
    Name        string            // full name: "namespace:verb"  e.g. "sites:php"
    Description string            // one-line description shown in help and skill
    Usage       string            // argument signature after the command name, e.g. "<domain> <version>"
    Args        []ArgDef          // positional argument documentation
    Flags       []FlagDef         // flag documentation (used by help + skill generator)
    Examples    []string          // usage examples shown in help and skill
    Handler     func(c *Client, args []string, jsonMode bool) error
}

type ArgDef struct {
    Name        string
    Description string
    Optional    bool
}

type FlagDef struct {
    Name        string   // flag name without "--", e.g. "output"
    Default     string   // default value (displayed in help)
    Description string
}
```

## Registering a command

Use `init()` — the `init()` in each file runs automatically at startup:

```go
package cli

import "fmt"

func init() {
    Register(&Cmd{
        Name:        "sites:open",
        Description: "Open a site in the browser",
        Usage:       "<domain>",
        Args:        []ArgDef{{Name: "domain", Description: "Site domain (e.g. myapp.test)"}},
        Examples:    []string{"devctl sites:open myapp.test", "devctl sites:open myapp.test --json"},
        Handler: func(c *Client, args []string, jsonMode bool) error {
            if len(args) == 0 {
                return fmt.Errorf("usage: devctl sites:open <domain>")
            }
            domain := args[0]
            if err := c.OpenSite(domain); err != nil {
                return err
            }
            if jsonMode {
                PrintJSON(map[string]string{"status": "ok", "domain": domain})
                return nil
            }
            PrintOK("Opened " + domain)
            return nil
        },
    })
}
```

### Naming rules

- Format: `<namespace>:<verb>` — both lowercase, hyphen-separated words
- Namespace must be consistent with the file name: `sites:*` → `cli/sites.go`
- The namespace is used for grouping in help output and `devctl <namespace>` autocomplete

## Adding a Client method

All HTTP calls go through `cli/client.go`. Add your API wrapper method there:

```go
// OpenSite calls POST /api/sites/{domain}/open on the daemon.
func (c *Client) OpenSite(domain string) error {
    return c.post("/api/sites/"+domain+"/open", nil, nil)
}
```

### Available HTTP helpers on `*Client`

| Method | Signature | Use when |
|---|---|---|
| `get` | `(path string, out any) error` | GET and decode JSON response |
| `getRaw` | `(path string) (string, error)` | GET and return raw string body |
| `post` | `(path string, body any, out any) error` | POST JSON body, optionally decode response |
| `put` | `(path string, body any, out any) error` | PUT JSON body |
| `delete` | `(path string) error` | DELETE with no body |
| `deleteWithBody` | `(path string, body any) error` | DELETE with JSON body |

For SSE-streaming endpoints (long-running installs, updates), follow the pattern in `InstallServiceSSE` / `UpdateServiceSSE` — use a long-timeout `http.Client` (`10 * time.Minute`) and parse `event:` / `data:` lines manually.

## Output helpers (cli/output.go)

Always use these — never call `fmt.Fprintf(os.Stderr, ...)` directly:

| Helper | Signature | Use for |
|---|---|---|
| `PrintOK(msg)` | `string` | Success messages (green ✓) |
| `PrintErr(err)` | `error` | Error messages to stderr (red) — called by Dispatch, not usually needed in handlers |
| `PrintJSON(v)` | `any` | JSON output when `--json` flag is set |
| `Header(s)` | `string` | Bold section header |
| `KV(key, val)` | `string, string` | Aligned key-value pair (indented) |
| `Table(headers, rows)` | `[]string, [][]string` | Aligned table with header row and divider |
| `StatusStyle(status)` | `string → string` | Coloured status string: `running`, `stopped`, `pending`, `warning` |

Styles for manual formatting:

```go
styleDim   // gray  — secondary info
styleBold  // bold  — emphasis
styleLabel // bold cyan — labels/headings
styleOK    // green
styleWarn  // yellow
styleErr   // red
```

## JSON mode pattern

Every command **must** support `--json`. The `jsonMode bool` parameter is `true` when the user passes `--json`. Typical pattern:

```go
Handler: func(c *Client, args []string, jsonMode bool) error {
    result, err := c.SomeAPICall()
    if err != nil {
        return err   // Dispatch handles printing the error
    }
    if jsonMode {
        PrintJSON(result)
        return nil
    }
    // human-readable output
    Table([]string{"Col1", "Col2"}, rows)
    return nil
},
```

For SSE-streaming commands in JSON mode, emit per-line events:

```go
type outputEvent struct {
    Type string `json:"type"`
    Line string `json:"line,omitempty"`
}
err := c.SomeSSECall(id, func(line string) {
    PrintJSON(outputEvent{Type: "output", Line: line})
})
if err != nil {
    PrintJSON(map[string]string{"type": "error", "error": err.Error()})
    return err
}
PrintJSON(map[string]string{"type": "done", "id": id})
```

## Parsing flags inside a handler

The `args` slice contains everything after the command name (flags not yet stripped). Parse custom flags with `flag.NewFlagSet`:

```go
Handler: func(c *Client, args []string, jsonMode bool) error {
    fs := flag.NewFlagSet("sites:open", flag.ContinueOnError)
    browser := fs.String("browser", "", "browser to use (e.g. firefox)")
    _ = fs.Parse(args)
    positional := fs.Args()
    if len(positional) == 0 {
        return fmt.Errorf("usage: devctl sites:open <domain>")
    }
    // ...
},
```

Document every custom flag in the `Flags []FlagDef` field — the `devctl:skill` generator reads them.

## Updating README.md

**Every new command must be documented in `README.md`.** The CLI section starts at `## CLI` (around line 484). Two places need updates:

### 1. Quick-start snippet

If the command is commonly used or showcases a new namespace, add it to the code block:

```sh
devctl services:list              # list all services and status
devctl sites:open myapp.test      # open a site in the browser   ← add here
...
```

### 2. Available commands table

Add a row in the `### Available commands` table under the correct namespace. If it is a new namespace, add a new namespace group:

```markdown
| `sites` | `sites:list` | List all sites |
|         | `sites:get <domain>` | Show full details for a site |
|         | `sites:open <domain>` | Open a site in the browser |   ← add here
```

If the command belongs to a **new namespace**, add a new block:

```markdown
| `myns` | `myns:list` | List things |
|        | `myns:get <id>` | Get a thing |
```

Keep the table sorted: `services`, `sites`, `php`, `logs`, `dumps`, `spx`, `mail`, `dns`, `tls`, `settings`, `devctl`.

## Auto-generated CLI skill

`cli/skill.go` generates `~/.agents/skills/devctl-cli/SKILL.md` from the live command registry. The daemon regenerates it silently on every startup (if the file already exists). After adding a command, run:

```sh
devctl devctl:skill
```

This updates the agent skill so AI tools immediately know about the new command. You do **not** need to manually edit the auto-generated skill file.

## Checklist

- [ ] Add the command to the correct `cli/<namespace>.go` file (or create a new one for a new namespace)
- [ ] Fill in `Name`, `Description`, `Usage`, `Args`, `Flags`, `Examples` on the `Cmd` struct
- [ ] Add any new API wrapper methods to `cli/client.go`
- [ ] Support `--json` mode in the handler — emit `PrintJSON(...)` output and return early
- [ ] Return a descriptive error (not `os.Exit`) on bad input; Dispatch handles printing it
- [ ] Update the **quick-start snippet** in `README.md § CLI` if the command is notable
- [ ] Update the **Available commands table** in `README.md § CLI › Available commands`
- [ ] Run `devctl devctl:skill` to regenerate the agent skill file
- [ ] Build and test: `make build && make install`, then `devctl <new-command> --help`
