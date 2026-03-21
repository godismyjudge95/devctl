package config

import (
	"context"
	"io"

	"github.com/danielgormly/devctl/dnsserver"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

// DefaultServices returns the built-in service definitions.
// serverRoot is the absolute path to the devctl server directory
// (e.g. "/home/alice/ddev/sites/server").
// siteUser is the username of the non-root site user (e.g. "alice") — required
// for services that must not run as root (e.g. PostgreSQL uses ManagedUser to
// drop privs).
func DefaultServices(serverRoot, siteUser string) []services.Definition {
	caddyDir := paths.ServiceDir(serverRoot, "caddy")
	meiliDir := paths.ServiceDir(serverRoot, "meilisearch")
	tsDir := paths.ServiceDir(serverRoot, "typesense")
	valkeyDir := paths.ServiceDir(serverRoot, "valkey")
	mailpitDir := paths.ServiceDir(serverRoot, "mailpit")
	mysqlDir := paths.ServiceDir(serverRoot, "mysql")
	postgresDir := paths.ServiceDir(serverRoot, "postgres")
	reverbDir := paths.ServiceDir(serverRoot, "reverb")
	whodbDir := paths.ServiceDir(serverRoot, "whodb")
	rustfsDir := paths.ServiceDir(serverRoot, "rustfs")
	return []services.Definition{
		{
			ID:             "caddy",
			Label:          "Caddy",
			Description:    "Fast, production-ready reverse proxy and TLS terminator",
			InstallVersion: "v2.10.0",
			Installable:    true,
			Required:       true,
			Managed:        true,
			ManagedCmd:     caddyDir + "/caddy",
			ManagedArgs:    "run",
			ManagedDir:     caddyDir,
			ManagedEnvFile: caddyDir + "/caddy.env",
			Version:        caddyDir + "/caddy version",
			VersionRegex:   `v(?P<version>[\d.]+)`,
			Log:            paths.LogPath(serverRoot, "caddy"),
			HealthCheck:    "curl -sf http://localhost:2019/config/",
		},
		{
			ID:              "redis",
			Label:           "Valkey",
			Description:     "High-performance Redis-compatible in-memory data store",
			InstallVersion:  "9.0.3",
			Installable:     true,
			HasCredentials:  true,
			Managed:         true,
			ManagedCmd:      valkeyDir + "/valkey-server",
			ManagedArgs:     valkeyDir + "/valkey.conf",
			ManagedDir:      valkeyDir,
			Version:         valkeyDir + "/valkey-server --version",
			VersionRegex:    `v=(?P<version>[\d.]+)`,
			CredentialsFile: valkeyDir + "/config.env",
			Log:             paths.LogPath(serverRoot, "redis"),
		},
		{
			ID:              "postgres",
			Label:           "PostgreSQL",
			Description:     "Powerful open-source relational database",
			InstallVersion:  "18.3",
			Installable:     true,
			HasCredentials:  true,
			Managed:         true,
			ManagedCmd:      postgresDir + "/bin/postgres",
			ManagedArgs:     "-D " + postgresDir + "/data",
			ManagedDir:      postgresDir,
			ManagedUser:     siteUser,
			Version:         postgresDir + "/bin/postgres --version",
			VersionRegex:    `(?P<version>[\d.]+)`,
			CredentialsFile: postgresDir + "/config.env",
			Log:             paths.LogPath(serverRoot, "postgres"),
			HealthCheck:     postgresDir + "/bin/pg_isready -h 127.0.0.1 -p 5432 -U " + siteUser + " -d postgres",
		},
		{
			ID:              "mysql",
			Label:           "MySQL",
			Description:     "Popular open-source relational database",
			InstallVersion:  "8.4.8",
			Installable:     true,
			HasCredentials:  true,
			Managed:         true,
			ManagedCmd:      mysqlDir + "/bin/mysqld",
			ManagedArgs:     "--defaults-file=./my.cnf --user=root",
			ManagedDir:      mysqlDir,
			ManagedEnvFile:  mysqlDir + "/mysql.env",
			Version:         mysqlDir + "/bin/mysql --version",
			VersionRegex:    `(?P<version>[\d.]+)`,
			CredentialsFile: mysqlDir + "/config.env",
			Log:             paths.LogPath(serverRoot, "mysql"),
			HealthCheck:     mysqlDir + "/bin/mysqladmin --socket=" + mysqlDir + "/mysql.sock ping",
		},
		{
			ID:             "meilisearch",
			Label:          "Meilisearch",
			Description:    "Lightning-fast search engine with instant results",
			InstallVersion: "v1.37.0",
			Installable:    true,
			HasCredentials: true,
			Managed:        true,
			ManagedCmd:     meiliDir + "/meilisearch",
			ManagedArgs:    "--config-file-path ./config.toml",
			ManagedDir:     meiliDir,
			ManagedEnvFile: meiliDir + "/config.env",
			Version:        meiliDir + "/meilisearch --version",
			VersionRegex:   `meilisearch (?P<version>[\d.]+)`,
			Log:            paths.LogPath(serverRoot, "meilisearch"),
		},
		{
			ID:             "typesense",
			Label:          "Typesense",
			Description:    "Open-source typo-tolerant search engine",
			InstallVersion: "30.1",
			Installable:    true,
			HasCredentials: true,
			Managed:        true,
			ManagedCmd:     tsDir + "/typesense-server",
			ManagedArgs:    "--config=./typesense.ini",
			ManagedDir:     tsDir,
			ManagedEnvFile: tsDir + "/config.env",
			Version:        tsDir + "/typesense-server --version",
			VersionRegex:   `Typesense (?P<version>[\d.]+)`,
			Log:            paths.LogPath(serverRoot, "typesense"),
		},
		{
			ID:              "mailpit",
			Label:           "Mailpit",
			Description:     "Email testing tool with SMTP server and web UI",
			InstallVersion:  "v1.29.2",
			Installable:     true,
			HasCredentials:  true,
			Managed:         true,
			ManagedCmd:      mailpitDir + "/mailpit",
			ManagedArgs:     "",
			ManagedDir:      mailpitDir,
			ManagedEnvFile:  mailpitDir + "/config.env",
			Version:         mailpitDir + "/mailpit version",
			VersionRegex:    `v(?P<version>[\d.]+)`,
			CredentialsFile: mailpitDir + "/connection.env",
			Log:             paths.LogPath(serverRoot, "mailpit"),
		},
		{
			ID:             "reverb",
			Label:          "Laravel Reverb",
			Description:    "First-party WebSocket server for Laravel applications",
			InstallVersion: "latest",
			Installable:    true,
			Managed:        true,
			ManagedCmd:     "php",
			ManagedArgs:    "artisan reverb:start --host=127.0.0.1 --port=7383",
			ManagedDir:     reverbDir,
			Version:        `grep -m1 '"version"' ` + reverbDir + `/vendor/laravel/reverb/composer.json`,
			VersionRegex:   `"version": "(?P<version>[^"]+)"`,
			Log:            paths.LogPath(serverRoot, "reverb"),
			// Start/Stop/Restart/Status are handled by the Supervisor, not shell commands.
		},
		{
			ID:          "dns",
			Label:       "DNS Server",
			Description: "Intercepts .test TLD queries and returns your machine's LAN IP",
			Required:    true,
			Managed:     true,
			Installable: true,
			Log:         paths.LogPath(serverRoot, "dns"),
			// Default RunFunc uses hardcoded defaults; dnsDef() in the API layer
			// overwrites this with DB-configured values at runtime.
			RunFunc: func(ctx context.Context, logW io.Writer) error {
				return dnsserver.New(dnsserver.Config{
					Port:     "5354",
					TargetIP: dnsserver.DetectLANIP(),
					TLDs:     []string{".test"},
					Upstream: dnsserver.SystemUpstream(),
				}).Run(ctx, logW)
			},
		},
		{
			ID:             "whodb",
			Label:          "WhoDB",
			Description:    "Lightweight database explorer with web UI",
			InstallVersion: "0.100.0",
			Installable:    true,
			Managed:        true,
			ManagedCmd:     whodbDir + "/whodb",
			ManagedArgs:    "",
			ManagedDir:     whodbDir,
			ManagedEnvFile: whodbDir + "/config.env",
			Log:            paths.LogPath(serverRoot, "whodb"),
			HealthCheck:    "curl -sf http://localhost:8161/",
		},
		{
			ID:              "rustfs",
			Label:           "RustFS",
			Description:     "High-performance S3-compatible object storage",
			InstallVersion:  "latest",
			Installable:     true,
			Managed:         true,
			ManagedCmd:      rustfsDir + "/rustfs",
			ManagedArgs:     rustfsDir + "/data",
			ManagedDir:      rustfsDir,
			ManagedEnvFile:  rustfsDir + "/config.env",
			Version:         rustfsDir + "/rustfs --version",
			VersionRegex:    `rustfs (?P<version>[\S]+)`,
			Log:             paths.LogPath(serverRoot, "rustfs"),
			HealthCheck:     "curl -s --connect-timeout 2 -o /dev/null http://localhost:9000/minio/health/live",
			HasCredentials:  true,
			CredentialsFile: rustfsDir + "/connection.env",
		},
	}
}
