-- name: ListPHPIniDirectives :many
SELECT scope, key, value FROM php_ini_directives ORDER BY scope ASC, key ASC;

-- name: SetPHPIniDirective :exec
INSERT INTO php_ini_directives (scope, key, value) VALUES (?, ?, ?)
ON CONFLICT(scope, key) DO UPDATE SET value = excluded.value;

-- name: DeletePHPIniDirective :execrows
DELETE FROM php_ini_directives WHERE scope = ? AND key = ?;
