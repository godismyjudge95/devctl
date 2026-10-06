package install

import (
	"runtime"
	"testing"

	"github.com/danielgormly/devctl/dist"
)

func TestMySQLTarballURL(t *testing.T) {
	a, err := dist.Lookup("darwin", "arm64", "mysql")
	if err != nil {
		t.Fatal(err)
	}
	got := mysqlTarballURL(a)
	want := "https://cdn.mysql.com/Downloads/MySQL-8.4/mysql-8.4.11-macos15-arm64.tar.gz"
	if got != want {
		t.Fatalf("mysqlTarballURL = %q, want %q", got, want)
	}
}

func TestEDBPostgresZipURL(t *testing.T) {
	got := edbPostgresZipURL()
	want := "https://get.enterprisedb.com/postgresql/postgresql-18.4-1-osx-binaries.zip"
	if got != want {
		t.Fatalf("edbPostgresZipURL = %q, want %q", got, want)
	}
}

func TestPostgresLibEnvAssign(t *testing.T) {
	got := postgresLibEnvAssign("/tmp/pg")
	if runtime.GOOS == "darwin" {
		if got != "DYLD_LIBRARY_PATH=/tmp/pg/lib" {
			t.Fatalf("got %q", got)
		}
		return
	}
	if got != "LD_LIBRARY_PATH=/tmp/pg/lib" {
		t.Fatalf("got %q", got)
	}
}

func TestPerconaTarURLLinuxAmd64(t *testing.T) {
	if perconaTarArch() != "x86_64" && perconaTarArch() != "aarch64" {
		t.Fatalf("unexpected arch %q", perconaTarArch())
	}
}
