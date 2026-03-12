-- name: GetSetting :one
SELECT value FROM settings WHERE key = ? LIMIT 1;

-- name: GetAllSettings :many
SELECT key, value FROM settings ORDER BY key ASC;

-- name: SetSetting :exec
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;

-- name: UpsertSettings :exec
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
