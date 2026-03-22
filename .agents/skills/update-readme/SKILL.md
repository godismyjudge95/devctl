---
name: update-readme
description: How to keep README.md accurate and well-structured when devctl features change — canonical section order, per-section rules, source-of-truth files, style rules, and a pre-save checklist.
---

# Skill: update-readme

## Overview

This skill governs all edits to `README.md` in the devctl project root. It describes the canonical section order, which source files to verify accuracy against for each section, common drift points, style rules, and a pre-save checklist.

Load this skill any time you are asked to:
- Update the README after adding a feature or service
- Audit the README for accuracy
- Add a new section or reorder existing sections

---

## Canonical TOC order

The README must always contain a full TOC with anchor links. The sections must appear in this exact order:

1. Overview
2. Requirements
3. Installation
   - From a release binary
   - From source
4. Uninstall
5. Services
6. PHP
7. DNS
8. Sites
9. Git Worktrees
10. PHP Dumps (`php_dd`)
11. SPX Profiler
12. Mail
13. Config Editor
14. Browser Notifications
15. MCP Server
16. Ports
17. Data Paths
18. Contributing & Development
19. License

Do not add, remove, or reorder sections without updating the TOC anchor links to match.

---

## Source-of-truth files per section

Cross-check these files when updating each section to ensure accuracy.

| Section | Files to verify against |
|---|---|
| Requirements | `selfinstall/selfinstall.go` (OS checks, root check) |
| Installation | `selfinstall/selfinstall.go` (`--user`, `--sites-dir`, `--path`, `--yes` flags; default paths) |
| Uninstall | `selfinstall/selfinstall.go` (`--yes`, `--purge-services` flags; what is removed) |
| Services (table) | `config/defaults.go` (all service IDs, ports, config file paths); `install/*.go` (download URLs, binary locations) |
| PHP | `install/php.go`, `php/php.go`; `config/defaults.go` (PHP FPM config path pattern) |
| DNS | `dnsserver/` package; `api/` DNS settings handlers; `selfinstall/selfinstall.go` |
| Sites | `sites/` package; `api/server.go` site routes |
| Git Worktrees | `api/` worktree handlers; `sites/` worktree logic |
| PHP Dumps | `dumps/` package; `paths/paths.go` (`PrependPHPPath`) |
| SPX Profiler | `php/php.go` (SPX ini settings); `api/` SPX handlers |
| Mail | `config/defaults.go` (Mailpit ports and config path); `install/mailpit.go` |
| Config Editor | `api/` config editor handlers; `config/defaults.go` (which services have `ConfigFile`) |
| Browser Notifications | `frontend/src/` (notification logic) |
| MCP Server | `mcpserver/` package (tools, prompts, resources) |
| Ports | `config/defaults.go` (all service ports); `main.go` (dashboard/dump port defaults) |
| Data Paths | `paths/paths.go` — **single source of truth**; `selfinstall/selfinstall.go` (profile.d, systemd unit) |
| Contributing | `AGENTS.md`, `Makefile`, `api/server.go`, `go.mod` |

---

## Per-section rules

### Services table

The services table has five columns: **Service**, **Port(s)**, **`.test` vhost**, **Download source**, **Config file**.

- Port(s): use exact bind address (`127.0.0.1:port`). List all ports for multi-port services.
- `.test` vhost: write the hostname (e.g. `meilisearch.test`) or `—` if the service has no vhost.
- Download source: link the exact domain and path where the installer fetches the binary. Verify in `install/{service}.go`.
- Config file: use `{serverRoot}/...` notation. Verify the exact path in `config/defaults.go`.
- Rows appear in this order: Caddy, DNS Server, Valkey, PostgreSQL, MySQL, Meilisearch, Typesense, Mailpit, Laravel Reverb, WhoDB, RustFS, PHP-FPM.
- Services notes after the table must mention:
  - Supervised vs PostgreSQL privilege drop
  - Valkey's service ID `redis` (for Laravel compatibility)
  - Config files written once, never overwritten
  - Mailpit `MP_*` env var config pattern
  - Meilisearch autonomous update (dump → replace → re-import)

### Ports table

- Ports appear in the same order as the services table.
- Mark a port as **Yes** (configurable) only if it has a corresponding setting in the Settings tab or a CLI flag.
- Verify configurable ports against `api/` settings handlers and `main.go` flag defaults.

### Data Paths table

- Every path must use `{serverRoot}/...` notation — never `/etc/devctl/` or any hardcoded absolute path.
- `{serverRoot}` explanation must appear above the table: it equals `{sitesDir}/server`, defaults to `~/sites/server`, is stored in the systemd unit as `DEVCTL_SERVER_ROOT`, and is the single source of truth.
- Verify every path against `paths/paths.go`.
- Rows include: `devctl.db`, `prepend.php`, devctl binary, `bin/`, `logs/`, one row per service dir, `php/{version}/`, systemd unit file, `profile.d` entry.

### Installation section

- Verify all flag names against `selfinstall/selfinstall.go` (flags are defined there).
- Default binary install location is `{sites-dir}/server/devctl/devctl` — not `/usr/local/bin`.
- The `make install` note must mention that the user must edit `HOME`, `DEVCTL_SITE_USER`, and `DEVCTL_SERVER_ROOT` in the generated unit file.

### MCP Server section

The tools, prompts, and resources tables must match `mcpserver/` exactly. When new tools are added, add a row; when tools are removed, remove the row.

### Contributing & Development section

Must include: tech stack table, build commands block, package layout table, key conventions list. Verify `make` targets against `Makefile`.

---

## Path notation rules

- Always use `{serverRoot}` as the placeholder — never `~/sites/server`, `/etc/devctl/`, or any other hardcoded path.
- Explain once (in Data Paths) that `{serverRoot}` defaults to `{sitesDir}/server` and is fully configurable via `DEVCTL_SERVER_ROOT`.
- Other sections may reference `{serverRoot}/...` paths without re-explaining the variable.

---

## Style rules

- One blank line between TOC entries and section dividers (`---`).
- Tables: pipe-aligned, header row always present, at least one space of padding each side of `|`.
- Code blocks: use `sh` for shell commands, `php` for PHP, `json` for JSON, `env` for `.env` files, `ini` for INI files.
- No trailing whitespace.
- No emojis unless explicitly requested.
- Sentence case for headings below H2. H1 and H2 use title case.
- Keep screenshot references using the filenames already in `docs/` — do not add `[placeholder]` comments.

---

## Common drift points

These areas are the most likely to become stale after code changes. Always re-check them:

| Area | Why it drifts |
|---|---|
| Services table — download URLs | Installer files change URLs when upstream moves or version pinning changes |
| Services table — config paths | `config/defaults.go` `ConfigFile` fields change when paths are reorganised |
| Ports table | New services added to `config/defaults.go` without corresponding README row |
| MCP tools list | `mcpserver/` gains new tools without README update |
| Data Paths table | `paths/paths.go` adds new path functions without README update |
| PHP CLI symlinks | Logic in `php/` changes symlink targets or names |
| Installation flags | `selfinstall/selfinstall.go` adds/renames flags without README update |
| Valkey service ID note | Easy to omit when rewriting the Services section |

---

## Pre-save checklist

Before finalising any README edit, verify each item:

- [ ] TOC matches all H2/H3 headings and is in canonical order
- [ ] All anchor links in the TOC are correct (GitHub lowercases, strips punctuation, replaces spaces with `-`)
- [ ] Services table has all five columns and all rows including PHP-FPM
- [ ] Download sources in services table verified against `install/*.go`
- [ ] Config file paths in services table verified against `config/defaults.go`
- [ ] All ports in the Ports table; none missing from `config/defaults.go`
- [ ] No `/etc/devctl/` anywhere — all paths use `{serverRoot}/...`
- [ ] Data Paths table verified against `paths/paths.go`
- [ ] MCP tools/prompts/resources tables verified against `mcpserver/`
- [ ] Installation flags table verified against `selfinstall/selfinstall.go`
- [ ] Make targets verified against `Makefile`
- [ ] No hardcoded absolute paths specific to the developer's machine
- [ ] Screenshot filenames use existing `docs/screenshot-*.png` names only
