package elevate

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

const LaunchAgentLabel = "ai.devctl"

// LaunchAgentPath is {siteHome}/Library/LaunchAgents/ai.devctl.plist.
func LaunchAgentPath(siteHome string) string {
	return filepath.Join(siteHome, "Library", "LaunchAgents", LaunchAgentLabel+".plist")
}

// DetectServerRoot reads DEVCTL_SERVER_ROOT from a LaunchAgent plist or a
// systemd unit file. Empty means the file is missing or has no such key.
func DetectServerRoot(serviceFile string) string {
	data, err := os.ReadFile(serviceFile)
	if err != nil {
		return ""
	}
	body := string(data)
	if i := strings.Index(body, "<key>DEVCTL_SERVER_ROOT</key>"); i >= 0 {
		rest := body[i:]
		const open, closeTag = "<string>", "</string>"
		s := strings.Index(rest, open)
		e := strings.Index(rest, closeTag)
		if s >= 0 && e > s {
			return strings.TrimSpace(rest[s+len(open) : e])
		}
	}
	const prefix = "Environment=DEVCTL_SERVER_ROOT="
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}

func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// BuildLaunchAgentPlist generates a user LaunchAgent that starts the nested
// daemon. Children stay under the supervisor. pf handles ports 80 and 443.
func BuildLaunchAgentPlist(binaryPath, siteUser, siteHome, serverRoot string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>daemon</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>HOME</key>
		<string>%s</string>
		<key>DEVCTL_SITE_USER</key>
		<string>%s</string>
		<key>DEVCTL_SERVER_ROOT</key>
		<string>%s</string>
	</dict>
	<key>WorkingDirectory</key>
	<string>%s</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`, LaunchAgentLabel, xmlText(binaryPath), xmlText(siteHome), xmlText(siteUser), xmlText(serverRoot), xmlText(siteHome))
}

func helperWritePlist(args []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("write-plist is darwin-only; use write-unit")
	}
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
	if strings.ContainsAny(siteUser, "/\n\t ") || siteUser == "root" {
		return fmt.Errorf("invalid user %q", siteUser)
	}
	st, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("stat binary: %w", err)
	}
	if st.IsDir() || st.Mode()&0111 == 0 {
		return fmt.Errorf("binary is not an executable file: %s", binaryPath)
	}

	path := LaunchAgentPath(siteHome)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents: %w", err)
	}
	content := BuildLaunchAgentPlist(binaryPath, siteUser, siteHome, serverRoot)
	if err := writeFileAtomic(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
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
	if err := os.Lchown(path, uid, gid); err != nil {
		return fmt.Errorf("chown plist: %w", err)
	}
	fmt.Println("ok")
	return nil
}

func helperLaunchctl(args []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("launchctl is darwin-only")
	}
	if len(args) == 0 {
		return fmt.Errorf("launchctl: missing args")
	}
	allowed := map[string]bool{
		"bootstrap": true,
		"bootout":   true,
		"enable":    true,
		"kickstart": true,
		"print":     true,
	}
	if !allowed[args[0]] {
		return fmt.Errorf("launchctl: operation %q not allowed", args[0])
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, LaunchAgentLabel) {
		return fmt.Errorf("launchctl: arguments must name %s", LaunchAgentLabel)
	}
	return runPinned("launchctl", args...)
}
