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
// Rules apply on every interface so a DHCP/LAN A record reaches Caddy.
// Apple vmnet guests (192.168.64.0/24) are excluded so they can reach the
// public internet on 80/443 without matching these redirects.
func PFAnchorContent(httpPort, httpsPort string) string {
	httpPort = strings.TrimPrefix(httpPort, ":")
	httpsPort = strings.TrimPrefix(httpsPort, ":")
	return fmt.Sprintf(
		"rdr pass inet proto tcp from ! 192.168.64.0/24 to any port 80 -> 127.0.0.1 port %s\n"+
			"rdr pass inet proto tcp from ! 192.168.64.0/24 to any port 443 -> 127.0.0.1 port %s\n",
		httpPort, httpsPort)
}

func pfConfSnippet() string {
	return pfBegin + "\n" +
		`rdr-anchor "` + PFAnchorName + `"` + "\n" +
		`load anchor "` + PFAnchorName + `" from "` + PFAnchorPath + `"` + "\n" +
		pfEnd + "\n"
}

// mergePFConf inserts the devctl rdr-anchor in the translation section.
// Appending after filter anchors makes pfctl reject the file (rule order).
func mergePFConf(body string) string {
	body = stripPFConfSnippetString(body)
	snippet := pfConfSnippet()
	needle := `rdr-anchor "com.apple/*"`
	if i := strings.Index(body, needle); i >= 0 {
		i += len(needle)
		if i < len(body) && body[i] == '\n' {
			i++
		}
		return body[:i] + snippet + body[i:]
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return body + snippet
}

func stripPFConfSnippetString(body string) string {
	start := strings.Index(body, pfBegin)
	end := strings.Index(body, pfEnd)
	if start < 0 || end < 0 || end < start {
		return body
	}
	end += len(pfEnd)
	if end < len(body) && body[end] == '\n' {
		end++
	}
	return body[:start] + body[end:]
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
	merged := mergePFConf(body)
	if merged == body {
		return nil
	}
	return writeFileAtomic(PFConfPath, []byte(merged), 0644)
}

func stripPFConfSnippet() error {
	data, err := os.ReadFile(PFConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	out := stripPFConfSnippetString(string(data))
	if out == string(data) {
		return nil
	}
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
