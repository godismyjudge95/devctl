-- name: ListWorkers :many
SELECT w.id, w.site_id, w.type, w.command, w.enabled, s.domain, s.root_path, s.php_version
FROM site_workers w
JOIN sites s ON s.id = w.site_id
ORDER BY s.domain ASC, w.id ASC;

-- name: CreateWorker :one
INSERT INTO site_workers (site_id, type, command) VALUES (?, ?, ?)
RETURNING id;

-- name: SetWorkerEnabled :exec
UPDATE site_workers SET enabled = ? WHERE id = ?;

-- name: DeleteWorker :exec
DELETE FROM site_workers WHERE id = ?;
