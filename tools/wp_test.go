package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchWPCLILatestRelease_StripsVAndBuildsPharURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v2.12.0"}`)
	}))
	defer srv.Close()

	orig := http.DefaultTransport
	http.DefaultTransport = rewireTransport(srv.URL)
	defer func() { http.DefaultTransport = orig }()

	rel, err := fetchWPCLILatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "2.12.0" {
		t.Fatalf("Version = %q, want 2.12.0", rel.Version)
	}
	want := "https://github.com/wp-cli/wp-cli/releases/download/v2.12.0/wp-cli-2.12.0.phar"
	if rel.DownloadURL != want {
		t.Fatalf("DownloadURL = %q, want %q", rel.DownloadURL, want)
	}
}

func TestInstalledWPCLIVersion(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "wp")
	script := "#!/bin/sh\necho 'WP-CLI 2.12.0'\n"
	if err := os.WriteFile(stub, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	got := installedWPCLIVersion(context.Background(), stub)
	if got != "2.12.0" {
		t.Fatalf("got %q, want 2.12.0", got)
	}
}

func TestInstalledWPCLIVersion_AbsentBinary(t *testing.T) {
	got := installedWPCLIVersion(context.Background(), "/nonexistent/wp")
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestInstalledWPCLIVersion_StripsVPrefix(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "wp")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'WP-CLI v2.11.0'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got := installedWPCLIVersion(context.Background(), stub)
	if got != "2.11.0" {
		t.Fatalf("got %q, want 2.11.0", got)
	}
}
