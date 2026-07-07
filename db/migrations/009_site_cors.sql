-- +goose Up
ALTER TABLE sites ADD COLUMN cors INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sites DROP COLUMN cors;