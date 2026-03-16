-- +goose Up
ALTER TABLE sites ADD COLUMN service_vhost INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions; recreate table without the column.
CREATE TABLE sites_backup AS SELECT id, domain, root_path, php_version, aliases, spx_enabled, https, auto_discovered, created_at, updated_at, settings, parent_site_id, worktree_branch, public_dir FROM sites;
DROP TABLE sites;
ALTER TABLE sites_backup RENAME TO sites;
