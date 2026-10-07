package elevate

import (
	"os"
	"strings"
	"testing"
)

func TestRunDaemonRefusesUnprivileged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	if os.Getenv("DEVCTL_ELEVATED") == "1" {
		t.Skip("DEVCTL_ELEVATED is set")
	}
	err := RunDaemon()
	if err == nil {
		t.Fatal("expected error when not privileged")
	}
	if !strings.Contains(err.Error(), "root") && !strings.Contains(err.Error(), "DEVCTL_ELEVATED") {
		t.Fatalf("got %v", err)
	}
}
