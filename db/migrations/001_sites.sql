-- +goose Up
CREATE TABLE sites (
    id               TEXT PRIMARY KEY,
    domain           TEXT NOT NULL UNIQUE,
    root_path        TEXT NOT NULL,
    php_version      TEXT NOT NULL DEFAULT '8.3',
    aliases          TEXT NOT NULL DEFAULT '[]',
    spx_enabled      INTEGER NOT NULL DEFAULT 0,
    https            INTEGER NOT NULL DEFAULT 1,
    auto_discovered  INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE sites;
