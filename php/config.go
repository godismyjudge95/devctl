package php

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/paths"
)

//go:embed prepend.php
var prependPHP []byte

//go:embed php.ini-development
var phpIniDevelopment []byte

// InstallPrepend writes the embedded prepend.php to paths.PrependPath(serverRoot).
// Safe to call on every startup — it is idempotent.
func InstallPrepend(serverRoot string) error {
	p := paths.PrependPath(serverRoot)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("create %s dir: %w", filepath.Dir(p), err)
	}
	return os.WriteFile(p, prependPHP, 0644)
}

// GlobalSettings are the php.ini keys the PHP settings panel and php:set edit.
type GlobalSettings struct {
	UploadMaxFilesize string `json:"upload_max_filesize"`
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  string `json:"max_execution_time"`
	PostMaxSize       string `json:"post_max_size"`
}

// SettingKeys are the php.ini keys of GlobalSettings.
var SettingKeys = []string{"memory_limit", "upload_max_filesize", "post_max_size", "max_execution_time"}

func (g *GlobalSettings) fields() map[string]*string {
	return map[string]*string{
		"upload_max_filesize": &g.UploadMaxFilesize,
		"memory_limit":        &g.MemoryLimit,
		"max_execution_time":  &g.MaxExecutionTime,
		"post_max_size":       &g.PostMaxSize,
	}
}

// Values returns the non-empty settings keyed by php.ini name.
func (g GlobalSettings) Values() map[string]string {
	out := map[string]string{}
	for k, p := range g.fields() {
		if *p != "" {
			out[k] = *p
		}
	}
	return out
}

// Overlay sets every GlobalSettings key present in directives.
func (g *GlobalSettings) Overlay(directives map[string]string) {
	for k, p := range g.fields() {
		if v, ok := directives[k]; ok {
			*p = v
		}
	}
}

// GetSettings reads the effective settings from the FPM php.ini of ver,
// including the devctl managed block.
func GetSettings(ver, serverRoot string) (GlobalSettings, error) {
	data, err := os.ReadFile(fpmIniPath(ver, serverRoot))
	if err != nil {
		return GlobalSettings{}, err
	}
	return parseIniSettings(string(data)), nil
}

// BaseSettings reads the settings from the FPM php.ini of ver without the
// devctl managed block, i.e. the values PHP falls back to when no directive
// is set.
func BaseSettings(ver, serverRoot string) (GlobalSettings, error) {
	data, err := os.ReadFile(fpmIniPath(ver, serverRoot))
	if err != nil {
		return GlobalSettings{}, err
	}
	rest, _ := stripManagedBlock(string(data))
	return parseIniSettings(rest), nil
}

// ConfigurePrepend writes auto_prepend_file into the FPM php.ini for the
// given version. The FPM process must be restarted by the caller (via the
// supervisor) to pick up the new setting.
func ConfigurePrepend(ctx context.Context, ver, serverRoot string) error {
	defer LockIni()()
	path := fpmIniPath(ver, serverRoot)
	input, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	const key = "auto_prepend_file"
	line := key + " = " + paths.PrependPath(serverRoot)
	lines := strings.Split(string(input), "\n")
	begin, end := managedBlockRange(lines)
	found := false
	for i, l := range lines {
		if i >= begin && i <= end {
			continue
		}
		trimmed := strings.TrimSpace(l)
		// Match both commented and uncommented variants.
		if strings.HasPrefix(strings.TrimLeft(trimmed, ";"), key) {
			lines[i] = line
			found = true
			break
		}
	}
	if !found {
		lines = appendOutsideBlock(lines, line)
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

// WriteConfigs writes php.ini (first time only) and always regenerates php-fpm.conf.
//
// php.ini strategy:
//   - If the file does not yet exist, it is created from the embedded
//     php.ini-development template (full upstream defaults) with a devctl
//     overrides block appended at the end. PHP processes the whole file in
//     order; the last assignment wins, so our overrides take effect even
//     though the base file sets the same keys earlier.
//   - If the file already exists it is left untouched — the user may have
//     customised it. devctl-owned directives (php:set, php:ini:set) live in
//     a managed block at the end, written by WriteManagedBlock.
//     openssl.cafile and curl.cainfo are also patched so PHP trusts Caddy.
//
// php-fpm.conf is always regenerated because it contains runtime paths
// (socket, log dirs, site user) that must reflect the current environment.
func WriteConfigs(ver, serverRoot, siteUser string) error {
	socketPath := FPMSocket(ver, serverRoot)
	iniPath := fpmIniPath(ver, serverRoot)
	fpmConfPath := FPMConfigPath(ver, serverRoot)
	prependPath := paths.PrependPath(serverRoot)
	spxDataDir := SPXDataDir(ver, serverRoot)

	// Resolve siteUser uid/gid once; used throughout for chown calls.
	// devctl runs as root so every file/dir it creates is root-owned by default.
	// FPM workers run as siteUser and must be able to write to their log file
	// and read their config files — so we chown everything they touch.
	var uid, gid int = -1, -1
	userName, groupName := siteUser, siteUser
	if u, err := user.Lookup(siteUser); err == nil {
		fmt.Sscan(u.Uid, &uid)
		fmt.Sscan(u.Gid, &gid)
		userName = u.Username
		if g, err := user.LookupGroupId(u.Gid); err == nil && g.Name != "" {
			groupName = g.Name
		}
	}
	chown := func(path string) {
		if uid >= 0 {
			_ = os.Chown(path, uid, gid)
		}
	}

	// Ensure the central logs directory exists and is owned by siteUser.
	logsDir := paths.LogsDir(serverRoot)
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}
	chown(logsDir)

	// Ensure the pool log file exists and is owned by siteUser.
	// The FPM master process (root) writes the global log; pool workers (siteUser)
	// write the pool log. If the file is root-owned, PHP errors are silently lost.
	poolLog := paths.LogPath(serverRoot, "php-fpm-"+ver)
	if f, err := os.OpenFile(poolLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		f.Close()
	}
	chown(poolLog)

	// Ensure spx-data directory exists and is owned by siteUser.
	if err := os.MkdirAll(spxDataDir, 0755); err != nil {
		return fmt.Errorf("create spx-data dir: %w", err)
	}
	chown(spxDataDir)

	// Combined OS + Caddy CA bundle so static PHP OpenSSL trusts *.test.
	bundlePath, bundleErr := WriteCABundle(serverRoot, nil)
	if bundleErr != nil {
		log.Printf("php: write ca bundle: %v", bundleErr)
		bundlePath = ""
	}

	// Migrate existing php.ini files: restore spx.http_key = dev if it was
	// previously cleared to empty (SPX requires a non-empty key).
	if err := migrateSpxKey(iniPath); err != nil {
		log.Printf("php: migrate spx.http_key in %s: %v", iniPath, err)
	}

	// Write php.ini only if it does not already exist.
	// The file is seeded from the full php.ini-development upstream template so
	// the user gets sensible defaults for all PHP directives. devctl overrides
	// are appended at the end and take precedence (last-assignment-wins).
	if _, err := os.Stat(iniPath); os.IsNotExist(err) {
		caIni := ""
		if bundlePath != "" {
			caIni = fmt.Sprintf(`
; --- TLS — trust Caddy local CA plus OS CAs ---
; Static PHP OpenSSL does not use the OS trust store. Point curl and
; openssl at the combined bundle so HTTPS to *.test (meilisearch.test,
; s3.maxio.test, …) and public sites both succeed.
openssl.cafile = %s
curl.cainfo = %s
`, bundlePath, bundlePath)
		}
		overrides := fmt.Sprintf(`
; ============================================================
; devctl overrides for PHP %s
; These settings are appended after the php.ini-development
; baseline above. Edit them here to customise your environment.
; Values set in the devctl PHP settings panel, devctl php:set or
; devctl php:ini:set go in a managed block at the end of this file
; and win over the lines here.
; ============================================================

; --- Resource limits (dev-friendly defaults) ---
upload_max_filesize = 128M
memory_limit = 256M
max_execution_time = 120
post_max_size = 128M

; --- Error display and logging ---
; html_errors must be Off so that error log entries are plain text.
; With html_errors=On (the upstream default) PHP wraps messages in <b>/<br>
; tags which makes log files hard to read and parse.
html_errors = Off

; --- Auto-prepend for dd() dump interception (CLI) ---
; FPM processes override this via php_value in php-fpm.conf.
auto_prepend_file = %s

; --- OPcache — enabled with dev-safe revalidation ---
; validate_timestamps=1 ensures file changes are picked up immediately.
; revalidate_freq=0 checks on every request (no stale cache in dev).
opcache.enable = 1
opcache.validate_timestamps = 1
opcache.revalidate_freq = 0

; --- SPX profiler — zero overhead when not activated per-request ---
; Activate via browser: ?SPX_KEY=dev&SPX_ENABLED=1 (or cookies).
spx.http_enabled = 1
spx.http_key = dev
spx.http_ip_whitelist = 127.0.0.1
spx.data_dir = %s
%s`, ver, prependPath, spxDataDir, caIni)

		content := append(phpIniDevelopment, []byte(overrides)...)
		if err := os.WriteFile(iniPath, content, 0644); err != nil {
			return fmt.Errorf("write php.ini: %w", err)
		}
	} else if bundlePath != "" {
		if err := migrateCAFile(iniPath, bundlePath); err != nil {
			log.Printf("php: migrate openssl.cafile in %s: %v", iniPath, err)
		}
	}
	// Always chown php.ini — it may have been created by root on a previous
	// startup before this fix, or by the installer.
	chown(iniPath)

	// Write php-fpm.conf — always regenerated.
	// FPM is launched with -c php.ini so all php.ini settings apply to workers.
	// php_value directives override php.ini for pool workers. We use php_value
	// (not php_admin_value) for all settings except auto_prepend_file so that
	// the user's php.ini remains the single source of truth for everything else.
	//
	// html_errors must be Off here because: (a) php.ini is write-once and may
	// pre-date this setting, and (b) PHP defaults html_errors=On in web context,
	// which wraps log entries in HTML tags making them unreadable.
	fpmGlobalLog := paths.LogPath(serverRoot, "php-fpm-"+ver+"-global")
	fpmPoolLog := paths.LogPath(serverRoot, "php-fpm-"+ver)
	pidPath := FPMPidPath(ver, serverRoot)
	conf := fmt.Sprintf(`; devctl-managed php-fpm.conf for PHP %s
[global]
pid = %s
error_log = %s
daemonize = no

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
php_value[error_log] = %s
php_value[html_errors] = Off
php_admin_value[auto_prepend_file] = %s
`, ver, pidPath, fpmGlobalLog, userName, groupName, socketPath, userName, groupName, fpmPoolLog, prependPath)
	if err := os.WriteFile(fpmConfPath, []byte(conf), 0644); err != nil {
		return fmt.Errorf("write php-fpm.conf: %w", err)
	}
	chown(fpmConfPath)
	chown(PHPDir(ver, serverRoot))

	cliBin := filepath.Join(PHPDir(ver, serverRoot), "php")
	if _, err := os.Stat(cliBin); err == nil {
		if err := writeCLIWrapper(filepath.Join(paths.BinDir(serverRoot), "php"+ver), cliBin, serverRoot); err != nil {
			log.Printf("php: write cli wrapper for %s: %v", ver, err)
		}
		if err := UpdateGlobalSymlink(serverRoot); err != nil {
			log.Printf("php: %v", err)
		}
	}

	return nil
}

func fpmIniPath(ver, serverRoot string) string {
	return filepath.Join(PHPDir(ver, serverRoot), "php.ini")
}

func parseIniSettings(content string) GlobalSettings {
	var s GlobalSettings
	fields := s.fields()
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if p, ok := fields[strings.TrimSpace(parts[0])]; ok {
			*p = strings.TrimSpace(parts[1])
		}
	}
	return s
}

// migrateSpxKey ensures spx.http_key = dev in an existing php.ini.
// SPX requires a non-empty key; a previous devctl version incorrectly set it
// to empty. This restores the correct value on startup.
// The function is a no-op when the file does not exist or the key is already set.
func migrateSpxKey(path string) error {
	defer LockIni()()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	begin, end := managedBlockRange(lines)
	changed := false
	for i, line := range lines {
		if i >= begin && i <= end {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ";") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) == "spx.http_key" && strings.TrimSpace(parts[1]) == "" {
			lines[i] = "spx.http_key = dev"
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}
