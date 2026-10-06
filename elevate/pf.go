package elevate

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/danielgormly/devctl/dist"
)

const (
	PFAnchorName = "devctl"
	PFAnchorPath = "/etc/pf.anchors/devctl"
	PFConfPath   = "/etc/pf.conf"
	pfBegin      = "# BEGIN devctl"
	pfEnd        = "# END devctl"
)

// PFAnchorContent is the rdr rules that send 80/443 to Caddy's Darwin ports.
func PFAnchorContent(httpPort, httpsPort string) string {
	httpPort = strings.TrimPrefix(httpPort, ":")
	httpsPort = strings.TrimPrefix(httpsPort, ":")
	return fmt.Sprintf("rdr pass on lo0 inet proto tcp from any to any port 80 -> 127.0.0.1 port %s\nrdr pass on lo0 inet proto tcp from any to any port 443 -> 127.0.0.1 port %s\n", httpPort, httpsPort)
}

func pfConfSnippet() string {
	return pfBegin + "\n" +
		`rdr-anchor "` + PFAnchorName + `"` + "\n" +
		`load anchor "` + PFAnchorName + `" from "` + PFAnchorPath + `"` + "\n" +
		pfEnd + "\n"
}

func helperInstallPF(args []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("install-pf is darwin-only")
	}
	flags, _, err := parseFlags(args)
	if err != nil {
		return err
	}
	httpPort := flags["http"]
	httpsPort := flags["https"]
	if httpPort == "" || httpsPort == "" {
		listen := dist.ListenHTTPFor("darwin")
		if len(listen) >= 2 {
			if httpPort == "" {
				httpPort = strings.TrimPrefix(listen[0], ":")
			}
			if httpsPort == "" {
				httpsPort = strings.TrimPrefix(listen[1], ":")
			}
		}
	}
	if httpPort == "" || httpsPort == "" {
		return fmt.Errorf("--http and --https are required")
	}

	if err := writeFileAtomic(PFAnchorPath, []byte(PFAnchorContent(httpPort, httpsPort)), 0644); err != nil {
		return fmt.Errorf("write pf anchor: %w", err)
	}
	if err := ensurePFConfSnippet(); err != nil {
		return err
	}
	if err := runPinned("pfctl", "-f", PFConfPath); err != nil {
		return fmt.Errorf("pfctl load: %w", err)
	}
	_ = runPinned("pfctl", "-E")
	fmt.Println("ok")
	return nil
}

func helperUninstallPF() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("uninstall-pf is darwin-only")
	}
	if err := stripPFConfSnippet(); err != nil {
		return err
	}
	if err := os.Remove(PFAnchorPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = runPinned("pfctl", "-f", PFConfPath)
	fmt.Println("ok")
	return nil
}

func ensurePFConfSnippet() error {
	data, err := os.ReadFile(PFConfPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", PFConfPath, err)
	}
	body := string(data)
	if strings.Contains(body, pfBegin) {
		return nil
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return writeFileAtomic(PFConfPath, []byte(body+pfConfSnippet()), 0644)
}

func stripPFConfSnippet() error {
	data, err := os.ReadFile(PFConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	body := string(data)
	start := strings.Index(body, pfBegin)
	end := strings.Index(body, pfEnd)
	if start < 0 || end < 0 || end < start {
		return nil
	}
	end += len(pfEnd)
	if end < len(body) && body[end] == '\n' {
		end++
	}
	out := body[:start] + body[end:]
	return writeFileAtomic(PFConfPath, []byte(out), 0644)
}

// PFConfigured reports whether the Darwin pf anchor file exists.
func PFConfigured() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := os.Stat(PFAnchorPath)
	return err == nil
}
