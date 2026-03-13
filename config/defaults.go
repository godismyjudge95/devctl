package config

// ServiceDef mirrors the structure for a service definition.
// Commands are shell strings executed via sh -c.
// Services with Managed:true are run as child processes of devctl (not systemctl).
type ServiceDef struct {
	ID           string `yaml:"id"`
	Label        string `yaml:"label"`
	Start        string `yaml:"start"`
	Stop         string `yaml:"stop"`
	Restart      string `yaml:"restart"`
	Status       string `yaml:"status"`
	StatusRegex  string `yaml:"status_regex"`
	Version      string `yaml:"version"`
	VersionRegex string `yaml:"version_regex"`
	Log          string `yaml:"log"`
	// CredentialsFile overrides the default config.env path for the credentials
	// endpoint. If empty, $siteHome/sites/server/<id>/config.env is used.
	CredentialsFile string `yaml:"credentials_file"`
	Installable     bool   `yaml:"installable"`
	// Required marks that devctl depends on this service to function correctly.
	// Required services cannot be purged and are always auto-started on boot.
	Required bool `yaml:"required"`
	// Managed marks that devctl supervises this service as a child process
	// instead of delegating to systemctl.
	Managed bool `yaml:"managed"`
	// ManagedCmd is the executable to run (e.g. "php").
	ManagedCmd string `yaml:"managed_cmd"`
	// ManagedArgs are the arguments passed to ManagedCmd (e.g. "artisan reverb:start --host=127.0.0.1 --port=7383").
	ManagedArgs string `yaml:"managed_args"`
	// ManagedDir overrides the working directory for the supervised process.
	// If empty, $HOME/sites/<id> is used by convention.
	ManagedDir string `yaml:"managed_dir"`
	// ManagedEnvFile is a path to a key=value file whose values are appended
	// as CLI flags at process start (used to inject secrets known only after install).
	ManagedEnvFile string `yaml:"managed_env_file"`
	// HealthCheck is an optional shell command run when the service is running.
	// A non-zero exit code causes the status to be reported as "warning".
	HealthCheck string `yaml:"health_check"`
}

// DefaultServices returns the built-in service definitions.
// siteHome is the home directory of the non-root site user (e.g. "/home/alice").
func DefaultServices(siteHome string) []ServiceDef {
	caddyDir := siteHome + "/sites/server/caddy"
	meiliDir := siteHome + "/sites/server/meilisearch"
	tsDir := siteHome + "/sites/server/typesense"
	valkeyDir := siteHome + "/sites/server/valkey"
	mailpitDir := siteHome + "/sites/server/mailpit"
	mysqlDir := siteHome + "/sites/server/mysql"
	return []ServiceDef{
		{
			ID:             "caddy",
			Label:          "Caddy",
			Installable:    true,
			Required:       true,
			Managed:        true,
			ManagedCmd:     caddyDir + "/caddy",
			ManagedArgs:    "run --resume",
			ManagedDir:     caddyDir,
			ManagedEnvFile: caddyDir + "/caddy.env",
			Version:        caddyDir + "/caddy version",
			VersionRegex:   `v(?P<version>[\d.]+)`,
			Log:            caddyDir + "/caddy.log",
			HealthCheck:    "curl -sf http://localhost:2019/config/",
		},
		{
			ID:              "redis",
			Label:           "Valkey",
			Installable:     true,
			Managed:         true,
			ManagedCmd:      valkeyDir + "/valkey-server",
			ManagedArgs:     "--bind 127.0.0.1 --port 6379 --daemonize no",
			ManagedDir:      valkeyDir,
			Version:         valkeyDir + "/valkey-server --version",
			VersionRegex:    `v=(?P<version>[\d.]+)`,
			CredentialsFile: valkeyDir + "/config.env",
			Log:             valkeyDir + "/valkey.log",
		},
		{
			ID:           "postgres",
			Label:        "PostgreSQL",
			Start:        "systemctl start postgresql",
			Stop:         "systemctl stop postgresql",
			Restart:      "systemctl restart postgresql",
			Status:       "systemctl is-active postgresql",
			StatusRegex:  `(?P<status>active|inactive|failed)`,
			Version:      "psql --version",
			VersionRegex: `(?P<version>[\d.]+)`,
			Log:          "/var/log/postgresql/postgresql-15-main.log",
			Installable:  true,
		},
		{
			ID:              "mysql",
			Label:           "MySQL",
			Installable:     true,
			Managed:         true,
			ManagedCmd:      mysqlDir + "/bin/mysqld",
			ManagedArgs:     "--defaults-file=./my.cnf --user=root",
			ManagedDir:      mysqlDir,
			Version:         mysqlDir + "/bin/mysql --version",
			VersionRegex:    `(?P<version>[\d.]+)`,
			CredentialsFile: mysqlDir + "/config.env",
			Log:             mysqlDir + "/mysql-error.log",
		},
		{
			ID:             "meilisearch",
			Label:          "Meilisearch",
			Installable:    true,
			Managed:        true,
			ManagedCmd:     meiliDir + "/meilisearch",
			ManagedArgs:    "--http-addr 127.0.0.1:7700 --no-analytics --db-path ./data.ms",
			ManagedDir:     meiliDir,
			ManagedEnvFile: meiliDir + "/config.env",
			Version:        meiliDir + "/meilisearch --version",
			VersionRegex:   `meilisearch (?P<version>[\d.]+)`,
			Log:            meiliDir + "/meilisearch.log",
		},
		{
			ID:             "typesense",
			Label:          "Typesense",
			Installable:    true,
			Managed:        true,
			ManagedCmd:     tsDir + "/typesense-server",
			ManagedArgs:    "--data-dir ./data --listen-port 8108 --enable-cors",
			ManagedDir:     tsDir,
			ManagedEnvFile: tsDir + "/config.env",
			Version:        tsDir + "/typesense-server --version",
			VersionRegex:   `Typesense (?P<version>[\d.]+)`,
			Log:            tsDir + "/typesense.log",
		},
		{
			ID:              "mailpit",
			Label:           "Mailpit",
			Installable:     true,
			Managed:         true,
			ManagedCmd:      mailpitDir + "/mailpit",
			ManagedArgs:     "--listen 127.0.0.1:8025 --smtp 127.0.0.1:1025 --database ./data/mailpit.db",
			ManagedDir:      mailpitDir,
			Version:         mailpitDir + "/mailpit version",
			VersionRegex:    `v(?P<version>[\d.]+)`,
			CredentialsFile: mailpitDir + "/config.env",
			Log:             mailpitDir + "/mailpit.log",
		},
		{
			ID:           "reverb",
			Label:        "Laravel Reverb",
			Installable:  true,
			Managed:      true,
			ManagedCmd:   "php",
			ManagedArgs:  "artisan reverb:start --host=127.0.0.1 --port=7383",
			Version:      `grep -m1 '"version"' ` + siteHome + `/sites/reverb/vendor/laravel/reverb/composer.json`,
			VersionRegex: `"version": "(?P<version>[^"]+)"`,
			Log:          siteHome + "/sites/reverb/storage/logs/laravel.log",
			// Start/Stop/Restart/Status are handled by the Supervisor, not shell commands.
		},
	}
}
