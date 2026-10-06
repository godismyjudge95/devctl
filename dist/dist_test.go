package dist

import (
	"errors"
	"testing"
)

func TestLookupCaddyTokens(t *testing.T) {
	cases := []struct {
		goos, arch, wantToken, wantFile string
	}{
		{"linux", "amd64", "linux_amd64", "caddy-linux-amd64.tar.gz"},
		{"linux", "arm64", "linux_arm64", "caddy-linux-arm64.tar.gz"},
		{"darwin", "arm64", "mac_arm64", "caddy-mac-arm64.tar.gz"},
	}
	for _, tc := range cases {
		a, err := Lookup(tc.goos, tc.arch, "caddy")
		if err != nil {
			t.Fatalf("Lookup(%s, %s, caddy): %v", tc.goos, tc.arch, err)
		}
		if a.Token != tc.wantToken {
			t.Errorf("Lookup(%s, %s, caddy).Token = %q, want %q", tc.goos, tc.arch, a.Token, tc.wantToken)
		}
		if a.File != tc.wantFile {
			t.Errorf("Lookup(%s, %s, caddy).File = %q, want %q", tc.goos, tc.arch, a.File, tc.wantFile)
		}
	}
}

func TestLinuxAmd64HasBinaryServices(t *testing.T) {
	names := []string{
		"caddy", "mailpit", "meilisearch", "typesense", "maxio",
		"clickhouse", "php", "yq", "valkey",
	}
	for _, name := range names {
		if _, err := Lookup("linux", "amd64", name); err != nil {
			t.Errorf("linux/amd64/%s: %v", name, err)
		}
	}
}

func TestDarwinArm64SkipsValkeyTimescalePgvector(t *testing.T) {
	for _, name := range []string{"valkey", "timescale", "pgvector"} {
		_, err := Lookup("darwin", "arm64", name)
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("darwin/arm64/%s: err = %v, want ErrUnsupported", name, err)
		}
	}
}

func TestListenHTTPFor(t *testing.T) {
	linux := ListenHTTPFor("linux")
	if len(linux) != 2 || linux[0] != ":80" || linux[1] != ":443" {
		t.Errorf("linux listen = %v, want [:80 :443]", linux)
	}
	darwin := ListenHTTPFor("darwin")
	if len(darwin) != 2 || darwin[0] != ":8080" || darwin[1] != ":8443" {
		t.Errorf("darwin listen = %v, want [:8080 :8443]", darwin)
	}
}

func TestAllowsFor(t *testing.T) {
	if !AllowsFor("linux", "amd64", "redis") {
		t.Error("linux/amd64 redis should be allowed")
	}
	if AllowsFor("darwin", "arm64", "redis") {
		t.Error("darwin/arm64 redis should be skipped")
	}
	if !AllowsFor("darwin", "arm64", "caddy") {
		t.Error("darwin/arm64 caddy should be allowed")
	}
	if !AllowsFor("linux", "amd64", "mysql") {
		t.Error("linux/amd64 mysql stays allowed until tarball rows exist")
	}
	if !AllowsFor("darwin", "arm64", "dns") {
		t.Error("dns is in-process and always allowed")
	}
}
