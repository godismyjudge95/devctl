CREATE TABLE IF NOT EXISTS sites (
    id               TEXT PRIMARY KEY,
    domain           TEXT NOT NULL UNIQUE,
    root_path        TEXT NOT NULL,
    php_version      TEXT NOT NULL DEFAULT '8.3',
    aliases          TEXT NOT NULL DEFAULT '[]',
    spx_enabled      INTEGER NOT NULL DEFAULT 0,
    https            INTEGER NOT NULL DEFAULT 1,
    auto_discovered  INTEGER NOT NULL DEFAULT 0,
    settings         TEXT NOT NULL DEFAULT '{}',
    parent_site_id   TEXT DEFAULT NULL,
    worktree_branch  TEXT DEFAULT NULL,
    public_dir       TEXT NOT NULL DEFAULT '',
    service_vhost    INTEGER NOT NULL DEFAULT 0,
    is_git_repo      INTEGER NOT NULL DEFAULT 0,
    git_remote_url   TEXT    NOT NULL DEFAULT '',
    framework        TEXT    NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS dumps (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file        TEXT,
    line        INTEGER,
    nodes       TEXT NOT NULL,
    timestamp   REAL NOT NULL,
    site_domain TEXT
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
