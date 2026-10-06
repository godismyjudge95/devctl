package install

import (
	"bytes"
	"context"
	"runtime"
	"testing"
)

func TestAptInstallW_NoOpOffLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("apt no-op is for non-linux hosts")
	}
	var buf bytes.Buffer
	if err := aptInstallW(context.Background(), &buf, "this-package-does-not-exist"); err != nil {
		t.Fatalf("aptInstallW off linux: %v", err)
	}
}

func TestMysqldUserFlag(t *testing.T) {
	got := mysqldUserFlag()
	if runtime.GOOS == "darwin" {
		if got != "" {
			t.Errorf("darwin mysqldUserFlag = %q, want empty", got)
		}
		return
	}
	if got != " --user=root" {
		t.Errorf("linux mysqldUserFlag = %q, want \" --user=root\"", got)
	}
}
