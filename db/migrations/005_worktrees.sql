-- +goose Up
ALTER TABLE sites ADD COLUMN parent_site_id TEXT DEFAULT NULL;
ALTER TABLE sites ADD COLUMN worktree_branch TEXT DEFAULT NULL;

-- +goose Down
-- SQLite does not support DROP COLUMN for these additions cleanly;
-- a full table rebuild would be required. Omitting for dev safety.
