---
name: update-skills
description: How to create, update, or maintain agent skills for this project — file structure, frontmatter rules, naming constraints, and where OpenCode discovers them
license: MIT
compatibility: opencode
metadata:
  concerns: meta, skills, opencode
---

## Where skills live

Project skills are in `.agents/skills/<name>/SKILL.md` (OpenCode discovers this path automatically by walking up from the working directory to the git worktree root).

```
.agents/skills/
├── go-backend/SKILL.md
├── vue-frontend/SKILL.md
├── db-migrations/SKILL.md
├── add-service/SKILL.md
├── install-package/SKILL.md
└── update-skills/SKILL.md   ← this file
```

## Required frontmatter

Every `SKILL.md` **must** start with YAML frontmatter. Only these fields are recognised by OpenCode:

```yaml
---
name: my-skill-name          # required — must match the directory name
description: One sentence.   # required — 1–1024 chars; used by agents to decide when to load
license: MIT                 # optional
compatibility: opencode      # optional
metadata:                    # optional, string-to-string map
  key: value
---
```

## Naming rules

The `name` value:
- 1–64 characters
- Lowercase alphanumeric with single hyphens as separators
- No leading/trailing hyphens, no consecutive `--`
- **Must exactly match the directory name** containing `SKILL.md`

Valid: `go-backend`, `db-migrations`, `add-service`
Invalid: `Go-Backend`, `db--migrations`, `-add-service`

## Creating a new skill

1. Create a directory: `.agents/skills/<name>/`
2. Create `SKILL.md` inside it — uppercase, exact spelling.
3. Start the file with valid YAML frontmatter (`name` + `description` are mandatory).
4. Write the skill content in Markdown below the frontmatter.
5. Verify the `name` in frontmatter matches the directory name.

## Writing good skill content

- Lead with a short **Overview** section explaining what the skill covers.
- Use tables for lookup information (file paths, struct fields, helper functions).
- Include small, copy-pasteable code examples for the most common operations.
- End with a **Checklist** section for multi-step workflows.
- Keep descriptions specific: the agent uses `description` to decide whether to load the skill. Vague descriptions cause mis-loads or misses.

## Updating an existing skill

Edit the `SKILL.md` file directly. No regeneration step is needed — OpenCode reads the file on demand each time the skill is loaded.

When updating:
- Keep the `name` unchanged unless you also rename the directory.
- Update the `description` if the scope of the skill changes.
- Avoid breaking the YAML frontmatter — any syntax error will prevent the skill from loading.

## Current skills in this project

| Skill | Description |
|---|---|
| `go-backend` | Go API/service layer patterns, endpoints, SSE/WebSocket, sqlc, systemd context |
| `vue-frontend` | Pinia stores, api.ts wrappers, shadcn-vue, Tailwind v4, Vite/go:embed pipeline |
| `db-migrations` | goose SQL migration format, sqlc query codegen workflow |
| `add-service` | ServiceDefinition fields, config/defaults.go, PHP-FPM auto-generation |
| `install-package` | Installer interface, APT/systemctl helpers, idempotency, Registry registration |
| `update-skills` | This file — how to create/update project skills |
| `create-release` | Workflow for tagging/publishing devctl and PHP binaries releases, versioning, release notes from TODO.md |
