-- name: InsertDump :one
INSERT INTO dumps (file, line, nodes, timestamp, site_domain)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetDumps :many
SELECT * FROM dumps
ORDER BY id DESC
LIMIT ? OFFSET ?;

-- name: GetDumpsBySite :many
SELECT * FROM dumps
WHERE site_domain = ?
ORDER BY id DESC
LIMIT ? OFFSET ?;

-- name: CountDumps :one
SELECT COUNT(*) FROM dumps;

-- name: CountDumpsBySite :one
SELECT COUNT(*) FROM dumps WHERE site_domain = ?;

-- name: DeleteAllDumps :exec
DELETE FROM dumps;

-- name: PruneOldDumps :exec
DELETE FROM dumps WHERE id NOT IN (
    SELECT id FROM dumps ORDER BY id DESC LIMIT ?
);
