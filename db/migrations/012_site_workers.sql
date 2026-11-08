-- +goose Up
-- Background processes devctl supervises for a site (queue, horizon,
-- schedule, vite, custom). enabled is the desired state: devctl starts
-- enabled workers again after a restart.
CREATE TABLE site_workers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id    TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    command    TEXT NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE site_workers;
