-- +goose Up
ALTER TABLE sites ADD COLUMN public_dir TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN cleanly; omitting for dev safety.
