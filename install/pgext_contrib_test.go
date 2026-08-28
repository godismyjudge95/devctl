package install

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedPostgresExtensionsIncludesSearchContrib(t *testing.T) {
	ids := map[string]bool{}
	for _, ext := range managedPostgresExtensions() {
		ids[ext.ID()] = true
	}
	want := []string{
		"timescaledb", "pgvector", "pg_clickhouse",
		"pg_trgm", "unaccent", "fuzzystrmatch", "btree_gin",
	}
	for _, id := range want {
		if !ids[id] {
			t.Errorf("registry missing %s: %v", id, ids)
		}
	}
}

func TestContribSearchExtension_IsFilesInstalled(t *testing.T) {
	ext := contribSearchExtension{id: "pg_trgm", label: "pg_trgm"}
	pgDir := t.TempDir()
	if ext.IsFilesInstalled(pgDir) {
		t.Fatal("expected false with empty tree")
	}

	libDir := filepath.Join(pgDir, "lib")
	extDir := filepath.Join(pgDir, "share", "extension")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "pg_trgm.so"), []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	if ext.IsFilesInstalled(pgDir) {
		t.Fatal("expected false without control file")
	}

	if err := os.WriteFile(filepath.Join(extDir, "pg_trgm.control"), []byte("default_version = '1.6'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !ext.IsFilesInstalled(pgDir) {
		t.Fatal("expected true with .so and .control")
	}

	if err := os.Remove(filepath.Join(libDir, "pg_trgm.so")); err != nil {
		t.Fatal(err)
	}
	if ext.IsFilesInstalled(pgDir) {
		t.Fatal("expected false without .so")
	}
}

func TestContribSearchExtension_InstallFiles(t *testing.T) {
	ext := contribSearchExtension{id: "unaccent", label: "unaccent"}
	empty := t.TempDir()
	if err := ext.InstallFiles(t.Context(), io.Discard, empty); err == nil {
		t.Fatal("expected error when files missing")
	}

	pgDir := t.TempDir()
	writeFakeContribFiles(t, pgDir, "unaccent")
	if err := ext.InstallFiles(t.Context(), io.Discard, pgDir); err != nil {
		t.Fatalf("InstallFiles should no-op when files present: %v", err)
	}
}

func TestContribCreateSQL(t *testing.T) {
	for _, id := range []string{"pg_trgm", "unaccent", "fuzzystrmatch", "btree_gin"} {
		sql := contribCreateSQL(id)
		if strings.Contains(sql, "$") {
			t.Errorf("%s: SQL must not contain $: %q", id, sql)
		}
		if !strings.Contains(sql, "IF NOT EXISTS") {
			t.Errorf("%s: SQL must use IF NOT EXISTS: %q", id, sql)
		}
		if !strings.Contains(sql, id) {
			t.Errorf("%s: SQL must contain module name: %q", id, sql)
		}
	}
}

func TestParseConnectableDatabases(t *testing.T) {
	got := parseConnectableDatabases("\npostgres\n\ntemplate1\nmydb\n")
	want := []string{"template1", "postgres", "mydb"}
	if len(got) != len(want) {
		t.Fatalf("len got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}

	got = parseConnectableDatabases("template1\npostgres\n")
	if len(got) != 2 || got[0] != "template1" || got[1] != "postgres" {
		t.Fatalf("already-first: got %v", got)
	}

	got = parseConnectableDatabases("postgres\n")
	if len(got) != 1 || got[0] != "postgres" {
		t.Fatalf("no template1: got %v", got)
	}

	got = parseConnectableDatabases("template0\npostgres\ntemplate1\n")
	if len(got) != 2 || got[0] != "template1" || got[1] != "postgres" {
		t.Fatalf("skipped template0: got %v", got)
	}
}

func writeFakeContribFiles(t *testing.T, pgDir, id string) {
	t.Helper()
	libDir := filepath.Join(pgDir, "lib")
	extDir := filepath.Join(pgDir, "share", "extension")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, id+".so"), []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, id+".control"), []byte("default_version = '1.0'\n"), 0644); err != nil {
		t.Fatal(err)
	}
}
