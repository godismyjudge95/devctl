package php

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PrependPath is the well-known location devctl writes prepend.php to.
const PrependPath = "/etc/devctl/prepend.php"

//go:embed prepend.php
var prependPHP []byte

// InstallPrepend writes the embedded prepend.php to PrependPath.
// Safe to call on every startup — it is idempotent.
func InstallPrepend() error {
	if err := os.MkdirAll(filepath.Dir(PrependPath), 0755); err != nil {
		return fmt.Errorf("create %s dir: %w", filepath.Dir(PrependPath), err)
	}
	return os.WriteFile(PrependPath, prependPHP, 0644)
}

// GlobalSettings are the php.ini keys we expose for editing.
type GlobalSettings struct {
	UploadMaxFilesize string `json:"upload_max_filesize"`
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  string `json:"max_execution_time"`
	PostMaxSize       string `json:"post_max_size"`
}

// GetSettings reads the global settings from the first installed FPM php.ini.
// All installed versions should have the same values after ApplySettings is called.
func GetSettings(ver string) (GlobalSettings, error) {
	iniPath := fpmIniPath(ver)
	return readIni(iniPath)
}

// ApplySettings writes the given settings to all installed PHP-FPM versions.
// Callers should restart PHP-FPM processes via the supervisor to apply changes.
// This is a best-effort operation — errors are collected but all versions are attempted.
func ApplySettings(ctx context.Context, s GlobalSettings) []error {
	versions, err := InstalledVersions()
	if err != nil {
		return []error{fmt.Errorf("list versions: %w", err)}
	}

	var errs []error
	for _, v := range versions {
		if err := writeIni(fpmIniPath(v.Version), s); err != nil {
			errs = append(errs, fmt.Errorf("write ini for %s: %w", v.Version, err))
		}
	}
	return errs
}

// ConfigurePrepend writes auto_prepend_file into the FPM php.ini for the
// given version. The FPM process must be restarted by the caller (via the
// supervisor) to pick up the new setting.
func ConfigurePrepend(ctx context.Context, ver string) error {
	path := fpmIniPath(ver)
	input, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	const key = "auto_prepend_file"
	line := key + " = " + PrependPath
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

func fpmIniPath(ver string) string {
	return filepath.Join("/etc/php", ver, "fpm", "php.ini")
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
