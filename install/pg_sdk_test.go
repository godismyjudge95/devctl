package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDarwinPGCompileVars(t *testing.T) {
	got := darwinPGCompileVars()
	if runtime.GOOS != "darwin" {
		if got != "" {
			t.Fatalf("non-darwin vars = %q", got)
		}
		return
	}
	if !strings.Contains(got, "SDKROOT=") || !strings.Contains(got, "MacOSX") {
		t.Fatalf("darwin vars = %q", got)
	}
}

func TestWriteDarwinPgConfigShimRewritesSysroot(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real-pg_config")
	body := "#!/bin/sh\necho '-isysroot /Library/Developer/CommandLineTools/SDKs/MacOSX14.sdk -O2'\n"
	if err := os.WriteFile(real, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	shim, err := writeDarwinPgConfigShim(filepath.Join(dir, "bin"), real)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(shim, "--cppflags").Output()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "MacOSX14.sdk") {
		t.Fatalf("shim still has stale sdk: %q", s)
	}
	if !strings.Contains(s, "-isysroot ") {
		t.Fatalf("shim missing isysroot: %q", s)
	}
}

func TestRewriteStaleDarwinSysroot(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "link.txt")
	old := "-isysroot /Library/Developer/CommandLineTools/SDKs/MacOSX14.sdk -lSystem\n"
	if err := os.WriteFile(p, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	if err := rewriteStaleDarwinSysroot(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "MacOSX14.sdk") {
		t.Fatalf("still stale: %s", got)
	}
	if !strings.Contains(string(got), "-isysroot ") {
		t.Fatalf("lost isysroot: %s", got)
	}
}
