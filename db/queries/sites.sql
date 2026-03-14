-- name: GetAllSites :many
SELECT * FROM sites ORDER BY created_at ASC;

-- name: GetSite :one
SELECT * FROM sites WHERE id = ? LIMIT 1;

-- name: GetSiteByDomain :one
SELECT * FROM sites WHERE domain = ? LIMIT 1;

-- name: GetSiteByRootPath :one
SELECT * FROM sites WHERE root_path = ? LIMIT 1;

-- name: GetWorktreesBySite :many
SELECT * FROM sites WHERE parent_site_id = ? ORDER BY created_at ASC;

-- name: CreateSite :one
INSERT INTO sites (id, domain, root_path, php_version, aliases, spx_enabled, https, auto_discovered, settings, parent_site_id, worktree_branch)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateSite :one
UPDATE sites
SET domain = ?, root_path = ?, php_version = ?, aliases = ?, spx_enabled = ?, https = ?, settings = ?, updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: UpdateSiteSettings :exec
UPDATE sites SET settings = ?, updated_at = datetime('now') WHERE id = ?;

-- name: SetSiteWorktreeInfo :exec
UPDATE sites SET parent_site_id = ?, worktree_branch = ?, updated_at = datetime('now') WHERE id = ?;

-- name: DeleteSite :exec
DELETE FROM sites WHERE id = ?;

-- name: SetSiteSPX :exec
UPDATE sites SET spx_enabled = ?, updated_at = datetime('now') WHERE id = ?;
