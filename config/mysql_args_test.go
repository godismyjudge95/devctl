package config

import (
	"runtime"
	"strings"
	"testing"
)

func TestMySQLManagedArgs_OmitsUserOnDarwin(t *testing.T) {
	got := MySQLManagedArgs("3306", "127.0.0.1")
	if !strings.Contains(got, "--defaults-file=./my.cnf") {
		t.Errorf("missing defaults-file: %q", got)
	}
	if !strings.Contains(got, "--port=3306") {
		t.Errorf("missing port: %q", got)
	}
	if !strings.Contains(got, "--bind-address=127.0.0.1") {
		t.Errorf("missing bind: %q", got)
	}
	hasUser := strings.Contains(got, "--user=root")
	if runtime.GOOS == "darwin" && hasUser {
		t.Errorf("darwin must omit --user: %q", got)
	}
	if runtime.GOOS != "darwin" && !hasUser {
		t.Errorf("linux must include --user=root: %q", got)
	}
}
