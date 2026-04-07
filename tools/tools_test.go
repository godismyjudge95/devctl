package tools

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// sqliteVersionIntToString
// ---------------------------------------------------------------------------

func TestSQLiteVersionIntToString(t *testing.T) {
	cases := []struct {
		versionInt int
		want       string
	}{
		{3460100, "3.46.1"},
		{3470200, "3.47.2"},
		{3000000, "3.0.0"},
		{3460000, "3.46.0"},
		{3460105, "3.46.1"}, // non-zero build digit is ignored in output
	}
	for _, tc := range cases {
		got := sqliteVersionIntToString(tc.versionInt)
		if got != tc.want {
			t.Errorf("sqliteVersionIntToString(%d) = %q, want %q", tc.versionInt, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// fetchSQLite3LatestRelease (against a fake HTTP server)
// ---------------------------------------------------------------------------

func TestFetchSQLite3LatestRelease_ParsesPage(t *testing.T) {
	html := `<html><body>
<a href="2024/sqlite-tools-linux-x64-3460100.zip">sqlite-tools-linux-x64-3460100.zip</a>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, html)
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewireTransport(srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	rel, err := fetchSQLite3LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("fetchSQLite3LatestRelease: %v", err)
	}
	if rel.Version != "3.46.1" {
		t.Errorf("Version = %q, want %q", rel.Version, "3.46.1")
	}
	if rel.VersionInt != 3460100 {
		t.Errorf("VersionInt = %d, want 3460100", rel.VersionInt)
	}
	if !strings.Contains(rel.DownloadURL, "sqlite-tools-linux-x64-3460100.zip") {
		t.Errorf("DownloadURL = %q, does not contain expected filename", rel.DownloadURL)
	}
}

func TestFetchSQLite3LatestRelease_NoMatchReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body>no downloads here</body></html>")
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewireTransport(srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	_, err := fetchSQLite3LatestRelease(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// installedSQLite3Version
// ---------------------------------------------------------------------------

func TestInstalledSQLite3Version_AbsentBinary(t *testing.T) {
	got := installedSQLite3Version(context.Background(), "/nonexistent/sqlite3")
	if got != "" {
		t.Errorf("expected empty string for absent binary, got %q", got)
	}
}

func TestInstalledSQLite3Version_RealBinary(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "sqlite3")
	script := "#!/bin/sh\necho '3.46.1 2024-08-13 09:16:08 somehash'\n"
	if err := os.WriteFile(stub, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	got := installedSQLite3Version(context.Background(), stub)
	if got != "3.46.1" {
		t.Errorf("InstalledSQLite3Version = %q, want %q", got, "3.46.1")
	}
}

// ---------------------------------------------------------------------------
// installedFNMVersion
// ---------------------------------------------------------------------------

func TestInstalledFNMVersion_AbsentBinary(t *testing.T) {
	got := installedFNMVersion(context.Background(), "/nonexistent/fnm")
	if got != "" {
		t.Errorf("expected empty string for absent binary, got %q", got)
	}
}

func TestInstalledFNMVersion_RealBinary(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "fnm")
	// fnm --version outputs "fnm 1.39.0"
	script := "#!/bin/sh\necho 'fnm 1.39.0'\n"
	if err := os.WriteFile(stub, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	got := installedFNMVersion(context.Background(), stub)
	if got != "1.39.0" {
		t.Errorf("installedFNMVersion = %q, want %q", got, "1.39.0")
	}
}

func TestInstalledFNMVersion_OnlyOneLine(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "fnm")
	// Edge case: output with only one field should return empty string.
	script := "#!/bin/sh\necho 'fnm'\n"
	if err := os.WriteFile(stub, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	got := installedFNMVersion(context.Background(), stub)
	if got != "" {
		t.Errorf("expected empty string for single-field output, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// fetchFNMLatestRelease (against a fake GitHub API server)
// ---------------------------------------------------------------------------

func TestFetchFNMLatestRelease_ParsesTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v1.39.0"}`)
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewireTransport(srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	rel, err := fetchFNMLatestRelease(context.Background())
	if err != nil {
		t.Fatalf("fetchFNMLatestRelease: %v", err)
	}
	if rel.Version != "1.39.0" {
		t.Errorf("Version = %q, want %q", rel.Version, "1.39.0")
	}
	if !strings.Contains(rel.DownloadURL, "v1.39.0/fnm-linux.zip") {
		t.Errorf("DownloadURL = %q, expected v1.39.0/fnm-linux.zip", rel.DownloadURL)
	}
}

func TestFetchFNMLatestRelease_TagWithoutV(t *testing.T) {
	// Ensure "v" prefix is stripped from the Version field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v2.0.0"}`)
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewireTransport(srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	rel, err := fetchFNMLatestRelease(context.Background())
	if err != nil {
		t.Fatalf("fetchFNMLatestRelease: %v", err)
	}
	if strings.HasPrefix(rel.Version, "v") {
		t.Errorf("Version %q still has 'v' prefix, want stripped version", rel.Version)
	}
}

// ---------------------------------------------------------------------------
// extractBinaryFromZip
// ---------------------------------------------------------------------------

func TestExtractBinaryFromZip_ExtractsSQLite3(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "tools.zip")
	destPath := filepath.Join(dir, "sqlite3")

	content := []byte("fake sqlite3 binary content")
	if err := buildTestZip(zipPath, map[string][]byte{
		"sqlite3":          content,
		"sqldiff":          []byte("other binary"),
		"sqlite3_analyzer": []byte("another binary"),
	}); err != nil {
		t.Fatal(err)
	}

	if err := extractBinaryFromZip(zipPath, "sqlite3", destPath); err != nil {
		t.Fatalf("extractBinaryFromZip: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractBinaryFromZip_ExtractsFNM(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "fnm-linux.zip")
	destPath := filepath.Join(dir, "fnm")

	content := []byte("fake fnm binary content")
	if err := buildTestZip(zipPath, map[string][]byte{
		"fnm": content,
	}); err != nil {
		t.Fatal(err)
	}

	if err := extractBinaryFromZip(zipPath, "fnm", destPath); err != nil {
		t.Fatalf("extractBinaryFromZip: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractBinaryFromZip_MissingEntryReturnsError(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "tools.zip")

	if err := buildTestZip(zipPath, map[string][]byte{
		"sqldiff": []byte("other binary"),
	}); err != nil {
		t.Fatal(err)
	}

	err := extractBinaryFromZip(zipPath, "sqlite3", filepath.Join(dir, "sqlite3"))
	if err == nil {
		t.Fatal("expected error when binary not found in zip, got nil")
	}
}

// ---------------------------------------------------------------------------
// EnsureLatest — download behaviour
// ---------------------------------------------------------------------------

func TestEnsureLatest_DownloadsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")

	downloaded := false
	tl := Tool{
		Name: "mytool",
		LatestRelease: func(_ context.Context) (Release, error) {
			return Release{Version: "1.2.3", DownloadURL: "http://example.com/mytool"}, nil
		},
		InstalledVersion: func(_ context.Context, _ string) string { return "" },
		DownloadTo: func(_ context.Context, _ Release, destPath string) error {
			downloaded = true
			return os.WriteFile(destPath, []byte("binary content"), 0644)
		},
	}

	if err := EnsureLatest(context.Background(), tl, binDir, io.Discard); err != nil {
		t.Fatalf("EnsureLatest: %v", err)
	}
	if !downloaded {
		t.Error("expected DownloadTo to be called, but it was not")
	}
	if _, err := os.Stat(filepath.Join(binDir, "mytool")); err != nil {
		t.Errorf("binary not found at expected path: %v", err)
	}
}

func TestEnsureLatest_SkipsWhenUpToDate(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	downloaded := false
	tl := Tool{
		Name: "mytool",
		LatestRelease: func(_ context.Context) (Release, error) {
			return Release{Version: "1.2.3"}, nil
		},
		InstalledVersion: func(_ context.Context, _ string) string { return "1.2.3" },
		DownloadTo: func(_ context.Context, _ Release, _ string) error {
			downloaded = true
			return nil
		},
	}

	if err := EnsureLatest(context.Background(), tl, binDir, io.Discard); err != nil {
		t.Fatalf("EnsureLatest: %v", err)
	}
	if downloaded {
		t.Error("expected DownloadTo NOT to be called when already up-to-date")
	}
}

func TestEnsureLatest_ReturnsErrorWhenLatestReleaseFails(t *testing.T) {
	dir := t.TempDir()
	tl := Tool{
		Name: "mytool",
		LatestRelease: func(_ context.Context) (Release, error) {
			return Release{}, errors.New("network unavailable")
		},
		InstalledVersion: func(_ context.Context, _ string) string { return "" },
		DownloadTo:       func(_ context.Context, _ Release, _ string) error { return nil },
	}

	err := EnsureLatest(context.Background(), tl, dir, io.Discard)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "network unavailable") {
		t.Errorf("error %q does not mention underlying cause", err.Error())
	}
}

func TestEnsureLatest_ReturnsErrorWhenDownloadFails(t *testing.T) {
	dir := t.TempDir()
	tl := Tool{
		Name: "mytool",
		LatestRelease: func(_ context.Context) (Release, error) {
			return Release{Version: "1.0.0"}, nil
		},
		InstalledVersion: func(_ context.Context, _ string) string { return "" },
		DownloadTo: func(_ context.Context, _ Release, _ string) error {
			return errors.New("download failed")
		},
	}

	err := EnsureLatest(context.Background(), tl, dir, io.Discard)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEnsureLatest_CleansTempFileOnDownloadError(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")

	tl := Tool{
		Name: "mytool",
		LatestRelease: func(_ context.Context) (Release, error) {
			return Release{Version: "1.0.0"}, nil
		},
		InstalledVersion: func(_ context.Context, _ string) string { return "" },
		DownloadTo: func(_ context.Context, _ Release, destPath string) error {
			_ = os.WriteFile(destPath, []byte("partial"), 0644)
			return errors.New("interrupted")
		},
	}

	_ = EnsureLatest(context.Background(), tl, binDir, io.Discard)

	tmpPath := filepath.Join(binDir, "mytool.download")
	if _, err := os.Stat(tmpPath); err == nil {
		t.Errorf("temp file %s was not cleaned up after download failure", tmpPath)
	}
}

// ---------------------------------------------------------------------------
// EnsureLatest — alias symlink behaviour
// ---------------------------------------------------------------------------

func TestEnsureLatest_CreatesAliasSymlink(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")

	tl := Tool{
		Name:    "fnm",
		Aliases: []string{"nvm"},
		LatestRelease: func(_ context.Context) (Release, error) {
			return Release{Version: "1.0.0"}, nil
		},
		InstalledVersion: func(_ context.Context, _ string) string { return "" },
		DownloadTo: func(_ context.Context, _ Release, destPath string) error {
			return os.WriteFile(destPath, []byte("binary"), 0644)
		},
	}

	if err := EnsureLatest(context.Background(), tl, binDir, io.Discard); err != nil {
		t.Fatalf("EnsureLatest: %v", err)
	}

	aliasPath := filepath.Join(binDir, "nvm")
	info, err := os.Lstat(aliasPath)
	if err != nil {
		t.Fatalf("alias %s not found: %v", aliasPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("alias %s is not a symlink", aliasPath)
	}

	// Symlink must point to the fnm binary.
	target, err := os.Readlink(aliasPath)
	if err != nil {
		t.Fatalf("readlink %s: %v", aliasPath, err)
	}
	wantTarget := filepath.Join(binDir, "fnm")
	if target != wantTarget {
		t.Errorf("symlink target = %q, want %q", target, wantTarget)
	}
}

func TestEnsureLatest_RefreshesAliasWhenUpToDate(t *testing.T) {
	// Even when no download is needed, stale/missing aliases should be fixed.
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write the binary itself so InstalledVersion can check it exists.
	binPath := filepath.Join(binDir, "fnm")
	if err := os.WriteFile(binPath, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	tl := Tool{
		Name:    "fnm",
		Aliases: []string{"nvm"},
		LatestRelease: func(_ context.Context) (Release, error) {
			return Release{Version: "1.0.0"}, nil
		},
		InstalledVersion: func(_ context.Context, _ string) string { return "1.0.0" }, // already current
		DownloadTo: func(_ context.Context, _ Release, _ string) error {
			t.Error("DownloadTo must not be called when up-to-date")
			return nil
		},
	}

	if err := EnsureLatest(context.Background(), tl, binDir, io.Discard); err != nil {
		t.Fatalf("EnsureLatest: %v", err)
	}

	aliasPath := filepath.Join(binDir, "nvm")
	if _, err := os.Lstat(aliasPath); err != nil {
		t.Errorf("alias %s should exist even when binary was already up-to-date: %v", aliasPath, err)
	}
}

func TestEnsureLatest_MultipleAliases(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")

	tl := Tool{
		Name:    "mytool",
		Aliases: []string{"alias1", "alias2", "alias3"},
		LatestRelease: func(_ context.Context) (Release, error) {
			return Release{Version: "1.0.0"}, nil
		},
		InstalledVersion: func(_ context.Context, _ string) string { return "" },
		DownloadTo: func(_ context.Context, _ Release, destPath string) error {
			return os.WriteFile(destPath, []byte("binary"), 0644)
		},
	}

	if err := EnsureLatest(context.Background(), tl, binDir, io.Discard); err != nil {
		t.Fatalf("EnsureLatest: %v", err)
	}

	for _, alias := range tl.Aliases {
		aliasPath := filepath.Join(binDir, alias)
		if _, err := os.Lstat(aliasPath); err != nil {
			t.Errorf("alias %s not found: %v", alias, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func buildTestZip(zipPath string, entries map[string][]byte) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			return err
		}
		if _, err := fw.Write(content); err != nil {
			return err
		}
	}
	return w.Close()
}

// rewireTransport returns an http.RoundTripper that replaces the host of every
// outgoing request with the provided base URL (e.g. "http://127.0.0.1:PORT").
func rewireTransport(baseURL string) http.RoundTripper {
	return &hostRewriter{base: baseURL, inner: http.DefaultTransport}
}

type hostRewriter struct {
	base  string
	inner http.RoundTripper
}

func (h *hostRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	host := strings.TrimPrefix(h.base, "http://")
	host = strings.TrimPrefix(host, "https://")
	req2.URL.Host = host
	return h.inner.RoundTrip(req2)
}
