package elevate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolverDropinContent builds the systemd-resolved drop-in body.
// tld should be without a leading dot (e.g. "test").
func ResolverDropinContent(port, tld string) string {
	tld = strings.TrimPrefix(tld, ".")
	return fmt.Sprintf("[Resolve]\nDNS=127.0.0.1:%s\nDomains=~%s\n", port, tld)
}

// DarwinResolverPath is /etc/resolver/<tld>.
func DarwinResolverPath(tld string) string {
	tld = strings.TrimPrefix(tld, ".")
	return filepath.Join(DarwinResolverDir, tld)
}

// DarwinResolverContent is the macOS resolver stub for the in-process DNS.
func DarwinResolverContent(port string) string {
	return fmt.Sprintf("nameserver 127.0.0.1\nport %s\n", port)
}

// ResolverConfigured reports whether the OS DNS stub exists.
func ResolverConfigured() bool {
	if runtime.GOOS == "darwin" {
		data, err := os.ReadFile(DarwinResolverPath("test"))
		if err != nil {
			return false
		}
		return darwinResolverFileConfigured(string(data))
	}
	_, err := os.Stat(ResolvedDropinFile)
	return err == nil
}

func darwinResolverFileConfigured(body string) bool {
	return strings.Contains(body, "nameserver 127.0.0.1") && strings.Contains(body, "port ")
}

func helperInstallResolver(args []string) error {
	flags, _, err := parseFlags(args)
	if err != nil {
		return err
	}
	port := flags["port"]
	if port == "" {
		port = "5354"
	}
	tld := flags["tld"]
	if tld == "" {
		tld = "test"
	}
	// Basic validation: port digits only, tld single label.
	for _, c := range port {
		if c < '0' || c > '9' {
			return fmt.Errorf("invalid port %q", port)
		}
	}
	tld = strings.TrimPrefix(tld, ".")
	if tld == "" || strings.ContainsAny(tld, "./ \t\n") {
		return fmt.Errorf("invalid tld %q", tld)
	}

	if runtime.GOOS == "darwin" {
		path := DarwinResolverPath(tld)
		if err := writeFileAtomic(path, []byte(DarwinResolverContent(port)), 0644); err != nil {
			return fmt.Errorf("write resolver: %w", err)
		}
		fmt.Println("ok")
		return nil
	}

	// Skip (not fail) if systemd-resolved is not available.
	if _, err := os.Stat("/run/systemd/resolve"); err != nil {
		if _, err2 := os.Stat("/lib/systemd/systemd-resolved"); err2 != nil {
			fmt.Println("skipped (systemd-resolved not detected)")
			return nil
		}
	}

	content := ResolverDropinContent(port, tld)
	if err := writeFileAtomic(ResolvedDropinFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write drop-in: %w", err)
	}
	if err := runPinned("systemctl", "reload-or-restart", "systemd-resolved"); err != nil {
		// Some systems only support restart.
		if err2 := runPinned("systemctl", "restart", "systemd-resolved"); err2 != nil {
			return fmt.Errorf("restart systemd-resolved: %v (also: %v)", err, err2)
		}
	}
	fmt.Println("ok")
	return nil
}

func helperUninstallResolver(args []string) error {
	flags, _, err := parseFlags(args)
	if err != nil {
		return err
	}
	tld := flags["tld"]
	if tld == "" {
		tld = "test"
	}
	tld = strings.TrimPrefix(tld, ".")

	if runtime.GOOS == "darwin" {
		path := DarwinResolverPath(tld)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove resolver: %w", err)
		}
		fmt.Println("ok")
		return nil
	}

	if err := os.Remove(ResolvedDropinFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove drop-in: %w", err)
	}
	_ = runPinned("systemctl", "reload-or-restart", "systemd-resolved")
	_ = runPinned("systemctl", "restart", "systemd-resolved")
	fmt.Println("ok")
	return nil
}
