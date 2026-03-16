package php

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/paths"
)

//go:embed prepend.php
var prependPHP []byte

// InstallPrepend writes the embedded prepend.php to paths.PrependPath(siteHome).
// Safe to call on every startup — it is idempotent.
func InstallPrepend(siteHome string) error {
	p := paths.PrependPath(siteHome)
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
func GetSettings(ver, siteHome string) (GlobalSettings, error) {
	iniPath := fpmIniPath(ver, siteHome)
	return readIni(iniPath)
}

// ApplySettings writes the given settings to all installed PHP-FPM versions.
// Callers should restart PHP-FPM processes via the supervisor to apply changes.
// This is a best-effort operation — errors are collected but all versions are attempted.
func ApplySettings(ctx context.Context, s GlobalSettings, siteHome string) []error {
	versions, err := InstalledVersions(siteHome)
	if err != nil {
		return []error{fmt.Errorf("list versions: %w", err)}
	}

	var errs []error
	for _, v := range versions {
		if err := writeIni(fpmIniPath(v.Version, siteHome), s); err != nil {
			errs = append(errs, fmt.Errorf("write ini for %s: %w", v.Version, err))
		}
	}
	return errs
}

// ConfigurePrepend writes auto_prepend_file into the FPM php.ini for the
// given version. The FPM process must be restarted by the caller (via the
// supervisor) to pick up the new setting.
func ConfigurePrepend(ctx context.Context, ver, siteHome string) error {
	path := fpmIniPath(ver, siteHome)
	input, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	const key = "auto_prepend_file"
	line := key + " = " + paths.PrependPath(siteHome)
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
// Called during Install to create the initial config files.
// siteUser is the non-root OS user who owns the sites directory (e.g. "daniel").
// PHP-FPM worker processes run as this user so they can write to site storage dirs.
func WriteConfigs(ver, siteHome, siteUser string) error {
	phpDir := PHPDir(ver, siteHome)
	socketPath := FPMSocket(ver)
	iniPath := fpmIniPath(ver, siteHome)
	fpmConfPath := FPMConfigPath(ver, siteHome)

	// Write php.ini with sensible defaults.
	ini := fmt.Sprintf(`; devctl-managed php.ini for PHP %s
upload_max_filesize = 128M
memory_limit = 256M
max_execution_time = 120
post_max_size = 128M
`, ver)
	if err := os.WriteFile(iniPath, []byte(ini), 0644); err != nil {
		return fmt.Errorf("write php.ini: %w", err)
	}

	// Write php-fpm.conf.
	// Run workers as siteUser so they can write to site storage directories.
	conf := fmt.Sprintf(`; devctl-managed php-fpm.conf for PHP %s
[global]
error_log = %s/php-fpm.log

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
php_admin_value[error_log] = %s/php-fpm-www.log
php_admin_flag[log_errors] = on
`, ver, phpDir, siteUser, siteUser, socketPath, siteUser, siteUser, phpDir)
	if err := os.WriteFile(fpmConfPath, []byte(conf), 0644); err != nil {
		return fmt.Errorf("write php-fpm.conf: %w", err)
	}

	return nil
}

func fpmIniPath(ver, siteHome string) string {
	return filepath.Join(PHPDir(ver, siteHome), "php.ini")
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
