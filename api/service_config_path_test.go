package api

import (
	"path/filepath"
	"testing"
)

func TestServiceConfigFilePath_Postgres(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server")

	for _, file := range []string{"postgresql.conf", "pg_hba.conf"} {
		got, ok := serviceConfigFilePath(root, "postgres", file)
		if !ok {
			t.Fatalf("%s: not allowed", file)
		}
		if want := filepath.Join(root, "postgres", "data", file); got != want {
			t.Errorf("%s: got %q, want %q", file, got, want)
		}
	}

	for _, file := range []string{"postmaster.pid", "pg_ident.conf", "../config.env", ""} {
		if got, ok := serviceConfigFilePath(root, "postgres", file); ok {
			t.Errorf("%q: allowed as %q, want rejected", file, got)
		}
	}
}
