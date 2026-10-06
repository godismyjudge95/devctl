package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPgLibDirPrefersPostgresqlSubdir(t *testing.T) {
	pgDir := t.TempDir()
	want := filepath.Join(pgDir, "lib", "postgresql")
	if err := os.MkdirAll(want, 0755); err != nil {
		t.Fatal(err)
	}
	if got := pgLibDir(pgDir); got != want {
		t.Errorf("pgLibDir = %q, want %q", got, want)
	}
}

func TestPgShareExtDirPrefersPostgresqlSubdir(t *testing.T) {
	pgDir := t.TempDir()
	want := filepath.Join(pgDir, "share", "postgresql", "extension")
	if err := os.MkdirAll(want, 0755); err != nil {
		t.Fatal(err)
	}
	if got := pgShareExtDir(pgDir); got != want {
		t.Errorf("pgShareExtDir = %q, want %q", got, want)
	}
}

func TestPgShlibExt(t *testing.T) {
	got := pgShlibExt()
	if runtime.GOOS == "darwin" && got != ".dylib" {
		t.Errorf("darwin pgShlibExt = %q", got)
	}
	if runtime.GOOS != "darwin" && got != ".so" {
		t.Errorf("linux pgShlibExt = %q", got)
	}
}
