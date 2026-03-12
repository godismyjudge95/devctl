-- +goose Up
ALTER TABLE sites ADD COLUMN settings TEXT NOT NULL DEFAULT '{}';

-- +goose Down
-- SQLite does not support DROP COLUMN; intentionally omitted.
