package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgormly/devctl/internal/runuser"
	"github.com/danielgormly/devctl/paths"
)

// ExtensionStatus is the API/CLI/UI view of one managed Postgres extension.
type ExtensionStatus struct {
	ID                string `json:"id"`
	Label             string `json:"label"`
	FilesInstalled    bool   `json:"files_installed"`
	PreloadConfigured bool   `json:"preload_configured"`
	Wired             bool   `json:"wired"`
	Ready             bool   `json:"ready"`
	Version           string `json:"version,omitempty"`
	Note              string `json:"note,omitempty"`
	RequiresPeer      string `json:"requires_peer,omitempty"`
}

// ExtensionEnv is the runtime context for managed extension operations.
type ExtensionEnv struct {
	ServerRoot string
	SiteUser   string
	SiteHome   string
}

func (e ExtensionEnv) PGDir() string {
	return paths.ServiceDir(e.ServerRoot, "postgres")
}

func (e ExtensionEnv) PostgresInstalled() bool {
	return fileExists(filepath.Join(e.PGDir(), "bin", "postgres"))
}

func (e ExtensionEnv) ClickHouseInstalled() bool {
	return isClickHouseBinaryOK(filepath.Join(paths.ServiceDir(e.ServerRoot, "clickhouse"), "clickhouse"))
}

func (e ExtensionEnv) PeerInstalled(id string) bool {
	switch id {
	case "":
		return true
	case "clickhouse":
		return e.ClickHouseInstalled()
	default:
		return false
	}
}

// PostgresExtension is a managed extension installed into the Percona tree.
type PostgresExtension interface {
	ID() string
	Label() string
	// PreloadLibrary is the shared_preload_libraries entry, or "" if unused.
	PreloadLibrary() string
	// RequiresPeer is a peer service id that must be installed for full wire/ready, or "".
	RequiresPeer() string

	IsFilesInstalled(pgDir string) bool
	InstallFiles(ctx context.Context, w io.Writer, pgDir string) error
	UpdateFiles(ctx context.Context, w io.Writer, pgDir string) error

	// Wire creates SQL objects when the server is up. ready=false means retry later.
	Wire(ctx context.Context, env ExtensionEnv) (ready bool, err error)
	// IsWired reports whether SQL objects for this extension are present where expected.
	IsWired(env ExtensionEnv) bool
	// FilesVersion returns a version string from control/files when known.
	FilesVersion(pgDir string) string
}

// managedPostgresExtensions is the ordered registry.
func managedPostgresExtensions() []PostgresExtension {
	return []PostgresExtension{
		timescaleExtension{},
		pgvectorExtension{},
		pgClickhouseExtension{},
	}
}

// ListPostgresExtensions returns status for every managed extension.
func ListPostgresExtensions(serverRoot, siteUser string) []ExtensionStatus {
	env := ExtensionEnv{
		ServerRoot: serverRoot,
		SiteUser:   siteUser,
		SiteHome:   lookupHome(siteUser),
	}
	out := make([]ExtensionStatus, 0, len(managedPostgresExtensions()))
	for _, ext := range managedPostgresExtensions() {
		out = append(out, statusFor(ext, env))
	}
	return out
}

func statusFor(ext PostgresExtension, env ExtensionEnv) ExtensionStatus {
	pgDir := env.PGDir()
	st := ExtensionStatus{
		ID:           ext.ID(),
		Label:        ext.Label(),
		RequiresPeer: ext.RequiresPeer(),
	}
	if !env.PostgresInstalled() {
		st.Note = "PostgreSQL is not installed"
		return st
	}
	st.FilesInstalled = ext.IsFilesInstalled(pgDir)
	st.Version = ext.FilesVersion(pgDir)
	if lib := ext.PreloadLibrary(); lib != "" {
		st.PreloadConfigured = isPreloadConfigured(pgDir, lib)
	} else {
		st.PreloadConfigured = true // N/A
	}
	peerOK := env.PeerInstalled(ext.RequiresPeer())
	if st.FilesInstalled && peerOK {
		st.Wired = ext.IsWired(env)
	}
	st.Ready = st.FilesInstalled && st.PreloadConfigured && peerOK && st.Wired
	switch {
	case !st.FilesInstalled:
		st.Note = "extension files not installed"
	case ext.RequiresPeer() != "" && !peerOK:
		st.Note = fmt.Sprintf("waiting for %s", ext.RequiresPeer())
	case !st.PreloadConfigured:
		st.Note = "shared_preload_libraries not configured — restart PostgreSQL after ensure"
	case !st.Wired:
		st.Note = "SQL objects not wired yet (start PostgreSQL or run postgres:extensions:ensure)"
	default:
		st.Note = "ready"
	}
	return st
}

// InstallManagedPostgresExtensions installs files + preloads for all managed extensions.
// Does not require the server to be running. Wiring is separate.
func InstallManagedPostgresExtensions(ctx context.Context, w io.Writer, env ExtensionEnv) error {
	if !env.PostgresInstalled() {
		return nil
	}
	pgDir := env.PGDir()
	for _, ext := range managedPostgresExtensions() {
		fmt.Fprintf(w, "postgres: ensuring extension %s...\n", ext.ID())
		if err := ext.InstallFiles(ctx, w, pgDir); err != nil {
			return err
		}
		if lib := ext.PreloadLibrary(); lib != "" {
			if err := ensureSharedPreloadLibraries(filepath.Join(pgDir, "data", "postgresql.conf"), lib); err != nil {
				return fmt.Errorf("postgres: preload %s: %w", lib, err)
			}
		}
	}
	return nil
}

// UpdateManagedPostgresExtensions re-installs/updates all extension files (used on PG update).
func UpdateManagedPostgresExtensions(ctx context.Context, w io.Writer, env ExtensionEnv) error {
	if !env.PostgresInstalled() {
		return nil
	}
	pgDir := env.PGDir()
	for _, ext := range managedPostgresExtensions() {
		fmt.Fprintf(w, "postgres: updating extension %s...\n", ext.ID())
		if err := ext.UpdateFiles(ctx, w, pgDir); err != nil {
			return err
		}
		if lib := ext.PreloadLibrary(); lib != "" {
			if err := ensureSharedPreloadLibraries(filepath.Join(pgDir, "data", "postgresql.conf"), lib); err != nil {
				return fmt.Errorf("postgres: preload %s: %w", lib, err)
			}
		}
	}
	return nil
}

// EnsureManagedPostgresExtensions is the full idempotent ensure: files, preloads, wire.
// Safe on startup and after either service install. Wire is best-effort if server is down.
func EnsureManagedPostgresExtensions(ctx context.Context, w io.Writer, env ExtensionEnv) error {
	if env.SiteHome == "" && env.SiteUser != "" {
		env.SiteHome = lookupHome(env.SiteUser)
	}
	if !env.PostgresInstalled() {
		return nil
	}
	if err := InstallManagedPostgresExtensions(ctx, w, env); err != nil {
		return err
	}
	// Wire when possible; ignore "not ready" for startup.
	_, err := WireManagedPostgresExtensions(ctx, env)
	return err
}

// WireManagedPostgresExtensions runs Wire for each extension whose peer is present.
// Returns ready=false if any extension still needs the server to come up.
func WireManagedPostgresExtensions(ctx context.Context, env ExtensionEnv) (ready bool, err error) {
	if env.SiteHome == "" && env.SiteUser != "" {
		env.SiteHome = lookupHome(env.SiteUser)
	}
	if !env.PostgresInstalled() {
		return true, nil
	}
	allReady := true
	for _, ext := range managedPostgresExtensions() {
		if !ext.IsFilesInstalled(env.PGDir()) {
			continue
		}
		if !env.PeerInstalled(ext.RequiresPeer()) {
			continue
		}
		ok, werr := ext.Wire(ctx, env)
		if werr != nil {
			return false, werr
		}
		if !ok {
			allReady = false
		}
	}
	return allReady, nil
}

// EnsurePostgresExtensionsAfterStart polls until all wireable extensions are wired or timeout.
func EnsurePostgresExtensionsAfterStart(serverRoot, siteUser string) error {
	env := ExtensionEnv{
		ServerRoot: serverRoot,
		SiteUser:   siteUser,
		SiteHome:   lookupHome(siteUser),
	}
	if !env.PostgresInstalled() {
		return nil
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		ready, err := WireManagedPostgresExtensions(context.Background(), env)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("postgres: timed out waiting to wire extensions")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// EnsureClickHousePostgresBridge is called after ClickHouse install when Postgres may already exist.
func EnsureClickHousePostgresBridge(ctx context.Context, w io.Writer, serverRoot, siteUser string) error {
	env := ExtensionEnv{
		ServerRoot: serverRoot,
		SiteUser:   siteUser,
		SiteHome:   lookupHome(siteUser),
	}
	if !env.PostgresInstalled() {
		fmt.Fprintln(w, "clickhouse: postgres not installed — skip pg_clickhouse bridge")
		return nil
	}
	// Only need pg_clickhouse files + wire; still run full ensure for consistency.
	if err := EnsureManagedPostgresExtensions(ctx, w, env); err != nil {
		return err
	}
	// If postgres is up, wait briefly for wire.
	if err := EnsurePostgresExtensionsAfterStart(serverRoot, siteUser); err != nil {
		// Non-fatal if server isn't running yet — next postgres start will wire.
		fmt.Fprintf(w, "clickhouse: pg bridge wire deferred: %v\n", err)
	}
	return nil
}

// runPSQL executes SQL against a database via TCP as the superuser.
func runPSQL(ctx context.Context, env ExtensionEnv, database, sql string) error {
	pgDir := env.PGDir()
	cmd := fmt.Sprintf(
		`PGPASSWORD=%q LD_LIBRARY_PATH=%q/lib %q/bin/psql.bin -h 127.0.0.1 -p 5432 -U %q -d %q -v ON_ERROR_STOP=1 -c %q`,
		postgresDevPassword, pgDir, pgDir, postgresSuperuser, database, sql,
	)
	if out, err := runuser.RunAsUserW(ctx, io.Discard, env.SiteUser, env.SiteHome, "", cmd); err != nil {
		return fmt.Errorf("psql %s: %w\n%s", database, err, out)
	}
	return nil
}

// runPSQLQuery runs SQL and returns trimmed stdout (single-line results).
func runPSQLQuery(ctx context.Context, env ExtensionEnv, database, sql string) (string, error) {
	pgDir := env.PGDir()
	cmd := fmt.Sprintf(
		`PGPASSWORD=%q LD_LIBRARY_PATH=%q/lib %q/bin/psql.bin -h 127.0.0.1 -p 5432 -U %q -d %q -v ON_ERROR_STOP=1 -t -A -c %q`,
		postgresDevPassword, pgDir, pgDir, postgresSuperuser, database, sql,
	)
	out, err := runuser.RunAsUserW(ctx, io.Discard, env.SiteUser, env.SiteHome, "", cmd)
	if err != nil {
		return "", fmt.Errorf("psql %s: %w\n%s", database, err, out)
	}
	return strings.TrimSpace(out), nil
}

func pgIsReady(pgDir string) bool {
	readyCmd := fmt.Sprintf("LD_LIBRARY_PATH=%s/lib %s/bin/pg_isready -h 127.0.0.1 -p 5432 -q", pgDir, pgDir)
	_, err := runShell(context.Background(), readyCmd)
	return err == nil
}

func isPreloadConfigured(pgDir, name string) bool {
	confPath := filepath.Join(pgDir, "data", "postgresql.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		return false
	}
	_, changed := mergeSharedPreloadLibraries(string(data), name)
	// If merge would change conf, name is not present.
	return !changed
}

// parseControlDefaultVersion reads default_version from an extension .control file.
func parseControlDefaultVersion(controlPath string) string {
	data, err := os.ReadFile(controlPath)
	if err != nil {
		return ""
	}
	for _, line := range splitConfLines(string(data)) {
		key, val, ok := splitConfAssignment(line)
		if ok && key == "default_version" {
			return val
		}
	}
	return ""
}

func splitConfLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s) {
		out = append(out, s[start:])
	}
	return out
}
