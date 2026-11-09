package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPgvectorPortable_RequiresBundleStampAndFiles(t *testing.T) {
	pgDir := t.TempDir()
	libDir := filepath.Join(pgDir, "lib")
	extDir := filepath.Join(pgDir, "share", "extension")
	for _, d := range []string{libDir, extDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(libDir, "vector"+pgShlibExt()), []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "vector.control"), []byte("default_version = '0.8.2'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if isPgvectorPortable(pgDir) {
		t.Fatal("vendor vector library without the bundle stamp must not count as portable")
	}

	if err := os.WriteFile(pgExtensionsStampPath(pgDir), []byte("pg-extensions-20200101.1/old.tar.gz\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if isPgvectorPortable(pgDir) {
		t.Fatal("an older bundle stamp must not count as portable")
	}

	if err := os.WriteFile(pgExtensionsStampPath(pgDir), []byte(pgExtensionsStamp()+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !isPgvectorPortable(pgDir) {
		t.Fatal("current bundle stamp plus files must count as portable")
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
