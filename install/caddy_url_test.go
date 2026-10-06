package install

import (
	"testing"

	"github.com/danielgormly/devctl/dist"
)

func TestCaddyTarballURL(t *testing.T) {
	cases := []struct {
		goos, arch, want string
	}{
		{
			"linux", "amd64",
			"https://github.com/caddyserver/caddy/releases/download/v2.10.0/caddy_2.10.0_linux_amd64.tar.gz",
		},
		{
			"linux", "arm64",
			"https://github.com/caddyserver/caddy/releases/download/v2.10.0/caddy_2.10.0_linux_arm64.tar.gz",
		},
		{
			"darwin", "arm64",
			"https://github.com/caddyserver/caddy/releases/download/v2.10.0/caddy_2.10.0_mac_arm64.tar.gz",
		},
	}
	for _, tc := range cases {
		a, err := dist.Lookup(tc.goos, tc.arch, "caddy")
		if err != nil {
			t.Fatalf("Lookup(%s, %s, caddy): %v", tc.goos, tc.arch, err)
		}
		got := caddyTarballURL("v2.10.0", a)
		if got != tc.want {
			t.Errorf("caddyTarballURL(%s/%s) = %q, want %q", tc.goos, tc.arch, got, tc.want)
		}
	}
}
