package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPgvectorPortable_RequiresStampAndFiles(t *testing.T) {
	pgDir := t.TempDir()
	libDir := filepath.Join(pgDir, "lib")
	extDir := filepath.Join(pgDir, "share", "extension")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}

	if isPgvectorPortable(pgDir) {
		t.Fatal("expected false with no files")
	}

	if err := os.WriteFile(filepath.Join(libDir, "vector.so"), []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	if isPgvectorPortable(pgDir) {
		t.Fatal("expected false without control + stamp")
	}

	if err := os.WriteFile(filepath.Join(extDir, "vector.control"), []byte("default_version = '0.8.2'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if isPgvectorPortable(pgDir) {
		t.Fatal("expected false without portable stamp")
	}

	if err := os.WriteFile(pgvectorPortableStampPath(pgDir), []byte("0.7.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if isPgvectorPortable(pgDir) {
		t.Fatal("expected false with wrong stamp version")
	}

	if err := writePgvectorPortableStamp(pgDir); err != nil {
		t.Fatal(err)
	}
	if !isPgvectorPortable(pgDir) {
		t.Fatal("expected true with matching stamp + files")
	}
}

func TestPgvectorExtension_InstallFilesIdempotent(t *testing.T) {
	pgDir := t.TempDir()
	libDir := filepath.Join(pgDir, "lib")
	extDir := filepath.Join(pgDir, "share", "extension")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "vector.so"), []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "vector.control"), []byte("default_version = '0.8.2'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writePgvectorPortableStamp(pgDir); err != nil {
		t.Fatal(err)
	}

	ext := pgvectorExtension{}
	if err := ext.InstallFiles(t.Context(), os.Stdout, pgDir); err != nil {
		t.Fatalf("InstallFiles should no-op when portable: %v", err)
	}
}

func TestPgvectorSourceURL(t *testing.T) {
	want := "https://github.com/pgvector/pgvector/archive/refs/tags/v0.8.2.tar.gz"
	if got := pgvectorSourceURL(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestManagedPostgresExtensionsIncludesPgvector(t *testing.T) {
	ids := map[string]bool{}
	for _, ext := range managedPostgresExtensions() {
		ids[ext.ID()] = true
	}
	if !ids["pgvector"] || !ids["timescaledb"] || !ids["pg_clickhouse"] {
		t.Fatalf("registry missing expected extensions: %v", ids)
	}
}
