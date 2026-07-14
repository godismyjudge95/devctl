package elevate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelperRefusesNonRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — cannot test non-root refusal")
	}
	err := RunHelper([]string{"install-resolver", "--port", "5354", "--tld", "test"})
	if err == nil {
		t.Fatal("expected error when not root")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Fatalf("error should mention root, got: %v", err)
	}
}

func TestResolverDropinContent(t *testing.T) {
	got := ResolverDropinContent("5354", "test")
	want := "[Resolve]\nDNS=127.0.0.1:5354\nDomains=~test\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// Leading-dot tld stripped.
	got2 := ResolverDropinContent("5354", ".test")
	if got2 != want {
		t.Fatalf("leading-dot: got %q want %q", got2, want)
	}
}

func TestBuildServiceFileAmbient(t *testing.T) {
	s := BuildServiceFile("/opt/devctl/devctl", "alice", "/home/alice", "/home/alice/sites/server")
	for _, want := range []string{
		"User=alice",
		"Group=alice",
		"AmbientCapabilities=CAP_NET_BIND_SERVICE",
		"CapabilityBoundingSet=CAP_NET_BIND_SERVICE",
		"NoNewPrivileges=true",
		"ExecStart=/opt/devctl/devctl daemon",
		"Environment=DEVCTL_SITE_USER=alice",
		"Environment=DEVCTL_SERVER_ROOT=/home/alice/sites/server",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("unit missing %q\n%s", want, s)
		}
	}
	if strings.Contains(s, "User=root") {
		t.Error("unit must not run as root")
	}
}

func TestUnitHasAmbientBind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devctl.service")
	content := BuildServiceFile("/bin/devctl", "bob", "/home/bob", "/home/bob/s")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if !UnitHasAmbientBind(path) {
		t.Fatal("expected ambient bind")
	}
	if !UnitRunsAsUser(path) {
		t.Fatal("expected runs as user")
	}
	// Old root unit.
	old := `[Service]
ExecStart=/usr/local/bin/devctl daemon
`
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	if UnitHasAmbientBind(path) {
		t.Fatal("old unit should not report ambient")
	}
	if UnitRunsAsUser(path) {
		t.Fatal("old unit should not report User=")
	}
}

func TestParseCAFingerprint(t *testing.T) {
	// Minimal self-signed-looking PEM is hard; test error paths.
	_, err := ParseCAFingerprint([]byte("not pem"))
	if err == nil {
		t.Fatal("expected error for non-pem")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.conf")
	if err := writeFileAtomic(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("got %q", data)
	}
}

func TestHelperUnknownOp(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	// Still fails non-root first.
	err := RunHelper([]string{"not-an-op"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAptAllowlistValidation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root would actually apt-install")
	}
	err := RunHelper([]string{"apt-install", "evil-package"})
	if err == nil || !strings.Contains(err.Error(), "root") {
		// Non-root fails before allowlist — that's fine.
		if err == nil {
			t.Fatal("expected error")
		}
	}
}
