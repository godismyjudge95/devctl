package node

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/paths"
)

// The shims call the running binary with "node-shim". In tests that binary is
// the test binary, so it plays the devctl role here.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "node-shim" {
		if err := RunShim(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// fakeServerRoot makes a server root with the given Node versions installed
// and an fnm stand-in that prints its arguments.
func fakeServerRoot(t *testing.T, versions ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, v := range versions {
		if err := os.MkdirAll(filepath.Join(paths.NodeDir(root), "node-versions", v, "installation", "bin"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(paths.NodeDir(root), "node-versions", ".downloads", "x"), "")
	fnm := FnmPath(root)
	writeFile(t, fnm, "#!/bin/sh\necho \"$@\"\n")
	if err := os.Chmod(fnm, 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInstalled_NewestFirst(t *testing.T) {
	root := fakeServerRoot(t, "v18.19.1", "v22.11.0", "v20.11.1")
	var got []string
	for _, v := range Installed(root) {
		got = append(got, v.String())
	}
	want := []string{"v22.11.0", "v20.11.1", "v18.19.1"}
	if !slices.Equal(got, want) {
		t.Fatalf("Installed = %v, want %v", got, want)
	}
}

func TestInstalled_NoDir(t *testing.T) {
	if got := Installed(t.TempDir()); len(got) != 0 {
		t.Fatalf("Installed = %v, want none", got)
	}
}

func TestCommand_UsesProjectVersion(t *testing.T) {
	root := fakeServerRoot(t, "v20.11.1", "v22.11.0")
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "package.json"), `{"engines":{"node":"^20.10"}}`)

	got, err := Command(root, project, "npm", "run", "dev")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := []string{FnmPath(root), "--fnm-dir", paths.NodeDir(root), "exec", "--using", "v20.11.1", "--", "npm", "run", "dev"}
	if !slices.Equal(got, want) {
		t.Fatalf("Command =\n  %v\nwant\n  %v", got, want)
	}
}

func TestCommand_DefaultWithoutVersionFile(t *testing.T) {
	root := fakeServerRoot(t, "v20.11.1", "v22.11.0", "v23.3.0")
	got, err := Command(root, t.TempDir(), "node", "-v")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if !slices.Contains(got, "v22.11.0") {
		t.Fatalf("Command = %v, want the newest LTS v22.11.0", got)
	}
}

func TestCommand_Errors(t *testing.T) {
	project := t.TempDir()
	writeFile(t, filepath.Join(project, ".nvmrc"), "18\n")

	if _, err := Command(fakeServerRoot(t), t.TempDir(), "node"); err == nil || !strings.Contains(err.Error(), "node:install") {
		t.Errorf("nothing installed: err = %v, want a node:install hint", err)
	}
	if _, err := Command(fakeServerRoot(t, "v20.11.1"), project, "node"); err == nil || !strings.Contains(err.Error(), "Node 18") {
		t.Errorf("18 not installed: err = %v, want it to name Node 18", err)
	}
	noFnm := fakeServerRoot(t, "v18.19.1")
	if err := os.Remove(FnmPath(noFnm)); err != nil {
		t.Fatal(err)
	}
	if _, err := Command(noFnm, project, "node"); err == nil || !strings.Contains(err.Error(), "helpers:install fnm") {
		t.Errorf("fnm missing: err = %v, want a helpers:install fnm hint", err)
	}
}

func TestInstallArgs(t *testing.T) {
	ok := map[string][]string{
		"22":       {"install", "22", "--progress=never"},
		"20.11.1":  {"install", "20.11.1", "--progress=never"},
		"v20.11":   {"install", "v20.11", "--progress=never"},
		"lts/iron": {"install", "lts/iron", "--progress=never"},
		"lts":      {"install", "--lts", "--progress=never"},
		"latest":   {"install", "--latest", "--progress=never"},
	}
	for in, want := range ok {
		got, err := InstallArgs(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("InstallArgs(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "--help", ">=20", "20; rm -rf /", "lts/*", "22.1.2.3"} {
		if _, err := InstallArgs(in); err == nil {
			t.Errorf("InstallArgs(%q) succeeded, want error", in)
		}
	}
}

func TestEnsureShims_WritesAndRemoves(t *testing.T) {
	root := fakeServerRoot(t, "v22.11.0")
	if err := EnsureShims(root); err != nil {
		t.Fatalf("EnsureShims: %v", err)
	}
	exe, _ := os.Executable()
	for _, bin := range []string{"node", "npm", "npx"} {
		path := filepath.Join(paths.BinDir(root), bin)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s shim missing: %v", bin, err)
		}
		if info.Mode()&0111 == 0 {
			t.Errorf("%s shim not executable: %v", bin, info.Mode())
		}
		b, _ := os.ReadFile(path)
		if !strings.HasPrefix(string(b), "#!/bin/sh\n") || !strings.Contains(string(b), exe) || !strings.Contains(string(b), " node-shim ") {
			t.Errorf("%s shim does not call this devctl binary:\n%s", bin, b)
		}
	}

	if err := os.RemoveAll(filepath.Join(paths.NodeDir(root), "node-versions", "v22.11.0")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureShims(root); err != nil {
		t.Fatalf("EnsureShims after uninstall: %v", err)
	}
	for _, bin := range []string{"node", "npm", "npx"} {
		if _, err := os.Stat(filepath.Join(paths.BinDir(root), bin)); !os.IsNotExist(err) {
			t.Errorf("%s shim still present with no Node installed (err %v)", bin, err)
		}
	}
	if _, err := os.Stat(FnmPath(root)); err != nil {
		t.Errorf("EnsureShims removed fnm: %v", err)
	}
}

func TestShim_RunsFnmWithProjectVersion(t *testing.T) {
	root := fakeServerRoot(t, "v18.19.1", "v20.11.1")
	if err := EnsureShims(root); err != nil {
		t.Fatalf("EnsureShims: %v", err)
	}
	project := t.TempDir()
	writeFile(t, filepath.Join(project, ".nvmrc"), "18\n")
	sub := filepath.Join(project, "resources", "js")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(filepath.Join(paths.BinDir(root), "npm"), "run", "dev", "--", "--host")
	cmd.Dir = sub
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("npm shim: %v\n%s", err, out)
	}
	want := "--fnm-dir " + paths.NodeDir(root) + " exec --using v18.19.1 -- npm run dev -- --host"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("shim ran fnm with\n  %s\nwant\n  %s", got, want)
	}
}

func TestShim_FailsWithHintWhenVersionMissing(t *testing.T) {
	root := fakeServerRoot(t, "v20.11.1")
	if err := EnsureShims(root); err != nil {
		t.Fatalf("EnsureShims: %v", err)
	}
	project := t.TempDir()
	writeFile(t, filepath.Join(project, ".node-version"), "16\n")

	cmd := exec.Command(filepath.Join(paths.BinDir(root), "node"), "-v")
	cmd.Dir = project
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("node shim succeeded with Node 16 missing:\n%s", out)
	}
	if !strings.Contains(string(out), "Node 16") || !strings.Contains(string(out), "node:install") {
		t.Fatalf("node shim error = %q, want it to name Node 16 and node:install", out)
	}
}
