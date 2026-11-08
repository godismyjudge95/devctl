-- +goose Up
-- php.ini directives devctl writes into a managed block at the end of each
-- version's php.ini. scope is 'all' (every installed version) or a PHP minor
-- version such as '8.4'; a version row wins over an 'all' row for the same key.
CREATE TABLE php_ini_directives (
    scope TEXT NOT NULL,
    key   TEXT NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (scope, key)
);

-- +goose Down
DROP TABLE php_ini_directives;
