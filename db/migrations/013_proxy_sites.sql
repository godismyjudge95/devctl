-- +goose Up
-- A site is a proxy site when its settings hold a "proxy" object
-- ({"upstream": "http://127.0.0.1:7383", "preserve_host": true}). Service
-- vhosts were "ws" sites with a bare dial address; they keep the original
-- Host header. site_type is no longer read.
UPDATE sites
SET settings = json_set(
        json_remove(settings, '$.site_type', '$.ws_upstream'),
        '$.proxy',
        json_object('upstream', 'http://' || json_extract(settings, '$.ws_upstream'), 'preserve_host', json('true'))),
    php_version = ''
WHERE json_valid(settings) AND json_extract(settings, '$.site_type') = 'ws';

UPDATE sites
SET settings = json_remove(settings, '$.site_type')
WHERE json_valid(settings) AND json_extract(settings, '$.site_type') IS NOT NULL;

-- +goose Down
UPDATE sites
SET settings = json_set(
        json_remove(settings, '$.proxy'),
        '$.site_type', 'ws',
        '$.ws_upstream', replace(replace(json_extract(settings, '$.proxy.upstream'), 'https://', ''), 'http://', ''))
WHERE json_valid(settings) AND json_extract(settings, '$.proxy') IS NOT NULL;

UPDATE sites
SET settings = json_set(settings, '$.site_type', 'php')
WHERE json_valid(settings) AND json_extract(settings, '$.site_type') IS NULL;
