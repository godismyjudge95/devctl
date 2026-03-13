package services

// Status represents the running state of a service.
type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusUnknown Status = "unknown"
	StatusPending Status = "pending" // transitional: activating / deactivating / reloading
	StatusWarning Status = "warning" // process is up but health check failed
)

// Definition holds the configuration for a single managed service.
type Definition struct {
	ID           string
	Label        string
	Start        string
	Stop         string
	Restart      string
	Status       string
	StatusRegex  string
	Version      string
	VersionRegex string
	Log          string
	// CredentialsFile overrides the default config.env path for the credentials
	// endpoint. If empty, the default path ($siteHome/sites/server/<id>/config.env)
	// is used.
	CredentialsFile string
	// Installable marks that devctl can install/purge this service via the
	// install package. Corresponds to a registered Installer keyed by ID.
	Installable bool
	// Required marks that devctl depends on this service to function correctly.
	// Required services cannot be purged.
	Required bool
	// Managed marks that devctl runs this service as a supervised child process
	// rather than delegating to systemctl shell commands.
	Managed bool
	// ManagedCmd is the executable for a Managed service (e.g. "php").
	ManagedCmd string
	// ManagedArgs are the arguments for ManagedCmd
	// (e.g. "artisan reverb:start --host=127.0.0.1 --port=7383").
	ManagedArgs string
	// ManagedDir overrides the working directory for a Managed service.
	// If empty, the supervisor uses $siteHome/sites/<ID> by convention.
	ManagedDir string
	// ManagedEnvFile is a path to a key=value file whose contents are
	// appended to ManagedArgs at process start (used to inject secrets
	// that are only known after installation, e.g. a Meilisearch master key).
	ManagedEnvFile string
	// HealthCheck is an optional shell command run after the service is
	// confirmed running. A non-zero exit code causes the status to be
	// reported as StatusWarning instead of StatusRunning.
	HealthCheck string
}

// ServiceState is the live status of a service returned by the API.
type ServiceState struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Status      Status `json:"status"`
	Version     string `json:"version"`
	Log         string `json:"log"`
	Installed   bool   `json:"installed"`
	Installable bool   `json:"installable"`
	Required    bool   `json:"required"`
}
