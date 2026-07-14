package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

// clickhouseCLINames are multi-call symlink names for the single clickhouse binary.
// The fat binary dispatches based on argv[0] / subcommand.
var clickhouseCLINames = []string{
	"clickhouse",
	"clickhouse-client",
	"clickhouse-server",
	"clickhouse-local",
	"clickhouse-benchmark",
	"clickhouse-format",
	"clickhouse-compressor",
	"clickhouse-obfuscator",
}

// ClickHouseInstaller downloads the official clickhouse-common-static tarball
// (single multi-call binary) into {serverRoot}/clickhouse/, writes a minimal
// config, and symlinks the CLI tools into the shared bin dir.
//
// No APT packages, Docker, or system services are used.
type ClickHouseInstaller struct {
	supervisor *services.Supervisor
	serverRoot string
	siteUser   string
}

func (c *ClickHouseInstaller) ServiceID() string { return "clickhouse" }

func (c *ClickHouseInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(c.serverRoot, "clickhouse"), "clickhouse"))
}

func (c *ClickHouseInstaller) Install(ctx context.Context) error {
	return c.InstallW(ctx, io.Discard)
}

func (c *ClickHouseInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if c.IsInstalled() {
		fmt.Fprintln(w, "clickhouse: already installed")
		return nil
	}

	latest, err := c.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("clickhouse: resolve latest version: %w", err)
	}
	dlURL := clickhouseTarURL(latest)

	chDir := paths.ServiceDir(c.serverRoot, "clickhouse")
	binPath := filepath.Join(chDir, "clickhouse")
	// Version-independent basename so the test curl shim can serve a cached
	// artifact regardless of which latest version GitHub reports.
	tmpTar := filepath.Join(os.TempDir(), "clickhouse-common-static-amd64.tgz")
	defer os.Remove(tmpTar)

	// 1. Create directories (data layout under the service dir).
	fmt.Fprintln(w, "clickhouse: creating directories...")
	for _, d := range []string{
		chDir,
		filepath.Join(chDir, "data"),
		filepath.Join(chDir, "tmp"),
		filepath.Join(chDir, "user_files"),
		filepath.Join(chDir, "access"),
		filepath.Join(chDir, "format_schemas"),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("clickhouse: create dir %s: %w", d, err)
		}
	}

	// 2. Download the common-static tarball (contains the multi-call binary).
	fmt.Fprintf(w, "clickhouse: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, tmpTar); err != nil {
		return fmt.Errorf("clickhouse: download: %w", err)
	}

	// 3. Extract the clickhouse binary.
	fmt.Fprintln(w, "clickhouse: extracting binary...")
	if err := extractFromTar(tmpTar, "clickhouse", binPath); err != nil {
		return fmt.Errorf("clickhouse: extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("clickhouse: chmod binary: %w", err)
	}

	// 4. Symlink CLI multi-call names into the service dir and shared bin dir.
	fmt.Fprintln(w, "clickhouse: linking CLI tools...")
	binDir := paths.BinDir(c.serverRoot)
	for _, name := range clickhouseCLINames {
		// Local multi-call names next to the binary (except the binary itself).
		if name != "clickhouse" {
			localLink := filepath.Join(chDir, name)
			_ = os.Remove(localLink)
			if err := os.Symlink(binPath, localLink); err != nil {
				fmt.Fprintf(w, "clickhouse: warning: local symlink %s: %v\n", name, err)
			}
		}
		if err := LinkIntoBinDir(binDir, name, binPath); err != nil {
			fmt.Fprintf(w, "clickhouse: warning: %v\n", err)
		}
	}

	// 5. Write config.xml and users.xml (only if missing — preserve user edits).
	fmt.Fprintln(w, "clickhouse: writing config...")
	if err := writeClickHouseConfig(chDir); err != nil {
		return fmt.Errorf("clickhouse: write config: %w", err)
	}
	if err := writeClickHouseUsers(chDir); err != nil {
		return fmt.Errorf("clickhouse: write users: %w", err)
	}

	// 6. Runtime env for the supervisor (watchdog off so we stay single-process).
	fmt.Fprintln(w, "clickhouse: writing clickhouse.env...")
	if err := writeClickHouseRuntimeEnv(chDir); err != nil {
		return fmt.Errorf("clickhouse: write clickhouse.env: %w", err)
	}

	// 7. Write credentials for the Services panel / .env paste.
	fmt.Fprintln(w, "clickhouse: writing config.env...")
	envContent := "CLICKHOUSE_HOST=127.0.0.1\n" +
		"CLICKHOUSE_PORT=8123\n" +
		"CLICKHOUSE_TCP_PORT=9000\n" +
		"CLICKHOUSE_USER=default\n" +
		"CLICKHOUSE_PASSWORD=\n" +
		"CLICKHOUSE_DATABASE=default\n"
	if err := os.WriteFile(filepath.Join(chDir, "config.env"), []byte(envContent), 0600); err != nil {
		return fmt.Errorf("clickhouse: write config.env: %w", err)
	}

	// 7. Transfer ownership to the site user.
	if c.siteUser != "" {
		fmt.Fprintf(w, "clickhouse: chowning %s to %s...\n", chDir, c.siteUser)
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", c.siteUser, c.siteUser, chDir)
		if out, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("clickhouse: chown: %w\n%s", err, out)
		}
	}

	fmt.Fprintln(w, "clickhouse: install complete")
	return nil
}

// LatestVersion queries GitHub Releases for the latest ClickHouse version and
// returns a bare version string suitable for download URLs (e.g. "25.8.28.1").
// Tags look like "v25.8.28.1-lts" or "v26.5.5.8-stable".
func (c *ClickHouseInstaller) LatestVersion(ctx context.Context) (string, error) {
	if v := preResolvedVersionFromCtx(ctx); v != "" {
		return normalizeClickHouseVersion(v), nil
	}
	tag, err := fetchGitHubLatestVersion(ctx, "ClickHouse/ClickHouse")
	if err != nil {
		return "", err
	}
	return normalizeClickHouseVersion(tag), nil
}

// normalizeClickHouseVersion strips a leading "v" and trailing "-stable"/"-lts"
// (and any other suffix after the numeric version).
func normalizeClickHouseVersion(tag string) string {
	v := strings.TrimPrefix(tag, "v")
	// Keep only the numeric dotted version (first token before any letter suffix).
	// e.g. "25.8.28.1-lts" → "25.8.28.1", "26.5.5.8-stable" → "26.5.5.8"
	for i, r := range v {
		if (r < '0' || r > '9') && r != '.' {
			return v[:i]
		}
	}
	return v
}

// clickhouseTarURL returns the packages.clickhouse.com tarball URL for the
// given bare version (e.g. "25.8.28.1").
func clickhouseTarURL(version string) string {
	return fmt.Sprintf(
		"https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-%s-amd64.tgz",
		version,
	)
}

// UpdateW stops ClickHouse, replaces the binary with the latest version.
// The caller (API handler) is responsible for restarting the service.
func (c *ClickHouseInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := c.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("clickhouse: update: %w", err)
	}
	dlURL := clickhouseTarURL(latest)

	chDir := paths.ServiceDir(c.serverRoot, "clickhouse")
	binPath := filepath.Join(chDir, "clickhouse")
	// Version-independent basename for the test curl shim (see InstallW).
	tmpTar := filepath.Join(os.TempDir(), "clickhouse-common-static-update.tgz")
	defer os.Remove(tmpTar)

	fmt.Fprintf(w, "clickhouse: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, tmpTar); err != nil {
		return fmt.Errorf("clickhouse: update download: %w", err)
	}

	fmt.Fprintln(w, "clickhouse: stopping clickhouse...")
	if err := c.supervisor.Stop("clickhouse"); err != nil {
		fmt.Fprintf(w, "clickhouse: warning: stop: %v\n", err)
	}

	fmt.Fprintln(w, "clickhouse: replacing binary...")
	if err := extractFromTar(tmpTar, "clickhouse", binPath); err != nil {
		return fmt.Errorf("clickhouse: update extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("clickhouse: update chmod: %w", err)
	}

	// Refresh CLI multi-call links in the shared bin dir.
	binDir := paths.BinDir(c.serverRoot)
	for _, name := range clickhouseCLINames {
		if err := LinkIntoBinDir(binDir, name, binPath); err != nil {
			fmt.Fprintf(w, "clickhouse: warning: %v\n", err)
		}
	}

	fmt.Fprintf(w, "clickhouse: binary replaced with %s\n", latest)
	return nil
}

func (c *ClickHouseInstaller) Purge(ctx context.Context) error {
	return c.PurgeW(ctx, io.Discard, false)
}

func (c *ClickHouseInstaller) PurgeW(ctx context.Context, w io.Writer, preserveData bool) error {
	if err := c.supervisor.Stop("clickhouse"); err != nil {
		fmt.Fprintf(w, "clickhouse: warning: stop process: %v\n", err)
	}

	binDir := paths.BinDir(c.serverRoot)
	for _, name := range clickhouseCLINames {
		UnlinkFromBinDir(binDir, name)
	}

	chDir := paths.ServiceDir(c.serverRoot, "clickhouse")
	if preserveData {
		// Keep data/ and config; remove only the binary and local CLI links.
		_ = os.Remove(filepath.Join(chDir, "clickhouse"))
		for _, name := range clickhouseCLINames {
			if name == "clickhouse" {
				continue
			}
			_ = os.Remove(filepath.Join(chDir, name))
		}
		fmt.Fprintln(w, "clickhouse: purged binary (data preserved)")
		return nil
	}

	if err := os.RemoveAll(chDir); err != nil {
		return fmt.Errorf("clickhouse: remove dir: %w", err)
	}

	fmt.Fprintln(w, "clickhouse: purge complete")
	return nil
}

// EnsureClickHouseConfig writes config.xml / users.xml / clickhouse.env if missing.
// Safe to call on every startup — no-op when files already exist.
func EnsureClickHouseConfig(serverRoot string) error {
	chDir := paths.ServiceDir(serverRoot, "clickhouse")
	if !fileExists(filepath.Join(chDir, "clickhouse")) {
		return nil // not installed
	}
	if err := writeClickHouseConfig(chDir); err != nil {
		return err
	}
	if err := writeClickHouseUsers(chDir); err != nil {
		return err
	}
	return writeClickHouseRuntimeEnv(chDir)
}

// writeClickHouseRuntimeEnv writes clickhouse.env used by ManagedEnvFile.
// Only written if missing so user edits are preserved.
func writeClickHouseRuntimeEnv(dir string) error {
	path := filepath.Join(dir, "clickhouse.env")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	// Disable the watchdog fork so the supervisor tracks a single process
	// (otherwise ClickHouse double-forks and the supervised parent can exit).
	content := "CLICKHOUSE_WATCHDOG_ENABLE=0\n"
	return os.WriteFile(path, []byte(content), 0644)
}

// writeClickHouseConfig writes a minimal config.xml for local development.
// Only written if the file does not already exist so user edits are preserved.
//
// HTTP 8123 and native TCP 9000 — ClickHouse upstream defaults.
func writeClickHouseConfig(dir string) error {
	confPath := filepath.Join(dir, "config.xml")
	if _, err := os.Stat(confPath); err == nil {
		return nil
	}
	conf := `<?xml version="1.0"?>
<!-- devctl-managed ClickHouse server config. Edit freely — restarts pick it up. -->
<clickhouse>
    <logger>
        <level>information</level>
        <console>1</console>
    </logger>

    <!-- HTTP interface (primary for app drivers) -->
    <http_port>8123</http_port>
    <!-- Native TCP (clickhouse-client) -->
    <tcp_port>9000</tcp_port>

    <listen_host>127.0.0.1</listen_host>

    <!-- All paths relative to ManagedDir ({serverRoot}/clickhouse) -->
    <path>./data/</path>
    <tmp_path>./tmp/</tmp_path>
    <user_files_path>./user_files/</user_files_path>
    <format_schema_path>./format_schemas/</format_schema_path>
    <access_control_path>./access/</access_control_path>

    <user_directories>
        <users_xml>
            <path>./users.xml</path>
        </users_xml>
        <local_directory>
            <path>./access/</path>
        </local_directory>
    </user_directories>

    <!-- Avoid CAP_SYS_NICE requirement on unprivileged installs -->
    <mlock_executable>false</mlock_executable>
    <mark_cache_size>536870912</mark_cache_size>
</clickhouse>
`
	return os.WriteFile(confPath, []byte(conf), 0644)
}

// writeClickHouseUsers writes a minimal users.xml with an open default user
// (empty password, localhost-only access via listen_host). Only written if
// missing so user edits are preserved.
func writeClickHouseUsers(dir string) error {
	usersPath := filepath.Join(dir, "users.xml")
	if _, err := os.Stat(usersPath); err == nil {
		return nil
	}
	users := `<?xml version="1.0"?>
<!-- devctl-managed ClickHouse users. Edit freely — restarts pick it up. -->
<clickhouse>
    <profiles>
        <default/>
        <readonly>
            <readonly>1</readonly>
        </readonly>
    </profiles>

    <users>
        <default>
            <password></password>
            <networks>
                <ip>::/0</ip>
            </networks>
            <profile>default</profile>
            <quota>default</quota>
            <access_management>1</access_management>
        </default>
    </users>

    <quotas>
        <default>
            <interval>
                <duration>3600</duration>
                <queries>0</queries>
                <errors>0</errors>
                <result_rows>0</result_rows>
                <read_rows>0</read_rows>
                <execution_time>0</execution_time>
            </interval>
        </default>
    </quotas>
</clickhouse>
`
	return os.WriteFile(usersPath, []byte(users), 0644)
}
