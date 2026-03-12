-- +goose Up
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO settings (key, value) VALUES
    ('devctl_port',           '4000'),
    ('devctl_host',           '127.0.0.1'),
    ('dump_tcp_port',         '9912'),
    ('dump_max_entries',      '500'),
    ('caddy_admin_url',       'http://localhost:2019'),
    ('sites_watch_dir',       ''),
    ('sites_auto_discover',   'true'),
    ('default_php_version',   '8.3'),
    ('php_extensions',        '["bcmath","curl","gd","imagick","intl","mbstring","mysql","pdo-mysql","pgsql","pdo-pgsql","redis","sqlite3","xml","xmlwriter","zip","opcache","readline","soap"]'),
    ('mailpit_http_port',     '8025'),
    ('mailpit_smtp_port',     '1025'),
    ('service_poll_interval', '5');

-- +goose Down
DROP TABLE settings;
