package install

import (
	"testing"

	"github.com/danielgormly/devctl/dist"
)

func TestMailpitTarballURL(t *testing.T) {
	cases := []struct {
		goos, arch, want string
	}{
		{"linux", "amd64", "https://github.com/axllent/mailpit/releases/download/v1.27.10/mailpit-linux-amd64.tar.gz"},
		{"linux", "arm64", "https://github.com/axllent/mailpit/releases/download/v1.27.10/mailpit-linux-arm64.tar.gz"},
		{"darwin", "arm64", "https://github.com/axllent/mailpit/releases/download/v1.27.10/mailpit-darwin-arm64.tar.gz"},
	}
	for _, tc := range cases {
		a, err := dist.Lookup(tc.goos, tc.arch, "mailpit")
		if err != nil {
			t.Fatalf("Lookup(%s, %s, mailpit): %v", tc.goos, tc.arch, err)
		}
		got := mailpitTarballURL("v1.27.10", a)
		if got != tc.want {
			t.Errorf("mailpitTarballURL(%s/%s) = %q, want %q", tc.goos, tc.arch, got, tc.want)
		}
	}
}

func TestMeilisearchBinaryURL(t *testing.T) {
	cases := []struct {
		goos, arch, want string
	}{
		{"linux", "amd64", "https://github.com/meilisearch/meilisearch/releases/download/v1.24.0/meilisearch-linux-amd64"},
		{"linux", "arm64", "https://github.com/meilisearch/meilisearch/releases/download/v1.24.0/meilisearch-linux-aarch64"},
		{"darwin", "arm64", "https://github.com/meilisearch/meilisearch/releases/download/v1.24.0/meilisearch-macos-apple-silicon"},
	}
	for _, tc := range cases {
		a, err := dist.Lookup(tc.goos, tc.arch, "meilisearch")
		if err != nil {
			t.Fatalf("Lookup(%s, %s, meilisearch): %v", tc.goos, tc.arch, err)
		}
		got := meilisearchBinaryURL("v1.24.0", a)
		if got != tc.want {
			t.Errorf("meilisearchBinaryURL(%s/%s) = %q, want %q", tc.goos, tc.arch, got, tc.want)
		}
	}
}

func TestTypesenseTarballURL(t *testing.T) {
	cases := []struct {
		goos, arch, want string
	}{
		{"linux", "amd64", "https://dl.typesense.org/releases/29.0/typesense-server-29.0-linux-amd64.tar.gz"},
		{"linux", "arm64", "https://dl.typesense.org/releases/29.0/typesense-server-29.0-linux-arm64.tar.gz"},
		{"darwin", "arm64", "https://dl.typesense.org/releases/29.0/typesense-server-29.0-darwin-arm64.tar.gz"},
	}
	for _, tc := range cases {
		a, err := dist.Lookup(tc.goos, tc.arch, "typesense")
		if err != nil {
			t.Fatalf("Lookup(%s, %s, typesense): %v", tc.goos, tc.arch, err)
		}
		got := typesenseTarballURL("29.0", a)
		if got != tc.want {
			t.Errorf("typesenseTarballURL(%s/%s) = %q, want %q", tc.goos, tc.arch, got, tc.want)
		}
	}
}

func TestMaxioTarballURL(t *testing.T) {
	cases := []struct {
		goos, arch, want string
	}{
		{"linux", "amd64", "https://github.com/coollabsio/maxio/releases/download/v1.11.3/maxio-linux-amd64-1.11.3.tar.gz"},
		{"linux", "arm64", "https://github.com/coollabsio/maxio/releases/download/v1.11.3/maxio-linux-arm64-1.11.3.tar.gz"},
		{"darwin", "arm64", "https://github.com/coollabsio/maxio/releases/download/v1.11.3/maxio-macos-arm64-1.11.3.tar.gz"},
	}
	for _, tc := range cases {
		a, err := dist.Lookup(tc.goos, tc.arch, "maxio")
		if err != nil {
			t.Fatalf("Lookup(%s, %s, maxio): %v", tc.goos, tc.arch, err)
		}
		got := maxioTarballURL("v1.11.3", "1.11.3", a)
		if got != tc.want {
			t.Errorf("maxioTarballURL(%s/%s) = %q, want %q", tc.goos, tc.arch, got, tc.want)
		}
	}
}
