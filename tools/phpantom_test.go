package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchPHPantomLatestRelease_BuildsLinuxURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"0.10.0"}`)
	}))
	defer srv.Close()

	orig := http.DefaultTransport
	http.DefaultTransport = rewireTransport(srv.URL)
	defer func() { http.DefaultTransport = orig }()

	rel, err := fetchPHPantomLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.10.0" {
		t.Fatalf("Version = %q", rel.Version)
	}
	want := "https://github.com/PHPantom-dev/phpantom_lsp/releases/download/0.10.0/phpantom_lsp-x86_64-unknown-linux-gnu.tar.gz"
	if rel.DownloadURL != want {
		t.Fatalf("DownloadURL = %q", rel.DownloadURL)
	}
}

func TestInstalledPHPantomVersion(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "phpantom_lsp")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'phpantom_lsp 0.10.0'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got := installedPHPantomVersion(context.Background(), stub)
	if got != "0.10.0" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchYQLatestRelease_StripsV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v4.53.6"}`)
	}))
	defer srv.Close()

	orig := http.DefaultTransport
	http.DefaultTransport = rewireTransport(srv.URL)
	defer func() { http.DefaultTransport = orig }()

	rel, err := fetchYQLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "4.53.6" {
		t.Fatalf("Version = %q, want 4.53.6", rel.Version)
	}
	if !strings.Contains(rel.DownloadURL, "v4.53.6/yq_linux_amd64") {
		t.Fatalf("DownloadURL = %q", rel.DownloadURL)
	}
}

func TestInstalledYQVersion(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "yq")
	script := "#!/bin/sh\necho 'yq (https://github.com/mikefarah/yq/) version v4.53.6'\n"
	if err := os.WriteFile(stub, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	got := installedYQVersion(context.Background(), stub)
	if got != "4.53.6" {
		t.Fatalf("got %q", got)
	}
}
