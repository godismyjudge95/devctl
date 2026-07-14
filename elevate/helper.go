// Package elevate implements one-shot privileged OS setup for devctl.
//
// Privileged mutations run only via `devctl helper <op>` (must be euid 0).
// Orchestration is `sudo devctl elevate|unelevate [target]`.
package elevate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Allowed apt packages that may be installed via helper apt-install.
var allowedAptPackages = map[string]struct{}{
	"libnss3-tools":  {},
	"libreadline-dev": {},
	"libnuma1":       {},
}

// pinnedPATH is used for all helper subprocesses.
const pinnedPATH = "/usr/sbin:/usr/bin:/sbin:/bin"

// ServiceUnitPath is the systemd system unit path for devctl.
const ServiceUnitPath = "/etc/systemd/system/devctl.service"

// ResolvedDropinDir / ResolvedDropinFile are the systemd-resolved drop-in paths.
const (
	ResolvedDropinDir  = "/etc/systemd/resolved.conf.d"
	ResolvedDropinFile = "/etc/systemd/resolved.conf.d/99-devctl-dns.conf"
)

// CACertName is the filename used under the distro CA anchors directory.
const CACertName = "devctl-local-ca.crt"

// RunHelper dispatches a frozen privileged operation. Must be called as root.
// args is everything after "helper" (e.g. ["install-resolver", "--port", "5354", "--tld", "test"]).
func RunHelper(args []string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("devctl helper: must run as root (euid 0)")
	}
	if len(args) == 0 {
		return fmt.Errorf("devctl helper: missing operation")
	}
	op := args[0]
	rest := args[1:]

	switch op {
	case "install-resolver":
		return helperInstallResolver(rest)
	case "uninstall-resolver":
		return helperUninstallResolver()
	case "install-ca":
		return helperInstallCA(rest)
	case "uninstall-ca":
		return helperUninstallCA(rest)
	case "write-unit":
		return helperWriteUnit(rest)
	case "chown-tree":
		return helperChownTree(rest)
	case "apt-install":
		return helperAptInstall(rest)
	case "systemctl":
		return helperSystemctl(rest)
	default:
		return fmt.Errorf("devctl helper: unknown operation %q", op)
	}
}

// runPinned runs a command with cleared env (except PATH and DEBIAN_FRONTEND)
// and working directory /.
func runPinned(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = "/"
	cmd.Env = []string{
		"PATH=" + pinnedPATH,
		"DEBIAN_FRONTEND=noninteractive",
		"LC_ALL=C",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w\n%s", name, args, err, string(out))
	}
	return nil
}

// writeFileAtomic writes content to path via a temp sibling + rename.
func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".devctl-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// parseFlags is a minimal flag parser for helper ops: --key value pairs.
func parseFlags(args []string) (map[string]string, []string, error) {
	flags := map[string]string{}
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			positional = append(positional, a)
			continue
		}
		key := strings.TrimPrefix(a, "--")
		if key == "" {
			return nil, nil, fmt.Errorf("empty flag")
		}
		if i+1 >= len(args) {
			return nil, nil, fmt.Errorf("flag --%s requires a value", key)
		}
		i++
		flags[key] = args[i]
	}
	return flags, positional, nil
}

// requireRoot is an alias for documentation at call sites.
func requireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root")
	}
	return nil
}
