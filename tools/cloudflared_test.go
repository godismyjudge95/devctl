package tools

import (
	"strings"
	"testing"

	"github.com/danielgormly/devctl/dist"
)

func TestParseCloudflaredVersion(t *testing.T) {
	cases := map[string]string{
		"cloudflared version 2026.10.7 (built 2026-10-30-1203 UTC)\n": "2026.10.7",
		"cloudflared version 2025.1.0\n":                              "2025.1.0",
		"cloudflared\n":                                               "",
		"something else entirely":                                     "",
	}
	for in, want := range cases {
		if got := parseCloudflaredVersion(in); got != want {
			t.Errorf("parseCloudflaredVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCloudflaredAssets(t *testing.T) {
	want := map[[2]string]string{
		{"linux", "amd64"}:  "cloudflared-linux-amd64",
		{"linux", "arm64"}:  "cloudflared-linux-arm64",
		{"darwin", "arm64"}: "cloudflared-darwin-arm64.tgz",
	}
	for plat, file := range want {
		a, err := dist.Lookup(plat[0], plat[1], "cloudflared")
		if err != nil {
			t.Fatalf("%v: %v", plat, err)
		}
		if a.File != file {
			t.Errorf("%v file = %q, want %q", plat, a.File, file)
		}
	}
}

func TestCloudflaredReleaseURL(t *testing.T) {
	rel, err := cloudflaredRelease("2026.10.7")
	if err != nil {
		t.Skipf("no cloudflared build for this platform: %v", err)
	}
	if rel.Version != "2026.10.7" {
		t.Errorf("Version = %q", rel.Version)
	}
	if !strings.HasPrefix(rel.DownloadURL, "https://github.com/cloudflare/cloudflared/releases/download/2026.10.7/cloudflared-") {
		t.Errorf("DownloadURL = %q", rel.DownloadURL)
	}
}

func TestCloudflaredRegistered(t *testing.T) {
	if _, ok := Lookup("cloudflared"); !ok {
		t.Fatal("cloudflared is not in AllTools")
	}
}
