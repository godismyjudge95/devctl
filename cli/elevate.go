package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/elevate"
)

func init() {
	Register(&Cmd{
		Name:        "elevate",
		Description: "Grant OS privileges (trust CA, DNS resolver, ports) — requires sudo",
		Usage:       "[trust|resolver|ports|install]",
		Args: []ArgDef{{
			Name:        "target",
			Description: "Optional target: trust, resolver, ports, or install (default: all of trust+resolver+ports)",
			Optional:    true,
		}},
		Examples: []string{
			"sudo devctl elevate",
			"sudo devctl elevate trust",
			"sudo devctl elevate resolver",
			"sudo devctl elevate ports",
			"sudo devctl elevate install",
		},
		Handler: handleElevate,
	})
	Register(&Cmd{
		Name:        "unelevate",
		Description: "Revert OS privileges granted by elevate — requires sudo",
		Usage:       "[trust|resolver|ports]",
		Args: []ArgDef{{
			Name:        "target",
			Description: "Optional target: trust, resolver, or ports (default: all)",
			Optional:    true,
		}},
		Examples: []string{
			"sudo devctl unelevate",
			"sudo devctl unelevate trust",
			"sudo devctl unelevate resolver",
		},
		Handler: handleUnelevate,
	})
	Register(&Cmd{
		Name:        "elevate:status",
		Description: "Show which elevate targets are configured",
		Examples:    []string{"devctl elevate:status", "devctl elevate:status --json"},
		Handler:     handleElevateStatus,
	})
}

func handleElevate(c *Client, args []string, jsonMode bool) error {
	targets, err := parseElevateTargets(args, true)
	if err != nil {
		return err
	}
	facts, err := collectElevateFacts(c)
	if err != nil {
		// Ports/install can proceed without daemon; trust needs CA.
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	if err := elevate.Elevate(facts, targets, os.Stdout); err != nil {
		return err
	}
	if jsonMode {
		PrintJSON(map[string]any{"status": "ok", "targets": targets})
		return nil
	}
	PrintOK("elevate complete")
	return nil
}

func handleUnelevate(c *Client, args []string, jsonMode bool) error {
	targets, err := parseElevateTargets(args, false)
	if err != nil {
		return err
	}
	facts, _ := collectElevateFacts(c)
	if err := elevate.Unelevate(facts, targets, os.Stdout); err != nil {
		return err
	}
	if jsonMode {
		PrintJSON(map[string]any{"status": "ok", "targets": targets})
		return nil
	}
	PrintOK("unelevate complete")
	return nil
}

func handleElevateStatus(c *Client, args []string, jsonMode bool) error {
	fp := ""
	// Best-effort: fetch CA fingerprint from daemon for accurate trust status.
	if pem, err := c.GetCACertPEM(); err == nil && len(pem) > 0 {
		if f, err := elevate.ParseCAFingerprint(pem); err == nil {
			fp = f
		}
	}
	st := elevate.CollectStatus(fp)
	if jsonMode {
		PrintJSON(st)
		return nil
	}
	Header("Elevation status")
	KV("trust", boolStatus(st.TrustConfigured))
	KV("resolver", boolStatus(st.ResolverConfigured))
	KV("ports (ambient unit)", boolStatus(st.PortsConfigured))
	KV("unit runs as user", boolStatus(st.UnitRunsAsUser))
	KV("unit path", st.UnitPath)
	if len(st.Notes) > 0 {
		fmt.Println()
		Header("Suggested fixes")
		for _, n := range st.Notes {
			fmt.Println("  " + n)
		}
	}
	return nil
}

func boolStatus(ok bool) string {
	if ok {
		return styleOK.Render("configured")
	}
	return styleWarn.Render("not configured")
}

func parseElevateTargets(args []string, allowInstall bool) ([]string, error) {
	if len(args) == 0 {
		return nil, nil // means all
	}
	var out []string
	for _, a := range args {
		switch a {
		case elevate.TargetTrust, elevate.TargetResolver, elevate.TargetPorts:
			out = append(out, a)
		case elevate.TargetInstall:
			if !allowInstall {
				return nil, fmt.Errorf("install is not a valid unelevate target")
			}
			out = append(out, a)
		default:
			return nil, fmt.Errorf("unknown target %q (want trust, resolver, ports%s)", a, map[bool]string{true: ", or install", false: ""}[allowInstall])
		}
	}
	return out, nil
}

// collectElevateFacts gathers paths and CA material for elevate.
func collectElevateFacts(c *Client) (elevate.Facts, error) {
	f := elevate.Facts{
		DNSPort: "5354",
		DNSTLD:  "test",
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		f.BinaryPath = exe
	}
	if su := os.Getenv("SUDO_USER"); su != "" && su != "root" {
		f.SiteUser = su
	} else if su := os.Getenv("DEVCTL_SITE_USER"); su != "" {
		f.SiteUser = su
	}
	// Settings from daemon.
	settings, err := c.GetSettings()
	if err == nil {
		if v, ok := settings["dns_port"]; ok && v != "" {
			f.DNSPort = v
		}
		if v, ok := settings["dns_tld"]; ok && v != "" {
			f.DNSTLD = strings.TrimPrefix(v, ".")
		}
	}
	// CA from daemon.
	pem, err := c.GetCACertPEM()
	if err != nil {
		return f, fmt.Errorf("fetch CA from daemon: %w (start the daemon first for trust)", err)
	}
	if len(pem) > 0 {
		f.CAPEM = pem
		if fp, err := elevate.ParseCAFingerprint(pem); err == nil {
			f.CAFingerprint = fp
		}
	}
	// Server root from env or open helper paths.
	if v := os.Getenv("DEVCTL_SERVER_ROOT"); v != "" {
		f.ServerRoot = v
	}
	return f, nil
}

// GetCACertPEM fetches the Caddy internal CA PEM from GET /api/tls/cert.
func (c *Client) GetCACertPEM() ([]byte, error) {
	raw, err := c.getRaw("/api/tls/cert")
	if err != nil {
		return nil, err
	}
	return []byte(raw), nil
}
