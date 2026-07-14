-- +goose Up
-- Service vhosts (s3.maxio.test, meilisearch.test, reverb.test, …) are reverse
-- proxies that browser clients on *.test origins call cross-origin. Enable
-- Caddy CORS header injection for all existing service vhosts.
UPDATE sites SET cors = 1 WHERE service_vhost = 1;

-- +goose Down
UPDATE sites SET cors = 0 WHERE service_vhost = 1;
