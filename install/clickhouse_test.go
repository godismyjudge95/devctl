package install

import (
	"testing"

	"github.com/danielgormly/devctl/dist"
)

func TestNormalizeClickHouseVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"v25.8.28.1-lts", "25.8.28.1"},
		{"v26.5.5.8-stable", "26.5.5.8"},
		{"25.8.28.1", "25.8.28.1"},
		{"v25.8.28.1", "25.8.28.1"},
		{"26.5.5.8-stable", "26.5.5.8"},
		{"v1.2.3.4-rc1", "1.2.3.4"},
		{"", ""},
	}
	for _, tc := range cases {
		got := normalizeClickHouseVersion(tc.in)
		if got != tc.want {
			t.Errorf("normalizeClickHouseVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClickhouseDownloadURL(t *testing.T) {
	linux, err := dist.Lookup("linux", "amd64", "clickhouse")
	if err != nil {
		t.Fatal(err)
	}
	got := clickhouseDownloadURL("25.8.28.1", linux)
	want := "https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-25.8.28.1-amd64.tgz"
	if got != want {
		t.Errorf("linux clickhouseDownloadURL = %q, want %q", got, want)
	}

	darwin, err := dist.Lookup("darwin", "arm64", "clickhouse")
	if err != nil {
		t.Fatal(err)
	}
	got = clickhouseDownloadURL("25.8.28.1", darwin)
	want = "https://builds.clickhouse.com/master/macos-aarch64/clickhouse"
	if got != want {
		t.Errorf("darwin clickhouseDownloadURL = %q, want %q", got, want)
	}
}
