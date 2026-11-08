package workers

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/node"
	"github.com/danielgormly/devctl/paths"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestTypesMatchKinds(t *testing.T) {
	if len(Types) != len(kinds) {
		t.Fatalf("Types has %d entries, kinds has %d", len(Types), len(kinds))
	}
	for _, typ := range Types {
		if _, ok := kinds[typ]; !ok {
			t.Errorf("type %q has no kind", typ)
		}
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		typ     Type
		command string
		ok      bool
	}{
		{"queue", "", true},
		{"horizon", "", true},
		{"schedule", "", true},
		{"vite", "", true},
		{"custom", "php artisan reverb:start", true},
		{"custom", "  ", false},
		{"queue", "--queue=high", false},
		{"vite", "npm run watch", false},
		{"cron", "", false},
	}
	for _, c := range cases {
		err := validate(c.typ, c.command)
		if c.ok && err != nil {
			t.Errorf("validate(%q, %q) = %v, want nil", c.typ, c.command, err)
		}
		if !c.ok && !errors.Is(err, ErrInvalid) {
			t.Errorf("validate(%q, %q) = %v, want ErrInvalid", c.typ, c.command, err)
		}
	}
}

func TestArgv_PHPTypesRunTheSitePHPWrapper(t *testing.T) {
	serverRoot := t.TempDir()
	php := filepath.Join(paths.BinDir(serverRoot), "php8.3")
	touch(t, php)
	site := t.TempDir()
	touch(t, filepath.Join(site, "artisan"))

	want := map[Type][]string{
		"queue":    {php, "artisan", "queue:work"},
		"horizon":  {php, "artisan", "horizon"},
		"schedule": {php, "artisan", "schedule:work"},
	}
	for typ, w := range want {
		got, err := argv(serverRoot, site, "8.3", typ, "")
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		if !slices.Equal(got, w) {
			t.Errorf("%s argv = %v, want %v", typ, got, w)
		}
	}
}

func TestArgv_PHPErrors(t *testing.T) {
	serverRoot := t.TempDir()
	touch(t, filepath.Join(paths.BinDir(serverRoot), "php8.3"))
	site := t.TempDir()

	if _, err := argv(serverRoot, site, "8.2", "queue", ""); err == nil || !strings.Contains(err.Error(), "PHP 8.2 is not installed") {
		t.Errorf("missing PHP: err = %v", err)
	}
	if _, err := argv(serverRoot, site, "8.3", "queue", ""); err == nil || !strings.Contains(err.Error(), "artisan") {
		t.Errorf("no artisan: err = %v", err)
	}
}

func TestArgv_ViteRunsNpmWithTheSiteNode(t *testing.T) {
	serverRoot := t.TempDir()
	touch(t, node.FnmPath(serverRoot))
	if err := os.MkdirAll(filepath.Join(paths.NodeDir(serverRoot), "node-versions", "v22.11.0"), 0755); err != nil {
		t.Fatal(err)
	}
	site := t.TempDir()
	if err := os.WriteFile(filepath.Join(site, ".nvmrc"), []byte("22\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := argv(serverRoot, site, "8.3", "vite", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{node.FnmPath(serverRoot), "--fnm-dir", paths.NodeDir(serverRoot), "exec", "--using", "v22.11.0", "--", "npm", "run", "dev"}
	if !slices.Equal(got, want) {
		t.Fatalf("vite argv = %v, want %v", got, want)
	}

	if _, err := argv(t.TempDir(), site, "8.3", "vite", ""); err == nil {
		t.Error("vite with no Node installed: want an error")
	}
}

func TestArgv_CustomRunsThroughShell(t *testing.T) {
	got, err := argv(t.TempDir(), t.TempDir(), "8.3", "custom", "echo hi && sleep 1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/bin/sh", "-c", "echo hi && sleep 1"}; !slices.Equal(got, want) {
		t.Fatalf("custom argv = %v, want %v", got, want)
	}
}

func TestDisplay(t *testing.T) {
	cases := map[string]string{
		display("queue", "8.4", ""):        "php8.4 artisan queue:work",
		display("schedule", "8.3", ""):     "php8.3 artisan schedule:work",
		display("vite", "8.4", ""):         "npm run dev",
		display("custom", "8.4", "make w"): "make w",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("display = %q, want %q", got, want)
		}
	}
}
