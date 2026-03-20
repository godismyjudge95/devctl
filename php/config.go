package php

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/paths"
)

//go:embed prepend.php
var prependPHP []byte

// InstallPrepend writes the embedded prepend.php to paths.PrependPath(serverRoot).
// Safe to call on every startup — it is idempotent.
func InstallPrepend(serverRoot string) error {
	p := paths.PrependPath(serverRoot)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("create %s dir: %w", filepath.Dir(p), err)
	}
	return os.WriteFile(p, prependPHP, 0644)
}

// GlobalSettings are the php.ini keys we expose for editing.
type GlobalSettings struct {
	UploadMaxFilesize string `json:"upload_max_filesize"`
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  string `json:"max_execution_time"`
	PostMaxSize       string `json:"post_max_size"`
}

// GetSettings reads the global settings from the first installed FPM php.ini.
func GetSettings(ver, serverRoot string) (GlobalSettings, error) {
	iniPath := fpmIniPath(ver, serverRoot)
	return readIni(iniPath)
}

// ApplySettings writes the given settings to all installed PHP-FPM versions.
// Callers should restart PHP-FPM processes via the supervisor to apply changes.
// This is a best-effort operation — errors are collected but all versions are attempted.
func ApplySettings(ctx context.Context, s GlobalSettings, serverRoot string) []error {
	versions, err := InstalledVersions(serverRoot)
	if err != nil {
		return []error{fmt.Errorf("list versions: %w", err)}
	}

	var errs []error
	for _, v := range versions {
		if err := writeIni(fpmIniPath(v.Version, serverRoot), s); err != nil {
			errs = append(errs, fmt.Errorf("write ini for %s: %w", v.Version, err))
		}
	}
	return errs
}

// ConfigurePrepend writes auto_prepend_file into the FPM php.ini for the
// given version. The FPM process must be restarted by the caller (via the
// supervisor) to pick up the new setting.
func ConfigurePrepend(ctx context.Context, ver, serverRoot string) error {
	path := fpmIniPath(ver, serverRoot)
	input, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	const key = "auto_prepend_file"
	line := key + " = " + paths.PrependPath(serverRoot)
	lines := strings.Split(string(input), "\n")
	found := false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		// Match both commented and uncommented variants.
		if strings.HasPrefix(strings.TrimLeft(trimmed, ";"), key) {
			lines[i] = line
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, line)
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// WriteConfigs generates php-fpm.conf and php.ini for the given version.
// Called during Install and on every devctl startup to keep config current.
// serverRoot is the absolute path to the devctl server directory.
// siteUser is the non-root OS user who owns the sites directory (e.g. "daniel").
// PHP-FPM worker processes run as this user so they can write to site storage dirs.
// SPXDataDir returns the path where SPX writes profiling data for a given PHP version.
func SPXDataDir(ver, serverRoot string) string {
	return filepath.Join(PHPDir(ver, serverRoot), "spx-data")
}

func WriteConfigs(ver, serverRoot, siteUser string) error {
	socketPath := FPMSocket(ver, serverRoot)
	iniPath := fpmIniPath(ver, serverRoot)
	fpmConfPath := FPMConfigPath(ver, serverRoot)
	prependPath := paths.PrependPath(serverRoot)
	spxDataDir := SPXDataDir(ver, serverRoot)

	// Ensure spx-data directory exists and is writable by siteUser workers.
	if err := os.MkdirAll(spxDataDir, 0755); err != nil {
		return fmt.Errorf("create spx-data dir: %w", err)
	}
	// chown spx-data to siteUser so FPM workers (running as siteUser) can write profiles.
	if u, err := user.Lookup(siteUser); err == nil {
		var uid, gid int
		fmt.Sscan(u.Uid, &uid)
		fmt.Sscan(u.Gid, &gid)
		_ = os.Chown(spxDataDir, uid, gid)
	}

	// Write php.ini with sensible defaults.
	// auto_prepend_file is set here for CLI usage; FPM workers get it via the
	// pool config php_value which takes precedence for FPM requests.
	// SPX settings are loaded globally — zero overhead until activated per-request
	// via cookies (SPX_ENABLED=1; SPX_KEY=dev) or query params.
	ini := fmt.Sprintf(`; devctl-managed php.ini for PHP %s
upload_max_filesize = 128M
memory_limit = 256M
max_execution_time = 120
post_max_size = 128M
auto_prepend_file = %s

; SPX profiler — zero overhead when not activated per-request
spx.http_enabled = 1
spx.http_key = dev
spx.http_ip_whitelist = 127.0.0.1
spx.data_dir = %s
`, ver, prependPath, spxDataDir)
	if err := os.WriteFile(iniPath, []byte(ini), 0644); err != nil {
		return fmt.Errorf("write php.ini: %w", err)
	}

	// Write php-fpm.conf.
	// FPM is launched with -c php.ini so all php.ini settings apply to workers.
	// php_value[auto_prepend_file] is set at the pool level as an authoritative
	// override so the dump interceptor is always active for FPM requests.
	fpmGlobalLog := paths.LogPath(serverRoot, "php-fpm-"+ver+"-global")
	fpmPoolLog := paths.LogPath(serverRoot, "php-fpm-"+ver)
	conf := fmt.Sprintf(`; devctl-managed php-fpm.conf for PHP %s
[global]
error_log = %s

[www]
user = %s
group = %s
listen = %s
listen.owner = %s
listen.group = %s
listen.mode = 0660
pm = dynamic
pm.max_children = 10
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 4
php_admin_value[error_log] = %s
php_admin_flag[log_errors] = on
php_value[auto_prepend_file] = %s
`, ver, fpmGlobalLog, siteUser, siteUser, socketPath, siteUser, siteUser, fpmPoolLog, prependPath)
	if err := os.WriteFile(fpmConfPath, []byte(conf), 0644); err != nil {
		return fmt.Errorf("write php-fpm.conf: %w", err)
	}

	return nil
}

func fpmIniPath(ver, serverRoot string) string {
	return filepath.Join(PHPDir(ver, serverRoot), "php.ini")
}

func readIni(path string) (GlobalSettings, error) {
	f, err := os.Open(path)
	if err != nil {
		return GlobalSettings{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	keys := map[string]*string{
		"upload_max_filesize": nil,
		"memory_limit":        nil,
		"max_execution_time":  nil,
		"post_max_size":       nil,
	}
	// Pre-assign pointers.
	var s GlobalSettings
	keys["upload_max_filesize"] = &s.UploadMaxFilesize
	keys["memory_limit"] = &s.MemoryLimit
	keys["max_execution_time"] = &s.MaxExecutionTime
	keys["post_max_size"] = &s.PostMaxSize

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if ptr, ok := keys[key]; ok && ptr != nil {
			*ptr = val
		}
	}
	return s, scanner.Err()
}

func writeIni(path string, s GlobalSettings) error {
	input, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	updates := map[string]string{
		"upload_max_filesize": s.UploadMaxFilesize,
		"memory_limit":        s.MemoryLimit,
		"max_execution_time":  s.MaxExecutionTime,
		"post_max_size":       s.PostMaxSize,
	}

	lines := strings.Split(string(input), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if newVal, ok := updates[key]; ok && newVal != "" {
			lines[i] = key + " = " + newVal
		}
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}
