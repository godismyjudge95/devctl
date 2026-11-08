//go:build integration

package apitest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type capturedDump struct {
	File  *string `json:"file"`
	Nodes string  `json:"nodes"`
}

func setDumpToPage(t *testing.T, on bool) {
	t.Helper()
	body, status := httpPut(t, "/api/settings", map[string]string{"dump_to_page": fmt.Sprint(on)})
	if status != 200 {
		t.Fatalf("PUT dump_to_page=%v: %d %s", on, status, body)
	}
}

// TestDumpToPageSetting_RewritesPrepend verifies that the setting reaches the
// prepend.php file every PHP process loads, without a devctl restart.
func TestDumpToPageSetting_RewritesPrepend(t *testing.T) {
	prepend := filepath.Join(phpTestServerRoot(), "devctl", "prepend.php")
	t.Cleanup(func() { setDumpToPage(t, false) })

	for _, on := range []bool{true, false} {
		setDumpToPage(t, on)
		data, err := os.ReadFile(prepend)
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("return %v; // devctl:dump_to_page", on)
		if !strings.Contains(string(data), want) {
			t.Errorf("dump_to_page=%v: %s does not contain %q", on, prepend, want)
		}
		got := decodeJSON[map[string]string](t, httpGet(t, "/api/settings/resolved"))["dump_to_page"]
		if got != fmt.Sprint(on) {
			t.Errorf("resolved dump_to_page = %q, want %v", got, on)
		}
	}

	if _, status := httpPut(t, "/api/settings", map[string]string{"dump_to_page": "maybe"}); status != 400 {
		t.Errorf("PUT dump_to_page=maybe: status %d, want 400", status)
	}
}

// TestDumps_CapturedFromPHPCLI runs the devctl php wrapper against a project
// with the real symfony/var-dumper and checks that dump(), php_dd(),
// VarDumper::dump() (after a Laravel-style setHandler) and a child process
// started with PHP_BINARY all reach the Dumps store, and that nothing prints
// unless dump_to_page is on.
func TestDumps_CapturedFromPHPCLI(t *testing.T) {
	phpBin, composer := requireRealPHP(t)
	dir := t.TempDir()

	cmd := exec.Command(phpBin, composer, "require", "--no-interaction", "--no-progress", "symfony/var-dumper")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSER_HOME="+filepath.Join(dir, ".composer"), "COMPOSER_ALLOW_SUPERUSER=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("composer require symfony/var-dumper failed (no network in the test machine?): %v\n%s", err, out)
	}

	nonce := fmt.Sprint(time.Now().UnixNano())
	mark := func(name string) string { return "devctl-" + name + "-" + nonce }

	writeFile(t, filepath.Join(dir, "child.php"), `<?php
require __DIR__.'/vendor/autoload.php';
dump('`+mark("child")+`');
`)
	script := filepath.Join(dir, "run.php")
	writeFile(t, script, `<?php
require __DIR__.'/vendor/autoload.php';
use Symfony\Component\VarDumper\VarDumper;
VarDumper::setHandler(function ($v) { echo "REPLACED-HANDLER\n"; });
dump('`+mark("dump")+`');
VarDumper::dump('`+mark("vardumper")+`');
echo dump('`+mark("return")+`') === '`+mark("return")+`' ? "RETURN-OK\n" : "RETURN-BAD\n";
php_dd('`+mark("alias")+`');
passthru(PHP_BINARY.' '.escapeshellarg(__DIR__.'/child.php'));
`)

	out := runPHP(t, phpBin, script)
	if strings.Contains(out, "REPLACED-HANDLER") {
		t.Errorf("a later VarDumper::setHandler() took over the dumps:\n%s", out)
	}
	if !strings.Contains(out, "RETURN-OK") {
		t.Errorf("dump() did not return its argument:\n%s", out)
	}
	if strings.Contains(out, mark("dump")) {
		t.Errorf("dump printed with dump_to_page off:\n%s", out)
	}

	for _, name := range []string{"dump", "vardumper", "return", "alias", "child"} {
		d := waitForDump(t, mark(name))
		want := "run.php"
		if name == "child" {
			want = "child.php"
		}
		if d.File == nil || filepath.Base(*d.File) != want {
			t.Errorf("%s: source file = %v, want %s", name, d.File, want)
		}
	}

	setDumpToPage(t, true)
	t.Cleanup(func() { setDumpToPage(t, false) })
	page := filepath.Join(dir, "page.php")
	writeFile(t, page, `<?php
require __DIR__.'/vendor/autoload.php';
\Symfony\Component\VarDumper\VarDumper::dump('`+mark("page")+`');
`)
	if out := runPHP(t, phpBin, page); !strings.Contains(out, mark("page")) {
		t.Errorf("dump_to_page on: output does not show the dump:\n%s", out)
	}
	waitForDump(t, mark("page"))
}

// requireRealPHP returns the devctl php wrapper and composer, or skips when
// the test machine has no PHP that runs (the VMPal arm64 VM: devctl ships
// x86_64 Linux PHP builds only).
func requireRealPHP(t *testing.T) (phpBin, composer string) {
	t.Helper()
	bin := filepath.Join(phpTestServerRoot(), "bin")
	phpBin = filepath.Join(bin, "php")
	if out, err := exec.Command(phpBin, "-r", "echo PHP_VERSION;").CombinedOutput(); err != nil {
		t.Skipf("no runnable PHP CLI at %s (%v: %s)", phpBin, err, out)
	}
	// -r skips auto_prepend_file; a script file proves the prepend loads cleanly.
	hello := filepath.Join(t.TempDir(), "hello.php")
	writeFile(t, hello, "<?php echo function_exists('php_dd') ? 'prepend-loaded' : 'no-prepend';")
	if out := runPHP(t, phpBin, hello); out != "prepend-loaded" {
		t.Fatalf("the php wrapper did not load the devctl prepend: %q", out)
	}
	composer = filepath.Join(bin, "composer")
	if _, err := os.Stat(composer); err != nil {
		t.Skipf("composer not installed at %s", composer)
	}
	return phpBin, composer
}

func runPHP(t *testing.T, phpBin, script string) string {
	t.Helper()
	out, err := exec.Command(phpBin, script).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", phpBin, script, err, out)
	}
	return string(out)
}

func waitForDump(t *testing.T, marker string) capturedDump {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range decodeJSON[[]capturedDump](t, httpGet(t, "/api/dumps?limit=200")) {
			if strings.Contains(d.Nodes, marker) {
				return d
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no dump containing %q reached devctl", marker)
	return capturedDump{}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
