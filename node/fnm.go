package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"syscall"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/tools"
)

// FnmPath is the fnm helper binary.
func FnmPath(serverRoot string) string {
	return filepath.Join(paths.BinDir(serverRoot), tools.FNM.Name)
}

// Fnm returns an fnm command that works on the devctl Node directory.
func Fnm(ctx context.Context, serverRoot string, args ...string) *exec.Cmd {
	full := append([]string{"--fnm-dir", paths.NodeDir(serverRoot)}, args...)
	return exec.CommandContext(ctx, FnmPath(serverRoot), full...)
}

// Installed lists the Node versions in the devctl fnm directory, newest first.
func Installed(serverRoot string) []Version {
	entries, err := os.ReadDir(filepath.Join(paths.NodeDir(serverRoot), "node-versions"))
	if err != nil {
		return nil
	}
	var out []Version
	for _, e := range entries {
		if v, ok := ParseVersion(e.Name()); ok && e.IsDir() {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, func(a, b Version) int {
		switch {
		case b.less(a):
			return -1
		case a.less(b):
			return 1
		}
		return 0
	})
	return out
}

// Command returns the argv that runs args (for example "npm", "run", "dev")
// with the Node version the project in dir asks for. Workers and the shims
// both use it.
func Command(serverRoot, dir string, args ...string) ([]string, error) {
	spec := Find(dir)
	v, ok := Resolve(spec.Version, Installed(serverRoot))
	switch {
	case ok:
	case spec.Version == "":
		return nil, errors.New("no Node version is installed. Run: devctl node:install lts")
	case !Valid(spec.Version):
		return nil, fmt.Errorf("unsupported Node version %q in %s", spec.Version, spec.File)
	default:
		return nil, fmt.Errorf("Node %s (from %s) is not installed. Run: devctl node:install <version>", spec.Version, spec.File)
	}
	fnm := FnmPath(serverRoot)
	if _, err := os.Stat(fnm); err != nil {
		return nil, errors.New("fnm is not installed. Run: devctl helpers:install fnm")
	}
	argv := []string{fnm, "--fnm-dir", paths.NodeDir(serverRoot), "exec", "--using", v.String(), "--"}
	return append(argv, args...), nil
}

var installVersion = regexp.MustCompile(`^(v?\d+(\.\d+){0,2}|lts/[a-z]+)$`)

// InstallArgs turns a node:install argument ("22", "20.11.1", "lts",
// "lts/iron", "latest") into fnm install arguments.
func InstallArgs(want string) ([]string, error) {
	switch {
	case want == "lts":
		return []string{"install", "--lts", "--progress=never"}, nil
	case want == "latest":
		return []string{"install", "--latest", "--progress=never"}, nil
	case installVersion.MatchString(want):
		return []string{"install", want, "--progress=never"}, nil
	}
	return nil, fmt.Errorf("invalid Node version %q: use a version like 22 or 20.11.1, lts, lts/<name>, or latest", want)
}

// shimBins are the commands devctl puts on PATH for the Node of each site.
var shimBins = []string{"node", "npm", "npx"}

func shimScript(devctl, serverRoot, bin string) string {
	return "#!/bin/sh\n" +
		"exec " + strconv.Quote(devctl) + " node-shim " + strconv.Quote(serverRoot) + " " + bin + " \"$@\"\n"
}

// EnsureShims writes the node, npm, and npx shims into the shared bin dir
// while at least one Node version is installed, and removes them otherwise so
// a system Node on PATH keeps working. The shims call the running devctl
// binary.
func EnsureShims(serverRoot string) error {
	binDir := paths.BinDir(serverRoot)
	keep := len(Installed(serverRoot)) > 0
	devctl, err := os.Executable()
	if err != nil {
		return err
	}
	for _, bin := range shimBins {
		path := filepath.Join(binDir, bin)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if !keep {
			continue
		}
		if err := os.MkdirAll(binDir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(shimScript(devctl, serverRoot, bin)), 0755); err != nil {
			return fmt.Errorf("write %s shim: %w", bin, err)
		}
	}
	return nil
}

// RunShim is the body of the shims: devctl node-shim <server-root> <bin> [args...].
// It replaces the process with fnm exec for the project in the cwd.
func RunShim(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: devctl node-shim <server-root> <bin> [args...]")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	argv, err := Command(args[0], cwd, args[1:]...)
	if err != nil {
		return err
	}
	return syscall.Exec(argv[0], argv, os.Environ())
}
