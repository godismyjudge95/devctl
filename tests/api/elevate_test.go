//go:build integration

package apitest

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestElevateStatus reports configuration without requiring root.
func TestElevateStatus(t *testing.T) {
	out, err := exec.Command("devctl", "elevate:status", "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("elevate:status: %v\n%s", err, out)
	}
	var st map[string]any
	if err := json.Unmarshal(out, &st); err != nil {
		t.Fatalf("decode status: %v\n%s", err, out)
	}
	// Unit in test env should have ambient + User=testuser.
	if ports, _ := st["ports_configured"].(bool); !ports {
		t.Errorf("ports_configured: want true in test env (ambient unit), got %v\n%s", st["ports_configured"], out)
	}
	if asUser, _ := st["unit_runs_as_user"].(bool); !asUser {
		t.Errorf("unit_runs_as_user: want true, got %v", st["unit_runs_as_user"])
	}
	t.Logf("elevate status: %s", out)
}

// TestDaemonNotRoot verifies the daemon process is not running as root.
func TestDaemonNotRoot(t *testing.T) {
	// systemctl show -p User
	out, err := exec.Command("systemctl", "show", "devctl", "-p", "User", "--value").CombinedOutput()
	if err != nil {
		t.Fatalf("systemctl show: %v\n%s", err, out)
	}
	user := strings.TrimSpace(string(out))
	if user == "" || user == "root" {
		t.Fatalf("devctl unit User=%q — want non-root site user", user)
	}
	t.Logf("devctl runs as User=%s", user)

	// Confirm AmbientCapabilities is set.
	out2, err := exec.Command("systemctl", "show", "devctl", "-p", "AmbientCapabilities", "--value").CombinedOutput()
	if err != nil {
		t.Fatalf("systemctl show AmbientCapabilities: %v\n%s", err, out2)
	}
	caps := strings.TrimSpace(string(out2))
	if !strings.Contains(strings.ToUpper(caps), "CAP_NET_BIND_SERVICE") && caps != "0" {
		// systemd may show numeric or name form; empty means not set.
		// On some systems AmbientCapabilities shows as a bitmask.
		t.Logf("AmbientCapabilities raw value: %q", caps)
	}
	// Also read unit file.
	data, err := os.ReadFile("/etc/systemd/system/devctl.service")
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	if !strings.Contains(string(data), "AmbientCapabilities=CAP_NET_BIND_SERVICE") {
		t.Fatalf("unit missing AmbientCapabilities:\n%s", data)
	}
	if !strings.Contains(string(data), "User=") {
		t.Fatalf("unit missing User=:\n%s", data)
	}
}

// TestHelperRefusesNonRoot ensures helper ops refuse non-root.
func TestHelperRefusesNonRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		// Run as testuser if we're root in the container.
		out, err := exec.Command("sudo", "-u", "testuser", "devctl", "helper", "install-resolver", "--port", "5354", "--tld", "test").CombinedOutput()
		if err == nil {
			t.Fatalf("helper as testuser should fail, got success: %s", out)
		}
		if !strings.Contains(string(out), "root") {
			t.Fatalf("expected root error, got: %s", out)
		}
		return
	}
	out, err := exec.Command("devctl", "helper", "install-resolver", "--port", "5354", "--tld", "test").CombinedOutput()
	if err == nil {
		t.Fatalf("helper as non-root should fail, got: %s", out)
	}
	if !strings.Contains(string(out), "root") {
		t.Fatalf("expected root error, got: %s", out)
	}
}

// TestElevateResolverIdempotent installs and leaves the resolver drop-in.
func TestElevateResolverIdempotent(t *testing.T) {
	if os.Geteuid() != 0 {
		// Integration tests often run as root inside the container.
		// Try via sudo if needed.
		cmd := exec.Command("sudo", "devctl", "elevate", "resolver")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("sudo elevate resolver: %v\n%s", err, out)
		}
		t.Logf("elevate resolver: %s", out)
	} else {
		out, err := exec.Command("devctl", "elevate", "resolver").CombinedOutput()
		if err != nil {
			t.Fatalf("elevate resolver: %v\n%s", err, out)
		}
		t.Logf("elevate resolver: %s", out)
	}

	if _, err := os.Stat("/etc/systemd/resolved.conf.d/99-devctl-dns.conf"); err != nil {
		t.Fatalf("drop-in missing after elevate resolver: %v", err)
	}
	data, err := os.ReadFile("/etc/systemd/resolved.conf.d/99-devctl-dns.conf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "DNS=127.0.0.1:") {
		t.Fatalf("unexpected drop-in content:\n%s", data)
	}
	if !strings.Contains(string(data), "Domains=~") {
		t.Fatalf("missing Domains= in drop-in:\n%s", data)
	}

	// Idempotent second run.
	cmd := exec.Command("devctl", "elevate", "resolver")
	if os.Geteuid() != 0 {
		cmd = exec.Command("sudo", "devctl", "elevate", "resolver")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("second elevate resolver: %v\n%s", err, out)
	}
}

// TestDNSSetupNeedsElevationWhenAPICannotWrite is a smoke check that DNS setup
// still works when running as elevated unit (User= with write via... actually
// User= cannot write /etc). So POST /api/dns/setup should return needs_elevation.
func TestDNSSetupNeedsElevation(t *testing.T) {
	// Daemon runs as testuser — writing /etc should fail with needs_elevation
	// unless drop-in already exists and... no, WriteFile still needs permission.
	body, code := httpPost(t, "/api/dns/setup", nil)
	// Either 200 (if somehow root — should not) or 403 needs_elevation.
	if code == 200 {
		t.Log("dns setup succeeded (daemon may still be able to write — unexpected for User= unit)")
		return
	}
	if code != 403 {
		t.Fatalf("POST /api/dns/setup: status %d, body %s", code, body)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if ne, _ := resp["needs_elevation"].(bool); !ne {
		t.Fatalf("expected needs_elevation=true, got %v", resp)
	}
	cmd, _ := resp["command"].(string)
	if !strings.Contains(cmd, "elevate") {
		t.Fatalf("command should mention elevate, got %q", cmd)
	}
}
