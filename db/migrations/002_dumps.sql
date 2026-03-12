-- +goose Up
CREATE TABLE dumps (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file        TEXT,
    line        INTEGER,
    nodes       TEXT NOT NULL,
    timestamp   REAL NOT NULL,
    site_domain TEXT
);

CREATE INDEX dumps_site_domain ON dumps(site_domain);
CREATE INDEX dumps_timestamp ON dumps(timestamp DESC);

-- +goose Down
DROP TABLE dumps;
