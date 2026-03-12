-- name: GetAllSites :many
SELECT * FROM sites ORDER BY created_at ASC;

-- name: GetSite :one
SELECT * FROM sites WHERE id = ? LIMIT 1;

-- name: GetSiteByDomain :one
SELECT * FROM sites WHERE domain = ? LIMIT 1;

-- name: CreateSite :one
INSERT INTO sites (id, domain, root_path, php_version, aliases, spx_enabled, https, auto_discovered, settings)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateSite :one
UPDATE sites
SET domain = ?, root_path = ?, php_version = ?, aliases = ?, spx_enabled = ?, https = ?, settings = ?, updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: DeleteSite :exec
DELETE FROM sites WHERE id = ?;

-- name: SetSiteSPX :exec
UPDATE sites SET spx_enabled = ?, updated_at = datetime('now') WHERE id = ?;
