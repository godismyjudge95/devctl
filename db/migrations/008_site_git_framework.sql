-- +goose Up
ALTER TABLE sites ADD COLUMN is_git_repo    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sites ADD COLUMN git_remote_url TEXT    NOT NULL DEFAULT '';
ALTER TABLE sites ADD COLUMN framework      TEXT    NOT NULL DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN cleanly; omitting for dev safety.
