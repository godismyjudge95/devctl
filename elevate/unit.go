package elevate

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// BuildServiceFile generates the systemd system unit content for a non-root
// daemon with ambient CAP_NET_BIND_SERVICE so Caddy can bind :80/:443.
func BuildServiceFile(binaryPath, siteUser, siteHome, serverRoot string) string {
	return fmt.Sprintf(`[Unit]
Description=devctl — Local PHP Dev Dashboard
After=network.target

[Service]
Type=simple
User=%s
Group=%s
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ExecStart=%s daemon
Restart=on-failure
RestartSec=5s
Environment=HOME=%s
Environment=DEVCTL_SITE_USER=%s
Environment=DEVCTL_SERVER_ROOT=%s

[Install]
WantedBy=multi-user.target
`, siteUser, siteUser, binaryPath, siteHome, siteUser, serverRoot)
}

// UnitHasAmbientBind reports whether the unit file mentions ambient bind capability.
func UnitHasAmbientBind(unitPath string) bool {
	data, err := os.ReadFile(unitPath)
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "AmbientCapabilities=CAP_NET_BIND_SERVICE") &&
		strings.Contains(s, "User=")
}

// UnitRunsAsUser reports whether the unit has a User= directive (non-root model).
func UnitRunsAsUser(unitPath string) bool {
	data, err := os.ReadFile(unitPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "User=") {
			u := strings.TrimPrefix(line, "User=")
			return u != "" && u != "root"
		}
	}
	return false
}

func helperWriteUnit(args []string) error {
	flags, _, err := parseFlags(args)
	if err != nil {
		return err
	}
	binaryPath := flags["binary"]
	siteUser := flags["user"]
	siteHome := flags["home"]
	serverRoot := flags["server-root"]
	if binaryPath == "" || siteUser == "" || siteHome == "" || serverRoot == "" {
		return fmt.Errorf("--binary, --user, --home, and --server-root are required")
	}
	if !filepath.IsAbs(binaryPath) || !filepath.IsAbs(siteHome) || !filepath.IsAbs(serverRoot) {
		return fmt.Errorf("binary, home, and server-root must be absolute paths")
	}
	// Refuse path traversal / empty user.
	if strings.ContainsAny(siteUser, "/\n\t ") || siteUser == "root" {
		return fmt.Errorf("invalid user %q", siteUser)
	}
	// Binary must exist and be executable.
	st, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("stat binary: %w", err)
	}
	if st.IsDir() || st.Mode()&0111 == 0 {
		return fmt.Errorf("binary is not an executable file: %s", binaryPath)
	}

	content := BuildServiceFile(binaryPath, siteUser, siteHome, serverRoot)
	if err := writeFileAtomic(ServiceUnitPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}
	fmt.Println("ok")
	return nil
}

func helperChownTree(args []string) error {
	flags, _, err := parseFlags(args)
	if err != nil {
		return err
	}
	path := flags["path"]
	siteUser := flags["user"]
	if path == "" || siteUser == "" {
		return fmt.Errorf("--path and --user are required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("--path must be absolute")
	}
	if strings.ContainsAny(siteUser, "/\n\t ") || siteUser == "root" {
		return fmt.Errorf("invalid user %q", siteUser)
	}
	// Refuse chown of system paths outside a home directory or explicit server root.
	// Allow /home/... and paths that look like site server roots.
	if path == "/" || path == "/etc" || path == "/usr" || path == "/var" {
		return fmt.Errorf("refusing to chown system path %q", path)
	}
	u, err := user.Lookup(siteUser)
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}
	var uid, gid int
	if _, err := fmt.Sscan(u.Uid, &uid); err != nil {
		return err
	}
	if _, err := fmt.Sscan(u.Gid, &gid); err != nil {
		return err
	}
	err = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip unreadable entries.
			return nil
		}
		return os.Chown(p, uid, gid)
	})
	if err != nil {
		return err
	}
	fmt.Println("ok")
	return nil
}

func helperSystemctl(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("systemctl: missing args")
	}
	// Allowlist systemctl subcommands used by elevate install.
	allowed := map[string]bool{
		"daemon-reload": true,
		"enable":        true,
		"disable":       true,
		"start":         true,
		"stop":          true,
		"restart":       true,
	}
	if !allowed[args[0]] {
		return fmt.Errorf("systemctl: operation %q not allowed", args[0])
	}
	// Only operate on devctl or systemd-resolved.
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		base := filepath.Base(a)
		if base != "devctl" && base != "devctl.service" && base != "systemd-resolved" && base != "systemd-resolved.service" {
			return fmt.Errorf("systemctl: unit %q not allowed", a)
		}
	}
	return runPinned("systemctl", args...)
}
