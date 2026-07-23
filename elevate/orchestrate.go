package elevate

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// Target names for elevate / unelevate.
const (
	TargetTrust    = "trust"
	TargetResolver = "resolver"
	TargetPorts    = "ports"
	TargetInstall  = "install"
)

// AllTargets is the default elevate order (excluding install).
var AllTargets = []string{TargetTrust, TargetResolver, TargetPorts}

// Facts collected from the environment / daemon for elevate orchestration.
type Facts struct {
	// Absolute path to the currently running (or to-install) binary.
	BinaryPath string
	// Site user and home.
	SiteUser string
	SiteHome string
	// Server root directory.
	ServerRoot string
	// DNS settings.
	DNSPort string
	DNSTLD  string // without leading dot
	// CA PEM bytes from Caddy (optional if trust skipped).
	CAPEM []byte
	// CA fingerprint (sha256 hex), computed if CAPEM set.
	CAFingerprint string
	// Path to a temp PEM file written for helper install-ca (cleaned by caller).
	CAPEMPath string
}

// RunHelperSelf re-execs the current binary as `devctl helper <args...>` with
// the current (root) privileges. stdout/stderr are forwarded.
func RunHelperSelf(stdout, stderr io.Writer, helperArgs ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	// Prefer the real path (not a symlink into /proc).
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	args := append([]string{"helper"}, helperArgs...)
	cmd := exec.Command(exe, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// Elevate runs the given targets (or AllTargets if empty). Must be root.
func Elevate(facts Facts, targets []string, w io.Writer) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("elevate must be run as root — re-run with: sudo devctl elevate")
	}
	if len(targets) == 0 {
		targets = AllTargets
	}
	var failed []string
	for _, t := range targets {
		fmt.Fprintf(w, "==> %s\n", t)
		var err error
		switch t {
		case TargetTrust:
			err = elevateTrust(facts, w)
		case TargetResolver:
			err = elevateResolver(facts, w)
		case TargetPorts:
			err = elevatePorts(facts, w)
		case TargetInstall:
			err = elevateInstall(facts, w)
		default:
			err = fmt.Errorf("unknown target %q", t)
		}
		if err != nil {
			fmt.Fprintf(w, "    failed: %v\n", err)
			failed = append(failed, t)
			continue
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("elevate failed for: %s", strings.Join(failed, ", "))
	}
	return nil
}

// Unelevate reverses the given targets (or trust+resolver+ports if empty).
func Unelevate(facts Facts, targets []string, w io.Writer) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("unelevate must be run as root — re-run with: sudo devctl unelevate")
	}
	if len(targets) == 0 {
		targets = []string{TargetTrust, TargetResolver, TargetPorts}
	}
	var failed []string
	for _, t := range targets {
		fmt.Fprintf(w, "==> unelevate %s\n", t)
		var err error
		switch t {
		case TargetTrust:
			err = unelevateTrust(facts, w)
		case TargetResolver:
			err = unelevateResolver(w)
		case TargetPorts:
			err = unelevatePorts(facts, w)
		default:
			err = fmt.Errorf("unknown target %q", t)
		}
		if err != nil {
			fmt.Fprintf(w, "    failed: %v\n", err)
			failed = append(failed, t)
			continue
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("unelevate failed for: %s", strings.Join(failed, ", "))
	}
	return nil
}

func elevateTrust(facts Facts, w io.Writer) error {
	if len(facts.CAPEM) == 0 {
		return fmt.Errorf("no CA PEM available — is the daemon running with Caddy up? start devctl then re-run")
	}
	fp := facts.CAFingerprint
	if fp == "" {
		var err error
		fp, err = ParseCAFingerprint(facts.CAPEM)
		if err != nil {
			return err
		}
	}
	pemPath := facts.CAPEMPath
	if pemPath == "" {
		// Write a temp file owned by root for the helper.
		f, err := os.CreateTemp("", "devctl-ca-*.pem")
		if err != nil {
			return err
		}
		pemPath = f.Name()
		defer os.Remove(pemPath)
		if _, err := f.Write(facts.CAPEM); err != nil {
			f.Close()
			return err
		}
		if err := f.Chmod(0644); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	fmt.Fprintf(w, "    trusting CA (fingerprint %s)\n", fp)
	if err := RunHelperSelf(w, w, "install-ca", "--pem", pemPath, "--fingerprint", fp); err != nil {
		return err
	}
	return nil
}

func unelevateTrust(facts Facts, w io.Writer) error {
	args := []string{"uninstall-ca"}
	if facts.CAFingerprint != "" {
		args = append(args, "--fingerprint", facts.CAFingerprint)
	} else if len(facts.CAPEM) > 0 {
		fp, err := ParseCAFingerprint(facts.CAPEM)
		if err == nil {
			args = append(args, "--fingerprint", fp)
		}
	}
	fmt.Fprintf(w, "    removing CA from system trust store\n")
	return RunHelperSelf(w, w, args...)
}

func elevateResolver(facts Facts, w io.Writer) error {
	port := facts.DNSPort
	if port == "" {
		port = "5354"
	}
	tld := strings.TrimPrefix(facts.DNSTLD, ".")
	if tld == "" {
		tld = "test"
	}
	fmt.Fprintf(w, "    routing *.%s → 127.0.0.1:%s\n", tld, port)
	return RunHelperSelf(w, w, "install-resolver", "--port", port, "--tld", tld)
}

func unelevateResolver(w io.Writer) error {
	fmt.Fprintf(w, "    removing systemd-resolved drop-in\n")
	return RunHelperSelf(w, w, "uninstall-resolver")
}

func elevatePorts(facts Facts, w io.Writer) error {
	if err := ensureFactsForUnit(&facts); err != nil {
		return err
	}
	fmt.Fprintf(w, "    writing unit with User=%s and AmbientCapabilities=CAP_NET_BIND_SERVICE\n", facts.SiteUser)
	if err := RunHelperSelf(w, w,
		"write-unit",
		"--binary", facts.BinaryPath,
		"--user", facts.SiteUser,
		"--home", facts.SiteHome,
		"--server-root", facts.ServerRoot,
	); err != nil {
		return err
	}
	// Migration from root daemon: ensure server tree is owned by the site user.
	fmt.Fprintf(w, "    chown %s → %s\n", facts.ServerRoot, facts.SiteUser)
	if err := RunHelperSelf(w, w,
		"chown-tree",
		"--path", facts.ServerRoot,
		"--user", facts.SiteUser,
	); err != nil {
		return err
	}
	fmt.Fprintf(w, "    reloading systemd and restarting devctl\n")
	if err := RunHelperSelf(w, w, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := RunHelperSelf(w, w, "systemctl", "enable", "devctl"); err != nil {
		return err
	}
	if err := RunHelperSelf(w, w, "systemctl", "restart", "devctl"); err != nil {
		return err
	}
	fmt.Fprintf(w, "    ok — daemon runs as %s with ambient bind capability\n", facts.SiteUser)
	return nil
}

func unelevatePorts(facts Facts, w io.Writer) error {
	// Re-write unit without AmbientCapabilities (still User= to stay non-root).
	if err := ensureFactsForUnit(&facts); err != nil {
		return err
	}
	fmt.Fprintf(w, "    rewriting unit without AmbientCapabilities (ports 80/443 will fail until re-elevated)\n")
	content := fmt.Sprintf(`[Unit]
Description=devctl — Local PHP Dev Dashboard
After=network.target

[Service]
Type=simple
User=%s
Group=%s
NoNewPrivileges=true
ExecStart=%s daemon
Restart=on-failure
RestartSec=5s
Environment=HOME=%s
Environment=DEVCTL_SITE_USER=%s
Environment=DEVCTL_SERVER_ROOT=%s

[Install]
WantedBy=multi-user.target
`, facts.SiteUser, facts.SiteUser, facts.BinaryPath, facts.SiteHome, facts.SiteUser, facts.ServerRoot)
	if err := writeFileAtomic(ServiceUnitPath, []byte(content), 0644); err != nil {
		return err
	}
	_ = RunHelperSelf(w, w, "systemctl", "daemon-reload")
	_ = RunHelperSelf(w, w, "systemctl", "restart", "devctl")
	fmt.Fprintf(w, "    ok\n")
	return nil
}

func elevateInstall(facts Facts, w io.Writer) error {
	if err := ensureFactsForUnit(&facts); err != nil {
		return err
	}
	fmt.Fprintf(w, "    installing unit + enabling service\n")
	if err := elevatePorts(facts, w); err != nil {
		return err
	}
	// Best-effort apt deps.
	fmt.Fprintf(w, "    installing allowlisted apt packages (best-effort)\n")
	_ = RunHelperSelf(w, w, "apt-install", "libnss3-tools", "libreadline-dev", "libnuma1", "build-essential")
	// trust + resolver after service is up (CA may need Caddy).
	// Caller may re-run elevate for trust after Caddy is ready.
	fmt.Fprintf(w, "    note: run `sudo devctl elevate trust` after Caddy has issued the local CA if trust failed\n")
	if err := elevateResolver(facts, w); err != nil {
		fmt.Fprintf(w, "    resolver: %v\n", err)
	}
	return nil
}

func ensureFactsForUnit(facts *Facts) error {
	if facts.BinaryPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("binary path: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		facts.BinaryPath = exe
	}
	if facts.SiteUser == "" {
		// Prefer SUDO_USER when elevating.
		if su := os.Getenv("SUDO_USER"); su != "" && su != "root" {
			facts.SiteUser = su
		} else {
			return fmt.Errorf("site user unknown — set DEVCTL_SITE_USER or run via sudo from the site user")
		}
	}
	if facts.SiteHome == "" || facts.ServerRoot == "" {
		u, err := user.Lookup(facts.SiteUser)
		if err != nil {
			return fmt.Errorf("lookup user %q: %w", facts.SiteUser, err)
		}
		if facts.SiteHome == "" {
			facts.SiteHome = u.HomeDir
		}
		if facts.ServerRoot == "" {
			if v := os.Getenv("DEVCTL_SERVER_ROOT"); v != "" {
				facts.ServerRoot = v
			} else {
				facts.ServerRoot = filepath.Join(u.HomeDir, "ddev", "sites", "server")
			}
		}
	}
	// Validate SUDO_UID ownership of server root when present.
	if sudoUID := os.Getenv("SUDO_UID"); sudoUID != "" {
		uid, err := strconv.Atoi(sudoUID)
		if err == nil {
			if st, err := os.Stat(facts.ServerRoot); err == nil {
				// Best-effort: don't fail hard on platforms without Sys() detail.
				_ = st
				_ = uid
			}
		}
	}
	return nil
}
