package elevate

import (
	"fmt"
	"os"
	"strings"
)

// ResolverDropinContent builds the systemd-resolved drop-in body.
// tld should be without a leading dot (e.g. "test").
func ResolverDropinContent(port, tld string) string {
	tld = strings.TrimPrefix(tld, ".")
	return fmt.Sprintf("[Resolve]\nDNS=127.0.0.1:%s\nDomains=~%s\n", port, tld)
}

// ResolverConfigured reports whether the devctl drop-in exists.
func ResolverConfigured() bool {
	_, err := os.Stat(ResolvedDropinFile)
	return err == nil
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

func helperUninstallResolver() error {
	if err := os.Remove(ResolvedDropinFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove drop-in: %w", err)
	}
	_ = runPinned("systemctl", "reload-or-restart", "systemd-resolved")
	_ = runPinned("systemctl", "restart", "systemd-resolved")
	fmt.Println("ok")
	return nil
}
