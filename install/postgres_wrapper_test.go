package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/paths"
)

func TestWritePostgresClientWrappers_DarwinPSQL(t *testing.T) {
	root := t.TempDir()
	pgBin := filepath.Join(paths.ServiceDir(root, "postgres"), "bin")
	if err := os.MkdirAll(pgBin, 0755); err != nil {
		t.Fatal(err)
	}
	// EDB macOS layout: psql, no psql.bin.
	for _, name := range []string{"psql", "pg_dump", "pg_restore", "createdb", "dropdb"} {
		if err := os.WriteFile(filepath.Join(pgBin, name), []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	if err := writePostgresClientWrappers(root); err != nil {
		t.Fatalf("writePostgresClientWrappers: %v", err)
	}

	dest := filepath.Join(paths.BinDir(root), "psql")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read psql wrapper: %v", err)
	}
	s := string(data)
	wantExec := `exec "` + filepath.Join(pgBin, "psql") + `"`
	if !strings.Contains(s, wantExec) {
		t.Errorf("psql wrapper missing %s\n%s", wantExec, s)
	}
	if strings.Contains(s, "psql.bin") {
		t.Errorf("darwin wrapper must not target psql.bin\n%s", s)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("psql wrapper not executable: %v", info.Mode())
	}
}

func TestWritePostgresClientWrappers_PrefersPsqlBin(t *testing.T) {
	root := t.TempDir()
	pgBin := filepath.Join(paths.ServiceDir(root, "postgres"), "bin")
	if err := os.MkdirAll(pgBin, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"psql", "psql.bin", "pg_dump"} {
		if err := os.WriteFile(filepath.Join(pgBin, name), []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	if err := writePostgresClientWrappers(root); err != nil {
		t.Fatalf("writePostgresClientWrappers: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(paths.BinDir(root), "psql"))
	if err != nil {
		t.Fatalf("read psql wrapper: %v", err)
	}
	s := string(data)
	wantExec := `exec "` + filepath.Join(pgBin, "psql.bin") + `"`
	if !strings.Contains(s, wantExec) {
		t.Errorf("linux wrapper missing %s\n%s", wantExec, s)
	}
}
