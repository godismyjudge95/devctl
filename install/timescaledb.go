package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/internal/runuser"
)

// TimescaleDB Community Edition (with the TSL library: compression,
// continuous aggregates) comes from the devctl pg-extensions bundle.
const timescaledbVersion = "2.28.3"

// isTimescaleInstalled reports whether the Community Edition files are present
// (loader + versioned core library + TSL library + control file).
func isTimescaleInstalled(pgDir string) bool {
	lib := pgLibDir(pgDir)
	ext := pgShareExtDir(pgDir)
	sh := pgShlibExt()
	return fileExists(filepath.Join(lib, "timescaledb"+sh)) &&
		fileExists(filepath.Join(ext, "timescaledb.control")) &&
		fileExists(filepath.Join(lib, "timescaledb-"+timescaledbVersion+sh)) &&
		fileExists(filepath.Join(lib, "timescaledb-tsl-"+timescaledbVersion+sh))
}

// installTimescale installs the bundle when TimescaleDB is missing, then
// enables shared_preload_libraries.
func installTimescale(ctx context.Context, w io.Writer, pgDir string) error {
	if isTimescaleInstalled(pgDir) {
		fmt.Fprintln(w, "postgres: TimescaleDB already installed")
	} else if err := installPgExtensionsBundle(ctx, w, pgDir); err != nil {
		return err
	}
	return ensureTimescalePreload(pgDir)
}

// updateTimescale reinstalls the bundle unless it is current.
func updateTimescale(ctx context.Context, w io.Writer, pgDir string) error {
	if err := installPgExtensionsBundle(ctx, w, pgDir); err != nil {
		return err
	}
	return ensureTimescalePreload(pgDir)
}

// ensureTimescalePreload sets shared_preload_libraries to include timescaledb
// in data/postgresql.conf (creates/merges the setting without duplicating).
func ensureTimescalePreload(pgDir string) error {
	confPath := filepath.Join(pgDir, "data", "postgresql.conf")
	if !fileExists(confPath) {
		return nil // initdb not done yet — caller writes conf after
	}
	return ensureSharedPreloadLibraries(confPath, "timescaledb")
}

// ensureSharedPreloadLibraries ensures name appears in shared_preload_libraries.
// If the setting is missing, appends it. If present, merges name into the list.
func ensureSharedPreloadLibraries(confPath, name string) error {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return fmt.Errorf("read postgresql.conf: %w", err)
	}

	newData, changed := mergeSharedPreloadLibraries(string(data), name)
	if !changed {
		return nil
	}
	return os.WriteFile(confPath, []byte(newData), 0600)
}

// mergeSharedPreloadLibraries returns the conf text with name included in
// shared_preload_libraries. changed is false when no write is needed.
func mergeSharedPreloadLibraries(conf, name string) (string, bool) {
	lines := strings.Split(conf, "\n")
	found := false
	changed := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Match active shared_preload_libraries = ...
		key, val, ok := splitConfAssignment(trimmed)
		if !ok || key != "shared_preload_libraries" {
			continue
		}
		found = true
		libs := parsePreloadList(val)
		if containsFold(libs, name) {
			return conf, false
		}
		libs = append(libs, name)
		// Preserve original indentation of the line.
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = indent + "shared_preload_libraries = '" + strings.Join(libs, ",") + "'"
		changed = true
		break
	}

	if !found {
		// Append under a small marker block.
		extra := "\n# devctl: TimescaleDB (Community Edition)\nshared_preload_libraries = '" + name + "'\n"
		if !strings.HasSuffix(conf, "\n") {
			extra = "\n" + extra
		}
		return conf + extra, true
	}
	if !changed {
		return conf, false
	}
	return strings.Join(lines, "\n"), true
}

// splitConfAssignment parses `key = value` (value may be quoted).
func splitConfAssignment(line string) (key, val string, ok bool) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	val = strings.TrimSpace(line[eq+1:])
	// Strip trailing comment.
	if i := strings.IndexByte(val, '#'); i >= 0 {
		val = strings.TrimSpace(val[:i])
	}
	val = strings.Trim(val, "'\"")
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

func parsePreloadList(val string) []string {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "'\"")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsFold(list []string, name string) bool {
	for _, s := range list {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}

// ensureTimescaleExtension creates the timescaledb extension in the default
// postgres database when the server is up and the files are installed.
// Returns (false, nil) when the server is not ready yet (caller may retry).
// Safe to call repeatedly. No-op when files are missing.
func ensureTimescaleExtension(pgDir, siteUser, siteHome string) (ready bool, err error) {
	if !isTimescaleInstalled(pgDir) {
		return true, nil
	}
	dataDir := filepath.Join(pgDir, "data")
	if !fileExists(filepath.Join(dataDir, "PG_VERSION")) {
		return true, nil
	}

	// Socket lives in /tmp by default for the Percona tarball — use TCP + password.
	readyCmd := fmt.Sprintf("%s %s/bin/pg_isready -h 127.0.0.1 -p 5432 -q", postgresLibEnvAssign(pgDir), pgDir)
	if _, err := runShell(context.Background(), readyCmd); err != nil {
		return false, nil // server not up — caller may retry
	}

	sql := `CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;`
	cmd := fmt.Sprintf(
		`PGPASSWORD=%q %s %q -h 127.0.0.1 -p 5432 -U %q -d postgres -v ON_ERROR_STOP=1 -c %q`,
		postgresDevPassword, postgresLibEnvAssign(pgDir), postgresPSQLBin(pgDir), postgresSuperuser, sql,
	)
	if out, err := runuser.RunAsUserW(context.Background(), io.Discard, siteUser, siteHome, "", cmd); err != nil {
		return true, fmt.Errorf("postgres: CREATE EXTENSION timescaledb: %w\n%s", err, out)
	}
	return true, nil
}
