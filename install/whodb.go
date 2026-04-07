package install

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

// WhoDBConnection represents a single pre-configured connection entry for WhoDB.
// WhoDB reads these from WHODB_POSTGRESQL / WHODB_MYSQL / WHODB_REDIS env vars
// as JSON arrays. Field names must match WhoDB's DatabaseCredentials struct
// (Hostname→"host", Username→"user", etc.).
type WhoDBConnection struct {
	Alias    string `json:"alias"`
	Host     string `json:"host,omitempty"`
	Port     string `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Database string `json:"database,omitempty"`
}

// WhoDBManualConnection is a user-defined connection stored in the DB.
// The Type field identifies the database engine (postgres, mysql, redis).
type WhoDBManualConnection struct {
	Type string          `json:"type"`
	Conn WhoDBConnection `json:"conn"`
}

// WhoDBAutoConnection is an auto-detected connection derived from an installed
// service's config.env. Source is the service ID (e.g. "postgres").
type WhoDBAutoConnection struct {
	Source string          `json:"source"`
	Type   string          `json:"type"`
	Conn   WhoDBConnection `json:"conn"`
}

// WhoDBInstaller downloads the WhoDB binary to {serverRoot}/whodb/,
// writes a config.env, and registers a Caddy reverse-proxy vhost at whodb.test.
type WhoDBInstaller struct {
	siteManager *sites.Manager
	supervisor  *services.Supervisor
	serverRoot  string
	siteUser    string
	queries     *dbq.Queries
	hooks       *HookRegistry
}

// SetQueries injects the DB queries handle (called from NewRegistry after creation).
func (w *WhoDBInstaller) SetQueries(q *dbq.Queries) { w.queries = q }

// SetHooks injects the hook registry so regenerateConfig can be wired as a hook.
func (w *WhoDBInstaller) SetHooks(h *HookRegistry) { w.hooks = h }

func (w *WhoDBInstaller) ServiceID() string { return "whodb" }

func (w *WhoDBInstaller) IsInstalled() bool {
	p := filepath.Join(paths.ServiceDir(w.serverRoot, "whodb"), "whodb")
	return fileExists(p)
}

func (w *WhoDBInstaller) Install(ctx context.Context) error {
	return w.InstallW(ctx, io.Discard)
}

func (w *WhoDBInstaller) InstallW(ctx context.Context, out io.Writer) error {
	if w.IsInstalled() {
		fmt.Fprintln(out, "whodb: already installed")
		return nil
	}

	latest, err := w.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("whodb: resolve latest version: %w", err)
	}
	dlURL := fmt.Sprintf("https://github.com/clidey/whodb/releases/download/%s/whodb-%s-linux-amd64", latest, latest)

	whodbDir := paths.ServiceDir(w.serverRoot, "whodb")
	binPath := filepath.Join(whodbDir, "whodb")

	// 1. Create directory.
	fmt.Fprintln(out, "whodb: creating directory...")
	if err := os.MkdirAll(whodbDir, 0755); err != nil {
		return fmt.Errorf("whodb: create dir: %w", err)
	}

	// 2. Download binary.
	fmt.Fprintf(out, "whodb: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, out, dlURL, binPath); err != nil {
		return fmt.Errorf("whodb: download: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("whodb: chmod binary: %w", err)
	}

	// 3. Symlink into the shared bin dir so whodb is in PATH.
	if err := LinkIntoBinDir(paths.BinDir(w.serverRoot), "whodb", binPath); err != nil {
		fmt.Fprintf(out, "whodb: warning: %v\n", err)
	}

	// 4. Write initial config.env (picks up any already-installed services).
	fmt.Fprintln(out, "whodb: writing config.env...")
	if err := w.regenerateConfig(ctx); err != nil {
		fmt.Fprintf(out, "whodb: warning: config.env: %v\n", err)
	}

	// 5. Register Caddy reverse-proxy vhost at whodb.test.
	fmt.Fprintln(out, "whodb: creating whodb.test Caddy vhost...")
	_, err = w.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:       "whodb.test",
		SiteType:     "ws",
		WSUpstream:   "127.0.0.1:8161",
		HTTPS:        true,
		ServiceVhost: true,
	})
	if err != nil {
		fmt.Fprintf(out, "whodb: warning: create site: %v\n", err)
	}

	// 6. Transfer ownership to the site user.
	if w.siteUser != "" {
		fmt.Fprintf(out, "whodb: chowning %s to %s...\n", whodbDir, w.siteUser)
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", w.siteUser, w.siteUser, whodbDir)
		if _, err := runShellW(ctx, out, chownCmd); err != nil {
			return fmt.Errorf("whodb: chown: %w", err)
		}
	}

	fmt.Fprintln(out, "whodb: install complete")
	return nil
}

func (w *WhoDBInstaller) Purge(ctx context.Context) error {
	return w.PurgeW(ctx, io.Discard, false)
}

func (w *WhoDBInstaller) PurgeW(ctx context.Context, out io.Writer, _ bool) error {
	// Stop the supervised process first.
	if err := w.supervisor.Stop("whodb"); err != nil {
		fmt.Fprintf(out, "whodb: warning: stop process: %v\n", err)
	}

	// Remove the Caddy vhost and DB row.
	if w.siteManager != nil {
		if err := w.siteManager.Delete(ctx, "whodb-test"); err != nil {
			fmt.Fprintf(out, "whodb: warning: delete site: %v\n", err)
		}
	}

	// Remove bin dir symlink.
	UnlinkFromBinDir(paths.BinDir(w.serverRoot), "whodb")

	// Remove the directory.
	whodbDir := paths.ServiceDir(w.serverRoot, "whodb")
	if err := os.RemoveAll(whodbDir); err != nil {
		return fmt.Errorf("whodb: remove dir: %w", err)
	}

	fmt.Fprintln(out, "whodb: purge complete")
	return nil
}

// LatestVersion queries GitHub Releases for the latest WhoDB version.
// If the context carries a pre-resolved version (via install.WithPreResolvedVersion),
// that value is returned immediately without hitting GitHub.
func (w *WhoDBInstaller) LatestVersion(ctx context.Context) (string, error) {
	if v := preResolvedVersionFromCtx(ctx); v != "" {
		return v, nil
	}
	return fetchGitHubLatestVersion(ctx, "clidey/whodb")
}

// UpdateW stops WhoDB, replaces the binary with the latest version, and returns.
// The caller (API handler) restarts the process normally via the supervisor.
func (w *WhoDBInstaller) UpdateW(ctx context.Context, out io.Writer) error {
	latest, err := w.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("whodb: update: %w", err)
	}
	dlURL := fmt.Sprintf("https://github.com/clidey/whodb/releases/download/%s/whodb-%s-linux-amd64", latest, latest)

	whodbDir := paths.ServiceDir(w.serverRoot, "whodb")
	binPath := filepath.Join(whodbDir, "whodb")

	// Stop the running process.
	fmt.Fprintln(out, "whodb: stopping...")
	if err := w.supervisor.Stop("whodb"); err != nil {
		fmt.Fprintf(out, "whodb: warning: stop: %v\n", err)
	}

	// Download the new binary to a temp location.
	fmt.Fprintf(out, "whodb: downloading %s...\n", latest)
	tmpBin := filepath.Join(os.TempDir(), "whodb-update-linux-amd64")
	defer os.Remove(tmpBin)
	if err := curlDownloadW(ctx, out, dlURL, tmpBin); err != nil {
		return fmt.Errorf("whodb: update download: %w", err)
	}
	if err := os.Chmod(tmpBin, 0755); err != nil {
		return fmt.Errorf("whodb: chmod new binary: %w", err)
	}

	// Replace the binary (rename, fall back to copy on cross-device move).
	fmt.Fprintln(out, "whodb: replacing binary...")
	if err := os.Rename(tmpBin, binPath); err != nil {
		if copyErr := copyFile(tmpBin, binPath); copyErr != nil {
			return fmt.Errorf("whodb: replace binary: rename failed (%v), copy also failed: %w", err, copyErr)
		}
		if err := os.Chmod(binPath, 0755); err != nil {
			return fmt.Errorf("whodb: chmod binary after copy: %w", err)
		}
	}

	fmt.Fprintf(out, "whodb: binary replaced with %s\n", latest)
	return nil
}

// RegenerateConfig rebuilds the WhoDB config.env from currently installed services
// plus any manual connections stored in the DB. Exported so the API can call it.
func (w *WhoDBInstaller) RegenerateConfig(ctx context.Context) error {
	return w.regenerateConfig(ctx)
}

// AutoConnections returns auto-detected WhoDB connections derived from the
// config.env of installed services (postgres, mysql, redis/valkey).
// This is exported so the API settings handler can serve them to the frontend.
func (w *WhoDBInstaller) AutoConnections() []WhoDBAutoConnection {
	pgEnv := readEnvFile(filepath.Join(paths.ServiceDir(w.serverRoot, "postgres"), "config.env"))
	myEnv := readEnvFile(filepath.Join(paths.ServiceDir(w.serverRoot, "mysql"), "config.env"))
	valkeyEnv := readEnvFile(filepath.Join(paths.ServiceDir(w.serverRoot, "valkey"), "config.env"))

	var out []WhoDBAutoConnection

	if len(pgEnv) > 0 {
		out = append(out, WhoDBAutoConnection{
			Source: "postgres",
			Type:   "postgres",
			Conn: WhoDBConnection{
				Alias:    "PostgreSQL",
				Host:     coalesce(pgEnv["DB_HOST"], "127.0.0.1"),
				Port:     coalesce(pgEnv["DB_PORT"], "5432"),
				User:     coalesce(pgEnv["DB_USERNAME"], "postgres"),
				Password: pgEnv["DB_PASSWORD"],
				Database: coalesce(pgEnv["DB_DATABASE"], "postgres"),
			},
		})
	}
	if len(myEnv) > 0 {
		out = append(out, WhoDBAutoConnection{
			Source: "mysql",
			Type:   "mysql",
			Conn: WhoDBConnection{
				Alias:    "MySQL",
				Host:     coalesce(myEnv["DB_HOST"], "127.0.0.1"),
				Port:     coalesce(myEnv["DB_PORT"], "3306"),
				User:     coalesce(myEnv["DB_USERNAME"], "root"),
				Password: myEnv["DB_PASSWORD"],
				Database: coalesce(myEnv["DB_DATABASE"], "mysql"),
			},
		})
	}
	if len(valkeyEnv) > 0 {
		out = append(out, WhoDBAutoConnection{
			Source: "redis",
			Type:   "redis",
			Conn: WhoDBConnection{
				Alias: "Valkey",
				Host:  coalesce(valkeyEnv["REDIS_HOST"], "127.0.0.1"),
				Port:  coalesce(valkeyEnv["REDIS_PORT"], "6379"),
			},
		})
	}
	if out == nil {
		return []WhoDBAutoConnection{}
	}
	return out
}

// regenerateConfig reads each installed service's config.env, builds pre-configured
// connection JSON arrays, and writes them to {serverRoot}/whodb/config.env.
// It also appends any manual connections stored in the DB.
func (w *WhoDBInstaller) regenerateConfig(ctx context.Context) error {
	if !w.IsInstalled() {
		return nil // WhoDB not installed yet — nothing to do.
	}

	whodbDir := paths.ServiceDir(w.serverRoot, "whodb")
	logPath := paths.LogPath(w.serverRoot, "whodb")

	// Read per-service config.env files into key=value maps.
	pgEnv := readEnvFile(filepath.Join(paths.ServiceDir(w.serverRoot, "postgres"), "config.env"))
	myEnv := readEnvFile(filepath.Join(paths.ServiceDir(w.serverRoot, "mysql"), "config.env"))
	valkeyEnv := readEnvFile(filepath.Join(paths.ServiceDir(w.serverRoot, "valkey"), "config.env"))

	// Assemble auto-detected connections from each service that is installed.
	var pgConns, myConns, redisConns []WhoDBConnection

	if len(pgEnv) > 0 {
		pgConns = append(pgConns, WhoDBConnection{
			Alias:    "PostgreSQL",
			Host:     coalesce(pgEnv["DB_HOST"], "127.0.0.1"),
			Port:     coalesce(pgEnv["DB_PORT"], "5432"),
			User:     coalesce(pgEnv["DB_USERNAME"], "postgres"),
			Password: pgEnv["DB_PASSWORD"],
			Database: coalesce(pgEnv["DB_DATABASE"], "postgres"),
		})
	}

	if len(myEnv) > 0 {
		myConns = append(myConns, WhoDBConnection{
			Alias:    "MySQL",
			Host:     coalesce(myEnv["DB_HOST"], "127.0.0.1"),
			Port:     coalesce(myEnv["DB_PORT"], "3306"),
			User:     coalesce(myEnv["DB_USERNAME"], "root"),
			Password: myEnv["DB_PASSWORD"],
			Database: coalesce(myEnv["DB_DATABASE"], "mysql"),
		})
	}

	if len(valkeyEnv) > 0 {
		redisConns = append(redisConns, WhoDBConnection{
			Alias:    "Valkey",
			Host:     coalesce(valkeyEnv["REDIS_HOST"], "127.0.0.1"),
			Port:     coalesce(valkeyEnv["REDIS_PORT"], "6379"),
			Password: valkeyEnv["REDIS_PASSWORD"],
		})
	}

	// Merge manual connections from DB (if queries handle is available).
	if w.queries != nil {
		manualJSON, _ := w.queries.GetSetting(ctx, "whodb_manual_connections")
		if manualJSON != "" {
			var manual []WhoDBManualConnection
			if err := json.Unmarshal([]byte(manualJSON), &manual); err == nil {
				for _, m := range manual {
					switch strings.ToLower(m.Type) {
					case "postgres", "postgresql":
						pgConns = append(pgConns, m.Conn)
					case "mysql":
						myConns = append(myConns, m.Conn)
					case "redis":
						redisConns = append(redisConns, m.Conn)
					}
				}
			}
		}
	}

	// Read disable_credential_form setting — default true when never explicitly set.
	disableForm := true
	if w.queries != nil {
		val, _ := w.queries.GetSetting(ctx, "whodb_disable_credential_form")
		if val == "false" {
			disableForm = false
		}
	}

	// Build env file content.
	var sb strings.Builder
	sb.WriteString("PORT=8161\n")
	sb.WriteString(fmt.Sprintf("WHODB_LOG_FILE=%s\n", logPath))
	sb.WriteString("WHODB_DISABLE_UPDATE_CHECK=true\n")
	sb.WriteString("WHODB_DISABLE_MOCK_DATA_GENERATION=*\n")
	if disableForm {
		sb.WriteString("WHODB_DISABLE_CREDENTIAL_FORM=true\n")
	}

	if len(pgConns) > 0 {
		b, _ := json.Marshal(pgConns)
		sb.WriteString(fmt.Sprintf("WHODB_POSTGRESQL=%s\n", b))
	}
	if len(myConns) > 0 {
		b, _ := json.Marshal(myConns)
		sb.WriteString(fmt.Sprintf("WHODB_MYSQL=%s\n", b))
	}
	if len(redisConns) > 0 {
		b, _ := json.Marshal(redisConns)
		sb.WriteString(fmt.Sprintf("WHODB_REDIS=%s\n", b))
	}

	envPath := filepath.Join(whodbDir, "config.env")
	if err := os.WriteFile(envPath, []byte(sb.String()), 0600); err != nil {
		return fmt.Errorf("whodb: write config.env: %w", err)
	}
	return nil
}

// readEnvFile parses a KEY=VALUE env file into a map. Returns empty map on error.
func readEnvFile(path string) map[string]string {
	m := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			m[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+1:])
		}
	}
	return m
}

// coalesce returns the first non-empty string from the arguments.
func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
