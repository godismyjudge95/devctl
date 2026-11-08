package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/paths"
)

func TestWriteMySQLClientWrappers(t *testing.T) {
	root := t.TempDir()
	mysqlBin := filepath.Join(paths.ServiceDir(root, "mysql"), "bin")
	if err := os.MkdirAll(mysqlBin, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range mysqlClientBins {
		if err := os.WriteFile(filepath.Join(mysqlBin, name), []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	if err := writeMySQLClientWrappers(root); err != nil {
		t.Fatalf("writeMySQLClientWrappers: %v", err)
	}

	mysqlDir := paths.ServiceDir(root, "mysql")
	for _, name := range mysqlClientBins {
		dest := filepath.Join(paths.BinDir(root), name)
		data, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("read wrapper %s: %v", name, err)
		}
		s := string(data)
		if !strings.Contains(s, "MYSQL_HOME="+mysqlDir) {
			t.Errorf("%s missing MYSQL_HOME=%s\n%s", name, mysqlDir, s)
		}
		wantExec := `exec "` + filepath.Join(mysqlBin, name) + `"`
		if !strings.Contains(s, wantExec) {
			t.Errorf("%s missing %s\n%s", name, wantExec, s)
		}
		info, err := os.Stat(dest)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0111 == 0 {
			t.Errorf("%s not executable: %v", name, info.Mode())
		}
	}
}

func TestWriteMySQLClientWrappers_SkipsMissing(t *testing.T) {
	root := t.TempDir()
	mysqlBin := filepath.Join(paths.ServiceDir(root, "mysql"), "bin")
	if err := os.MkdirAll(mysqlBin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mysqlBin, "mysql"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := writeMySQLClientWrappers(root); err != nil {
		t.Fatalf("writeMySQLClientWrappers: %v", err)
	}

	if _, err := os.Stat(filepath.Join(paths.BinDir(root), "mysql")); err != nil {
		t.Fatalf("mysql wrapper missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.BinDir(root), "mysqldump")); !os.IsNotExist(err) {
		t.Fatal("mysqldump wrapper should be absent when the binary is missing")
	}
}

func TestEnsureMySQLConfig_NoOpWhenNotInstalled(t *testing.T) {
	root := t.TempDir()
	if err := EnsureMySQLConfig(root); err != nil {
		t.Fatalf("EnsureMySQLConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.BinDir(root), "mysql")); !os.IsNotExist(err) {
		t.Fatal("wrapper must not be written when mysqld is absent")
	}
}

func TestEnsureMySQLConfig_WritesWrappersAndConfigEnv(t *testing.T) {
	root := t.TempDir()
	mysqlDir := paths.ServiceDir(root, "mysql")
	mysqlBin := filepath.Join(mysqlDir, "bin")
	if err := os.MkdirAll(mysqlBin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mysqlBin, "mysqld"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mysqlBin, "mysql"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMySQLConfig(root); err != nil {
		t.Fatalf("EnsureMySQLConfig: %v", err)
	}

	if _, err := os.Stat(filepath.Join(paths.BinDir(root), "mysql")); err != nil {
		t.Fatalf("mysql wrapper: %v", err)
	}
	envPath := filepath.Join(mysqlDir, "config.env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("config.env: %v", err)
	}
	s := string(data)
	for _, want := range []string{"DB_CONNECTION=mysql", "DB_HOST=127.0.0.1", "DB_PORT=3306", "DB_USERNAME=root"} {
		if !strings.Contains(s, want) {
			t.Errorf("config.env missing %q\n%s", want, s)
		}
	}

	// Existing config.env is not overwritten.
	if err := os.WriteFile(envPath, []byte("DB_PASSWORD=kept\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMySQLConfig(root); err != nil {
		t.Fatalf("EnsureMySQLConfig second call: %v", err)
	}
	kept, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "DB_PASSWORD=kept\n" {
		t.Errorf("config.env overwritten: %q", kept)
	}
}
