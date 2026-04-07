// Package selfinstall implements the "devctl install" sub-command, which
// installs devctl as a systemd system service without any manual steps.
package selfinstall

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgormly/devctl/cli"
	"github.com/danielgormly/devctl/db"
	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/install"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/tools"
)

const serviceDir = "/etc/systemd/system"
const serviceName = "devctl.service"

// Run is the entry point for `devctl install`. args is os.Args[2:].
func Run(args []string) error {
	fs := flag.NewFlagSet("devctl install", flag.ContinueOnError)
	flagUser := fs.String("user", "", "non-root user whose sites dir devctl will manage (auto-detected from SUDO_USER if omitted)")
	flagSitesDir := fs.String("sites-dir", "", "directory where sites are stored (default: ~/sites)")
	flagPath := fs.String("path", "", "directory to install the devctl binary into (default: /usr/local/bin)")
	flagYes := fs.Bool("yes", false, "skip all confirmation prompts (for scripted installs)")
	fs.SetOutput(os.Stderr)

	if err := fs.Parse(args); err != nil {
		return err
	}

	if os.Getuid() != 0 {
		return errors.New("must be run as root — re-run with: sudo devctl install")
	}

	// Refuse to self-install from `go run` — the temp binary path is useless.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	if strings.Contains(exe, "/go-build") || strings.Contains(exe, "/tmp/go-") {
		return errors.New("cannot self-install when running via `go run`. Build the binary first with `make build`")
	}

	isTTY := isTerminal()
	if !isTTY && !*flagYes {
		return errors.New("non-interactive mode detected — provide --user, --sites-dir, --path, and --yes flags for scripted installs")
	}

	r := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("devctl self-installer")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// --- 1. Resolve username ---
	siteUser, siteHome, err := resolveUser(*flagUser, *flagYes, r)
	if err != nil {
		return err
	}

	// --- 2. Resolve sites directory ---
	existingServiceFile := filepath.Join(serviceDir, serviceName)
	sitesDir, err := resolveSitesDir(*flagSitesDir, siteHome, *flagYes, r, existingServiceFile)
	if err != nil {
		return err
	}

	// serverRoot is the fully resolved server directory derived from sitesDir.
	serverRoot := filepath.Join(sitesDir, "server")

	// --- 3. Resolve binary install path ---
	installDir, err := resolveInstallDir(*flagPath, serverRoot, *flagYes, r)
	if err != nil {
		return err
	}
	binaryDest := filepath.Join(installDir, "devctl")

	// --- Check if already installed ---
	alreadyInstalled := fileExists(binaryDest) && fileExists(existingServiceFile)
	if alreadyInstalled && !*flagYes {
		fmt.Printf("devctl appears to already be installed at %s.\n", binaryDest)
		fmt.Print("Reinstall / upgrade? [y/N] ")
		if !confirm(r) {
			fmt.Println("Aborted.")
			return nil
		}
		fmt.Println()
	}

	binDir := paths.BinDir(serverRoot)

	// --- Confirmation summary ---
	if !*flagYes {
		fmt.Println("devctl will perform the following steps:")
		fmt.Printf("  1. Copy binary      → %s\n", binaryDest)
		fmt.Printf("  2. Write service    → %s\n", existingServiceFile)
		fmt.Printf("  3. Set sites dir    → %s (saved to DB)\n", sitesDir)
		fmt.Printf("  4. Link binary      → %s/devctl\n", binDir)
		fmt.Printf("  5. Configure shell PATH for %s\n", siteUser)
		fmt.Println("  6. Download dev tools (sqlite3, ...)")
		fmt.Println("  7. systemctl daemon-reload")
		fmt.Println("  8. systemctl enable devctl")
		fmt.Println("  9. systemctl start devctl")
		fmt.Println()
		fmt.Print("Proceed? [y/N] ")
		if !confirm(r) {
			fmt.Println("Aborted.")
			return nil
		}
		fmt.Println()
	}

	steps := []struct {
		label string
		fn    func() error
	}{
		{"Copying binary", func() error {
			return copyFile(exe, binaryDest, 0755)
		}},
		{"Writing service file", func() error {
			content := buildServiceFile(binaryDest, siteUser, siteHome, serverRoot)
			return os.WriteFile(existingServiceFile, []byte(content), 0644)
		}},
		{"Saving sites directory", func() error {
			return saveSitesDir(serverRoot, sitesDir)
		}},
		{"Linking binary into bin dir", func() error {
			return install.LinkIntoBinDir(binDir, "devctl", binaryDest)
		}},
		{"Downloading dev tools", func() error {
			tools.EnsureAllLatest(context.Background(), binDir, os.Stdout)
			return nil
		}},
		{"Configuring shell PATH", func() error {
			u, err := user.Lookup(siteUser)
			if err != nil {
				return fmt.Errorf("lookup user %q: %w", siteUser, err)
			}
			var uid, gid int
			if _, err := fmt.Sscan(u.Uid, &uid); err != nil {
				return fmt.Errorf("parse uid %q: %w", u.Uid, err)
			}
			if _, err := fmt.Sscan(u.Gid, &gid); err != nil {
				return fmt.Errorf("parse gid %q: %w", u.Gid, err)
			}
			return WritePATHSetup(binDir, siteHome, siteUser, uid, gid)
		}},
		{"Running daemon-reload", func() error {
			return systemctl("daemon-reload")
		}},
		{"Enabling service", func() error {
			return systemctl("enable", "devctl")
		}},
		{"Starting service", func() error {
			return systemctl("start", "devctl")
		}},
	}

	total := len(steps)
	for i, s := range steps {
		fmt.Printf("[%d/%d] %s... ", i+1, total, s.label)
		if err := s.fn(); err != nil {
			fmt.Println("✗")
			return fmt.Errorf("step %d (%s): %w", i+1, s.label, err)
		}
		fmt.Println("✓")
	}

	fmt.Println()
	fmt.Print("Waiting for devctl to start")
	if err := waitForActive(5 * time.Second); err != nil {
		fmt.Println()
		return fmt.Errorf("service did not become active within 5s: %w", err)
	}
	fmt.Println(" active ✓")
	fmt.Println()
	fmt.Println("devctl is running. Open http://127.0.0.1:4000")
	fmt.Println()

	// --- Optional: install OpenCode skill ---
	if !*flagYes && isTTY {
		fmt.Print("Install OpenCode CLI skill for devctl? [y/N] ")
		if confirm(r) {
			skillPath, err := cli.DefaultSkillPath()
			if err == nil {
				if err := cli.WriteSkill(skillPath); err != nil {
					fmt.Printf("  warning: could not write skill: %v\n", err)
				} else {
					fmt.Println("  Skill written to " + skillPath)
				}
			}
		}
		fmt.Println()
	}

	return nil
}

// resolveUser prompts the user to confirm or change the detected username.
func resolveUser(flagVal string, skipPrompt bool, r *bufio.Reader) (username, home string, err error) {
	if flagVal != "" {
		return lookupUser(flagVal)
	}

	detected := os.Getenv("SUDO_USER")

	if skipPrompt {
		if detected == "" {
			return "", "", errors.New("--user flag is required in non-interactive mode")
		}
		return lookupUser(detected)
	}

	type option struct {
		label string
		value string
	}
	var opts []option
	if detected != "" {
		opts = append(opts, option{fmt.Sprintf("%s  [detected]", detected), detected})
	}
	opts = append(opts, option{"Enter a different username", ""})

	fmt.Println("Which user's sites directory should devctl manage?")
	for i, o := range opts {
		fmt.Printf("  %d) %s\n", i+1, o.label)
	}
	fmt.Println()

	for {
		fmt.Print("Choice [1]: ")
		raw := readLine(r)
		if raw == "" {
			raw = "1"
		}

		var idx int
		if _, scanErr := fmt.Sscanf(raw, "%d", &idx); scanErr != nil || idx < 1 || idx > len(opts) {
			fmt.Printf("  Please enter a number between 1 and %d.\n", len(opts))
			continue
		}

		chosen := opts[idx-1]
		if chosen.value == "" {
			fmt.Print("  Username: ")
			custom := strings.TrimSpace(readLine(r))
			if custom == "" {
				fmt.Println("  Username cannot be empty.")
				continue
			}
			u, h, lookupErr := lookupUser(custom)
			if lookupErr != nil {
				fmt.Printf("  %v\n", lookupErr)
				continue
			}
			fmt.Println()
			return u, h, nil
		}

		u, h, lookupErr := lookupUser(chosen.value)
		if lookupErr != nil {
			fmt.Printf("  %v\n", lookupErr)
			continue
		}
		fmt.Println()
		return u, h, nil
	}
}

// resolveSitesDir prompts for the directory where sites are stored.
// serviceFile is the path to the existing systemd service file (if any). When
// it contains a DEVCTL_SERVER_ROOT value, its parent directory is used as the
// default instead of ~/sites — this prevents the reinstall bug where the old
// sites directory would be overwritten with the wrong default.
func resolveSitesDir(flagVal, siteHome string, skipPrompt bool, r *bufio.Reader, serviceFile string) (string, error) {
	if flagVal != "" {
		return filepath.Clean(flagVal), nil
	}

	// On reinstall, prefer the sites dir from the existing service file.
	defaultDir := filepath.Join(siteHome, "sites")
	if detected := detectServerRoot(serviceFile); detected != "" {
		defaultDir = filepath.Dir(detected)
	}

	if skipPrompt {
		return defaultDir, nil
	}

	opts := []struct {
		label string
		path  string
	}{
		{defaultDir + "  [default]", defaultDir},
		{"/var/www", "/var/www"},
		{"Enter a custom path", ""},
	}

	fmt.Println("Where are your sites stored?")
	for i, o := range opts {
		fmt.Printf("  %d) %s\n", i+1, o.label)
	}
	fmt.Println()

	for {
		fmt.Print("Choice [1]: ")
		raw := readLine(r)
		if raw == "" {
			raw = "1"
		}

		var idx int
		if _, err := fmt.Sscanf(raw, "%d", &idx); err != nil || idx < 1 || idx > len(opts) {
			fmt.Printf("  Please enter a number between 1 and %d.\n", len(opts))
			continue
		}

		chosen := opts[idx-1]
		if chosen.path == "" {
			fmt.Print("  Path: ")
			custom := strings.TrimSpace(readLine(r))
			if custom == "" {
				fmt.Println("  Path cannot be empty.")
				continue
			}
			fmt.Println()
			return filepath.Clean(custom), nil
		}

		fmt.Println()
		return chosen.path, nil
	}
}

// resolveInstallDir prompts the user to choose or type a binary install directory.
func resolveInstallDir(flagVal, serverRoot string, skipPrompt bool, r *bufio.Reader) (string, error) {
	if flagVal != "" {
		return filepath.Clean(flagVal), nil
	}
	devctlDir := paths.DevctlDir(serverRoot)

	if skipPrompt {
		return devctlDir, nil
	}

	type opt struct {
		label string
		path  string
	}

	// Build deduplicated preset list.
	seen := map[string]bool{}
	var opts []opt
	add := func(label, path string) {
		if !seen[path] {
			seen[path] = true
			opts = append(opts, opt{label, path})
		}
	}
	add(devctlDir+"  [recommended]", devctlDir)
	add("/usr/local/bin", "/usr/local/bin")
	opts = append(opts, opt{"Enter a custom path", ""})

	fmt.Println("Where should the devctl binary be installed?")
	for i, o := range opts {
		fmt.Printf("  %d) %s\n", i+1, o.label)
	}
	fmt.Println()

	for {
		fmt.Print("Choice [1]: ")
		raw := readLine(r)
		if raw == "" {
			raw = "1"
		}

		var idx int
		if _, err := fmt.Sscanf(raw, "%d", &idx); err != nil || idx < 1 || idx > len(opts) {
			fmt.Printf("  Please enter a number between 1 and %d.\n", len(opts))
			continue
		}

		chosen := opts[idx-1]
		if chosen.path == "" {
			fmt.Print("  Path: ")
			custom := strings.TrimSpace(readLine(r))
			if custom == "" {
				fmt.Println("  Path cannot be empty.")
				continue
			}
			fmt.Println()
			return filepath.Clean(custom), nil
		}

		fmt.Println()
		return chosen.path, nil
	}
}

// saveSitesDir opens (or creates) the devctl SQLite DB and persists sites_watch_dir.
func saveSitesDir(serverRoot, sitesDir string) error {
	database, err := db.Open(paths.DBPath(serverRoot))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()

	queries := dbq.New(database)
	return queries.SetSetting(context.Background(), dbq.SetSettingParams{
		Key:   "sites_watch_dir",
		Value: sitesDir,
	})
}

// buildServiceFile generates the systemd unit file content.
func buildServiceFile(binaryPath, siteUser, siteHome, serverRoot string) string {
	return fmt.Sprintf(`[Unit]
Description=devctl — Local PHP Dev Dashboard
After=network.target

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure
RestartSec=5s
Environment=HOME=%s
Environment=DEVCTL_SITE_USER=%s
Environment=DEVCTL_SERVER_ROOT=%s

[Install]
WantedBy=multi-user.target
`, binaryPath, siteHome, siteUser, serverRoot)
}

// waitForActive polls `systemctl is-active devctl` until it returns "active"
// or the deadline is exceeded.
func waitForActive(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("systemctl", "is-active", "devctl").Output()
		if err == nil && strings.TrimSpace(string(out)) == "active" {
			return nil
		}
		fmt.Print(".")
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("timed out waiting for active status")
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func lookupUser(name string) (username, home string, err error) {
	u, err := user.Lookup(name)
	if err != nil {
		return "", "", fmt.Errorf("user %q not found: %w", name, err)
	}
	return u.Username, u.HomeDir, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func confirm(r *bufio.Reader) bool {
	line := strings.TrimSpace(readLine(r))
	return strings.EqualFold(line, "y") || strings.EqualFold(line, "yes")
}

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

// isTerminal reports whether stdin is a real TTY.
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Uninstall is the entry point for `devctl uninstall`. args is os.Args[2:].
func Uninstall(args []string) error {
	fs := flag.NewFlagSet("devctl uninstall", flag.ContinueOnError)
	flagYes := fs.Bool("yes", false, "skip all confirmation prompts")
	flagPurgeServices := fs.Bool("purge-services", false, "remove installed services (Caddy, Valkey, etc.) without prompting")
	fs.SetOutput(os.Stderr)

	if err := fs.Parse(args); err != nil {
		return err
	}

	if os.Getuid() != 0 {
		return errors.New("must be run as root — re-run with: sudo devctl uninstall")
	}

	isTTY := isTerminal()
	if !isTTY && !*flagYes {
		return errors.New("non-interactive mode detected — provide --yes flag for scripted uninstall")
	}

	r := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("devctl uninstaller")
	fmt.Println("━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Read siteHome and serverRoot from the service file early, before we remove anything.
	// These are needed later for service purge. Errors are non-fatal.
	siteHome := readSiteHome()
	serviceFile := filepath.Join(serviceDir, serviceName)
	serverRoot := detectServerRoot(serviceFile)
	if serverRoot == "" && siteHome != "" {
		// Fallback for installs that predate DEVCTL_SERVER_ROOT.
		serverRoot = filepath.Join(siteHome, "sites", "server")
	}

	// Detect the installed binary path from the service file.
	binaryPath := detectBinaryPath(serviceFile)

	// Show what will happen.
	isActive := serviceIsActive()
	isEnabled := serviceIsEnabled()

	fmt.Println("The following steps will be performed:")
	if isActive {
		fmt.Println("  1. systemctl stop devctl")
	}
	if isEnabled {
		fmt.Println("  2. systemctl disable devctl")
	}
	fmt.Printf("  3. Remove service file  %s\n", serviceFile)
	fmt.Println()

	if !*flagYes {
		fmt.Print("Proceed with service removal? [y/N] ")
		if !confirm(r) {
			fmt.Println("Aborted.")
			return nil
		}
		fmt.Println()
	}

	// Remove devctl PATH block from shell config files and any legacy profile.d script.
	if siteHome != "" {
		RemovePATHSetup(siteHome)
	}

	// Stop and disable the service.
	if isActive {
		fmt.Print("Stopping service... ")
		if err := systemctl("stop", "devctl"); err != nil {
			fmt.Println("✗")
			return fmt.Errorf("stop devctl: %w", err)
		}
		fmt.Println("✓")
	}
	if isEnabled {
		fmt.Print("Disabling service... ")
		if err := systemctl("disable", "devctl"); err != nil {
			fmt.Println("✗")
			return fmt.Errorf("disable devctl: %w", err)
		}
		fmt.Println("✓")
	}

	fmt.Print("Removing service file... ")
	if fileExists(serviceFile) {
		if err := os.Remove(serviceFile); err != nil {
			fmt.Println("✗")
			return fmt.Errorf("remove service file: %w", err)
		}
	}
	fmt.Println("✓")

	fmt.Print("Running daemon-reload... ")
	if err := systemctl("daemon-reload"); err != nil {
		fmt.Println("✗")
		return fmt.Errorf("daemon-reload: %w", err)
	}
	fmt.Println("✓")

	fmt.Println()

	// --- Optional: remove binary ---
	if binaryPath != "" && fileExists(binaryPath) {
		fmt.Printf("Remove binary (%s)? [y/N] ", binaryPath)
		if *flagYes || confirm(r) {
			fmt.Print("Removing binary... ")
			if err := os.Remove(binaryPath); err != nil {
				fmt.Println("✗")
				fmt.Printf("  warning: could not remove binary: %v\n", err)
			} else {
				fmt.Println("✓")
			}
			// Also remove the BinDir symlink if serverRoot is known.
			if serverRoot != "" {
				install.UnlinkFromBinDir(paths.BinDir(serverRoot), "devctl")
			}
		} else {
			fmt.Printf("  Binary left in place at %s\n", binaryPath)
		}
		fmt.Println()
	}

	// --- Optional: remove config directory ---
	devctlDir := paths.DevctlDir(serverRoot)
	if serverRoot != "" && fileExists(devctlDir) {
		fmt.Printf("Remove devctl data directory (%s)? [y/N] ", devctlDir)
		fmt.Println()
		fmt.Println("  This contains the database and settings. The sites directory will NOT be touched.")
		fmt.Print("  Confirm removal? [y/N] ")
		if *flagYes || confirm(r) {
			fmt.Print("Removing devctl data directory... ")
			if err := os.RemoveAll(devctlDir); err != nil {
				fmt.Println("✗")
				fmt.Printf("  warning: could not remove devctl data dir: %v\n", err)
			} else {
				fmt.Println("✓")
			}
		} else {
			fmt.Printf("  Data directory left in place at %s\n", devctlDir)
		}
		fmt.Println()
	}

	// --- Optional: remove installed services ---
	purgeServices := *flagYes || *flagPurgeServices
	if serverRoot != "" {
		if !purgeServices {
			fmt.Println("Remove installed services (Caddy, Valkey, Mailpit, etc.)?")
			fmt.Printf("  This will stop and delete all service binaries under %s.\n", serverRoot)
			fmt.Println("  PHP versions and any APT-installed services (PostgreSQL, MySQL) will also be removed.")
			fmt.Print("  Proceed? [y/N] ")
			purgeServices = confirm(r)
			fmt.Println()
		}

		if purgeServices {
			ctx := context.Background()
			purgeInstalledServices(ctx, serverRoot, siteHome, os.Stdout)
		}
	}

	fmt.Println("devctl has been uninstalled.")
	fmt.Println()
	return nil
}

// readSiteHome resolves the site owner's home directory by looking up the
// SUDO_USER environment variable. This is always set when running via
// `sudo devctl uninstall`. Returns an empty string if it cannot be determined.
func readSiteHome() string {
	name := os.Getenv("SUDO_USER")
	if name == "" {
		return ""
	}
	u, err := user.Lookup(name)
	if err != nil {
		return ""
	}
	return u.HomeDir
}

// purgeInstalledServices removes all devctl-managed service binaries that are
// currently installed. It stops supervised processes (best-effort — they are
// already stopped because devctl itself has been stopped) and removes their
// data directories. APT-installed services (PostgreSQL, MySQL) are fully purged
// via apt-get.
func purgeInstalledServices(ctx context.Context, serverRoot, siteHome string, w io.Writer) {
	supervisor := services.NewSupervisor(serverRoot)
	// siteManager and queries are not available here; pass nil — the installers
	// that use them (Reverb, Meilisearch, Typesense) will emit warnings for the
	// site-cleanup step but will still remove their directories.
	registry, _ := install.NewRegistry(nil, nil, supervisor, "", serverRoot, siteHome)

	// Ordered list — stop Caddy first so it releases ports before we remove it.
	serviceOrder := []string{"caddy", "redis", "mailpit", "meilisearch", "typesense", "reverb", "postgres", "mysql"}

	for _, id := range serviceOrder {
		installer, ok := registry[id]
		if !ok || !installer.IsInstalled() {
			continue
		}
		fmt.Fprintf(w, "Removing %s... ", id)
		if err := installer.PurgeW(ctx, w, false); err != nil {
			fmt.Fprintf(w, "warning: %v\n", err)
		} else {
			fmt.Fprintf(w, "✓\n")
		}
	}

	// PHP versions — remove all installed versions and the global symlink.
	phpVersions, err := php.InstalledVersions(serverRoot)
	if err != nil {
		fmt.Fprintf(w, "warning: list PHP versions: %v\n", err)
		return
	}
	for _, v := range phpVersions {
		fmt.Fprintf(w, "Removing PHP %s... ", v.Version)
		if err := php.Uninstall(ctx, v.Version, serverRoot); err != nil {
			fmt.Fprintf(w, "warning: %v\n", err)
		} else {
			fmt.Fprintf(w, "✓\n")
		}
	}
}

// detectBinaryPath reads ExecStart= from the service file to find where the binary lives.
// It returns only the first field (the executable path), stripping any arguments such as
// the "daemon" subcommand that was added when support for CLI subcommands was introduced.
func detectBinaryPath(serviceFile string) string {
	data, err := os.ReadFile(serviceFile)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ExecStart=") {
			val := strings.TrimPrefix(line, "ExecStart=")
			// Return only the binary path — strip any subcommand/args that follow.
			return strings.Fields(val)[0]
		}
	}
	return ""
}

// detectServerRoot reads DEVCTL_SERVER_ROOT from an Environment= line in the
// service file. Returns an empty string if not found (pre-DEVCTL_SERVER_ROOT installs).
func detectServerRoot(serviceFile string) string {
	data, err := os.ReadFile(serviceFile)
	if err != nil {
		return ""
	}
	prefix := "Environment=DEVCTL_SERVER_ROOT="
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}

// serviceIsActive returns true if `systemctl is-active devctl` reports "active".
func serviceIsActive() bool {
	out, err := exec.Command("systemctl", "is-active", "devctl").Output()
	return err == nil && strings.TrimSpace(string(out)) == "active"
}

// serviceIsEnabled returns true if `systemctl is-enabled devctl` reports "enabled".
func serviceIsEnabled() bool {
	out, err := exec.Command("systemctl", "is-enabled", "devctl").Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(out))
	return s == "enabled" || s == "enabled-runtime"
}

// ---------------------------------------------------------------------------
// Shell config file PATH helpers
// ---------------------------------------------------------------------------

// pathBlockStart and pathBlockEnd are the sentinel comments that bracket the
// devctl-managed PATH block in shell config files. Using a matching pair (open
// + close) lets us safely replace the block in-place on re-runs (e.g. when the
// bin dir changes) without duplicating it.
const pathBlockStart = "# >>> devctl PATH >>>"
const pathBlockEnd = "# <<< devctl PATH <<<"

// pathBlock returns the full idempotent PATH block for a given bin directory.
func pathBlock(binDir string) string {
	return pathBlockStart + "\n" +
		"# Managed by devctl — do not edit manually\n" +
		`export PATH="` + binDir + `:$PATH"` + "\n" +
		pathBlockEnd + "\n"
}

// getUserShell returns the base name of the login shell for the given username
// (e.g. "zsh", "bash", "sh"). It tries `getent passwd` first (Linux) and falls
// back to parsing /etc/passwd directly (macOS, containers without getent).
// Returns an empty string if the shell cannot be determined.
func getUserShell(username string) string {
	// Try getent first (available on Linux, not always on macOS).
	if out, err := exec.Command("getent", "passwd", username).Output(); err == nil {
		return shellFromPasswdLine(strings.TrimSpace(string(out)))
	}

	// Fallback: scan /etc/passwd directly.
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	prefix := username + ":"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return shellFromPasswdLine(line)
		}
	}
	return ""
}

// shellFromPasswdLine extracts the base shell name from a colon-delimited
// passwd line (field 7, 0-indexed field 6).
func shellFromPasswdLine(line string) string {
	parts := strings.Split(line, ":")
	if len(parts) < 7 {
		return ""
	}
	return filepath.Base(strings.TrimSpace(parts[6]))
}

// shellTargets returns the list of shell config files that should receive the
// PATH block, based on the user's detected login shell.
//
// Rules:
//   - zsh  → always write ~/.zshenv (create if absent); that file is sourced
//     for every zsh invocation including non-interactive and VSCode terminals.
//   - bash → always write ~/.bashrc (create if absent); also write
//     ~/.bash_profile and ~/.profile if they already exist.
//   - sh / other → write ~/.profile only if it already exists.
//
// The bool in each pair indicates whether the file should be created when it
// does not already exist.
type shellTarget struct {
	path       string
	mustCreate bool
}

func shellTargets(shell, siteHome string) []shellTarget {
	switch shell {
	case "zsh":
		targets := []shellTarget{
			{filepath.Join(siteHome, ".zshenv"), true},
		}
		return targets
	case "bash":
		targets := []shellTarget{
			{filepath.Join(siteHome, ".bashrc"), true},
		}
		for _, f := range []string{".bash_profile", ".profile"} {
			p := filepath.Join(siteHome, f)
			if fileExists(p) {
				targets = append(targets, shellTarget{p, false})
			}
		}
		return targets
	default:
		p := filepath.Join(siteHome, ".profile")
		if fileExists(p) {
			return []shellTarget{{p, false}}
		}
		return nil
	}
}

// WritePATHSetup writes the devctl PATH block to the appropriate shell config
// files for the given user. It detects the user's login shell and only touches
// files that belong to that shell. The operation is idempotent — running it
// again with the same (or a different) binDir replaces the existing block.
//
// uid and gid are the numeric owner to set on any file written or created.
// Pass the target user's uid/gid so that files created while running as root
// are owned by the user, not root.
//
// This is exported so it can also be invoked from the path-setup sub-command.
func WritePATHSetup(binDir, siteHome, username string, uid, gid int) error {
	shell := getUserShell(username)

	targets := shellTargets(shell, siteHome)
	if len(targets) == 0 {
		// Unknown or unsupported shell — nothing to do, not an error.
		return nil
	}

	block := "\n" + pathBlock(binDir)
	for _, t := range targets {
		if err := updateShellFile(t.path, block, t.mustCreate, uid, gid); err != nil {
			return err
		}
	}
	return nil
}

// RemovePATHSetup removes the devctl PATH block from all shell config files it
// may have written to. It also removes the legacy /etc/profile.d/devctl.sh if
// it still exists (migration for installs that predate this mechanism).
//
// Exported so it can be called from the path-setup --remove sub-command.
func RemovePATHSetup(siteHome string) {
	// Remove legacy profile.d script if it exists.
	_ = os.Remove("/etc/profile.d/devctl.sh")

	// Candidates covers every file we may have ever written to.
	candidates := []string{
		filepath.Join(siteHome, ".zshenv"),
		filepath.Join(siteHome, ".bashrc"),
		filepath.Join(siteHome, ".bash_profile"),
		filepath.Join(siteHome, ".profile"),
	}
	for _, f := range candidates {
		removeBlockFromFile(f)
	}
}

// updateShellFile writes or replaces the devctl PATH block in a single shell
// config file.
//
//   - If the file contains the sentinel markers, the block between them
//     (inclusive) is replaced — this handles bin-dir changes on re-install.
//   - If the markers are absent, the block is appended.
//   - If the file does not exist and mustCreate is true, the file is created
//     containing only the block.
//   - If the file does not exist and mustCreate is false, the call is a no-op.
func updateShellFile(path, block string, mustCreate bool, uid, gid int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if !mustCreate {
				return nil
			}
			// Create the file with just the block (strip leading newline).
			if err := os.WriteFile(path, []byte(strings.TrimLeft(block, "\n")+"\n"), 0644); err != nil {
				return err
			}
			return os.Lchown(path, uid, gid)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	content := string(data)

	startIdx := strings.Index(content, pathBlockStart)
	endIdx := strings.Index(content, pathBlockEnd)

	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		// Replace the existing block in-place.
		// Preserve everything before the start marker and after the end marker.
		before := content[:startIdx]
		after := content[endIdx+len(pathBlockEnd):]
		// Trim any leading newline from after (the newline that preceded the start marker).
		after = strings.TrimLeft(after, "\n")
		// Re-assemble: keep trailing newline on before, insert block, then after.
		newContent := strings.TrimRight(before, "\n") + "\n" + block
		if after != "" {
			newContent += "\n" + after
		}
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			return err
		}
		return os.Lchown(path, uid, gid)
	}

	// No existing block — append.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	_, writeErr := f.WriteString(block)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write %s: %w", path, writeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Lchown(path, uid, gid)
}

// removeBlockFromFile removes the sentinel-delimited PATH block from a single
// file. The file is rewritten without the block (and the blank line that
// precedes it, if any). If the file does not exist or contains no block, it is
// left untouched.
func removeBlockFromFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)

	startIdx := strings.Index(content, pathBlockStart)
	endIdx := strings.Index(content, pathBlockEnd)
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return
	}

	before := content[:startIdx]
	after := content[endIdx+len(pathBlockEnd):]

	// Trim the trailing newline that we added before the start marker.
	before = strings.TrimRight(before, "\n")
	// Trim the leading newline after the end marker.
	after = strings.TrimLeft(after, "\n")

	var newContent string
	if before != "" && after != "" {
		newContent = before + "\n" + after
	} else if before != "" {
		newContent = before + "\n"
	} else {
		newContent = after
	}

	_ = os.WriteFile(path, []byte(newContent), 0644)
}
