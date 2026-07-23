package php

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLatestReleaseTag_IgnoresNonPHPReleases(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v0.7.0"},
			{"tag_name": "php-binaries-20260421.1"},
			{"tag_name": "php-binaries-20260422.1"},
		})
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	tag, err := LatestReleaseTag(context.Background())
	if err != nil {
		t.Fatalf("LatestReleaseTag: %v", err)
	}
	if tag != "php-binaries-20260422.1" {
		t.Fatalf("tag = %q, want %q", tag, "php-binaries-20260422.1")
	}
}

func TestFetchReleaseManifest_ParsesManifest(t *testing.T) {
	var serverURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/tags/php-binaries-20260422.1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "php-binaries-20260422.1",
				"assets": []map[string]any{{
					"name":                 manifestAssetName,
					"browser_download_url": serverURL + "/download/php-binaries.json",
				}},
			})
		case "/download/php-binaries.json":
			_ = json.NewEncoder(w).Encode(ReleaseManifest{
				ReleaseTag: "php-binaries-20260422.1",
				PHPVersions: map[string]string{
					"8.4": "8.4.19",
				},
				Assets: map[string]ReleaseAssets{
					"8.4": {CLI: "php-8.4-cli-linux-x86_64", FPM: "php-8.4-fpm-linux-x86_64"},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()
	serverURL = ts.URL

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	manifest, err := FetchReleaseManifest(context.Background(), "php-binaries-20260422.1")
	if err != nil {
		t.Fatalf("FetchReleaseManifest: %v", err)
	}
	if manifest.PHPVersions["8.4"] != "8.4.19" {
		t.Fatalf("php_versions[8.4] = %q", manifest.PHPVersions["8.4"])
	}
	if manifest.Assets["8.4"].CLI != "php-8.4-cli-linux-x86_64" {
		t.Fatalf("cli asset = %q", manifest.Assets["8.4"].CLI)
	}
}

func TestAssetURLsForMinor_MissingMinor(t *testing.T) {
	var serverURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "php-binaries-20260422.1"}})
		case "/releases/tags/php-binaries-20260422.1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "php-binaries-20260422.1",
				"assets": []map[string]any{{
					"name":                 manifestAssetName,
					"browser_download_url": serverURL + "/download/php-binaries.json",
				}},
			})
		case "/download/php-binaries.json":
			_ = json.NewEncoder(w).Encode(ReleaseManifest{
				ReleaseTag:  "php-binaries-20260422.1",
				PHPVersions: map[string]string{"8.3": "8.3.22"},
				Assets:      map[string]ReleaseAssets{"8.3": {CLI: "php-8.3-cli-linux-x86_64", FPM: "php-8.3-fpm-linux-x86_64"}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()
	serverURL = ts.URL

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	t.Setenv("DEVCTL_PHP_RELEASES_DOWNLOAD_BASE", ts.URL+"/download")
	_, _, _, err := AssetURLsForMinor(context.Background(), "8.4")
	if err == nil {
		t.Fatal("expected missing-minor error")
	}
	if !strings.Contains(err.Error(), "not available") || !strings.Contains(err.Error(), "8.3") {
		t.Fatalf("error should list available minors, got: %v", err)
	}
}

func TestLatestReleaseTag_PrefersVersionedOverLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		// php-binaries-latest sorts after dated tags lexicographically; we must
		// still prefer the immutable versioned tag.
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "php-binaries-latest"},
			{"tag_name": "php-binaries-20260422.1"},
			{"tag_name": "v0.10.0"},
		})
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	tag, err := LatestReleaseTag(context.Background())
	if err != nil {
		t.Fatalf("LatestReleaseTag: %v", err)
	}
	if tag != "php-binaries-20260422.1" {
		t.Fatalf("tag = %q, want versioned tag over php-binaries-latest", tag)
	}
}

func TestLatestReleaseTag_FallsBackToLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v0.10.0"},
			{"tag_name": "php-binaries-latest"},
		})
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	tag, err := LatestReleaseTag(context.Background())
	if err != nil {
		t.Fatalf("LatestReleaseTag: %v", err)
	}
	if tag != "php-binaries-latest" {
		t.Fatalf("tag = %q, want php-binaries-latest fallback", tag)
	}
}

func TestFetchReleaseManifest_SynthesizesWhenJSONMissing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/tags/php-binaries-latest" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "php-binaries-latest",
			"assets": []map[string]any{
				{"name": "php-8.1-cli-linux-x86_64", "browser_download_url": "https://example/php-8.1-cli"},
				{"name": "php-8.1-fpm-linux-x86_64", "browser_download_url": "https://example/php-8.1-fpm"},
				{"name": "php-8.4-cli-linux-x86_64", "browser_download_url": "https://example/php-8.4-cli"},
				{"name": "php-8.4-fpm-linux-x86_64", "browser_download_url": "https://example/php-8.4-fpm"},
				{"name": "devctl", "browser_download_url": "https://example/devctl"},
			},
		})
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	manifest, err := FetchReleaseManifest(context.Background(), "php-binaries-latest")
	if err != nil {
		t.Fatalf("FetchReleaseManifest: %v", err)
	}
	if manifest.ReleaseTag != "php-binaries-latest" {
		t.Fatalf("ReleaseTag = %q", manifest.ReleaseTag)
	}
	if manifest.Assets["8.4"].CLI != "php-8.4-cli-linux-x86_64" {
		t.Fatalf("8.4 cli = %q", manifest.Assets["8.4"].CLI)
	}
	if manifest.Assets["8.1"].FPM != "php-8.1-fpm-linux-x86_64" {
		t.Fatalf("8.1 fpm = %q", manifest.Assets["8.1"].FPM)
	}
	if _, ok := manifest.Assets["7.4"]; ok {
		t.Fatal("7.4 should not appear when no assets exist")
	}
}

func TestFetchReleaseManifest_SynthesizeFailsWhenNoPHPAssets(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "php-binaries-latest",
			"assets": []map[string]any{
				{"name": "devctl", "browser_download_url": "https://example/devctl"},
			},
		})
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	_, err := FetchReleaseManifest(context.Background(), "php-binaries-latest")
	if err == nil {
		t.Fatal("expected error when no PHP assets and no manifest")
	}
	if !strings.Contains(err.Error(), "missing php-binaries.json") {
		t.Fatalf("error should mention missing json, got: %v", err)
	}
}

func TestParsePHPBinaryAssetName(t *testing.T) {
	tests := []struct {
		name      string
		wantMinor string
		wantKind  string
		wantOK    bool
	}{
		{"php-8.4-cli-linux-x86_64", "8.4", "cli", true},
		{"php-8.1-fpm-linux-x86_64", "8.1", "fpm", true},
		{"php-7.4-cli-linux-x86_64", "7.4", "cli", true},
		{"devctl", "", "", false},
		{"php-binaries.json", "", "", false},
		{"php-8.4-linux-x86_64", "", "", false},
	}
	for _, tt := range tests {
		minor, kind, ok := parsePHPBinaryAssetName(tt.name)
		if ok != tt.wantOK || minor != tt.wantMinor || kind != tt.wantKind {
			t.Errorf("parsePHPBinaryAssetName(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.name, minor, kind, ok, tt.wantMinor, tt.wantKind, tt.wantOK)
		}
	}
}

func TestAssetURLsForMinor_WorksWithSynthesizedManifest(t *testing.T) {
	var serverURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "php-binaries-latest"}})
		case "/releases/tags/php-binaries-latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "php-binaries-latest",
				"assets": []map[string]any{
					{"name": "php-8.4-cli-linux-x86_64"},
					{"name": "php-8.4-fpm-linux-x86_64"},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()
	serverURL = ts.URL

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	t.Setenv("DEVCTL_PHP_RELEASES_DOWNLOAD_BASE", serverURL+"/download")
	cliURL, fpmURL, manifest, err := AssetURLsForMinor(context.Background(), "8.4")
	if err != nil {
		t.Fatalf("AssetURLsForMinor: %v", err)
	}
	if manifest.ReleaseTag != "php-binaries-latest" {
		t.Fatalf("ReleaseTag = %q", manifest.ReleaseTag)
	}
	wantCLI := serverURL + "/download/php-binaries-latest/php-8.4-cli-linux-x86_64"
	wantFPM := serverURL + "/download/php-binaries-latest/php-8.4-fpm-linux-x86_64"
	if cliURL != wantCLI {
		t.Fatalf("cliURL = %q, want %q", cliURL, wantCLI)
	}
	if fpmURL != wantFPM {
		t.Fatalf("fpmURL = %q, want %q", fpmURL, wantFPM)
	}
}

// TestLive_RealGitHubRelease_SynthesizesOrLoadsManifest hits the real GitHub
// API. Opt-in only (DEVCTL_LIVE_PHP_RELEASES=1) so CI/local suites stay offline.
func TestLive_RealGitHubRelease_SynthesizesOrLoadsManifest(t *testing.T) {
	if os.Getenv("DEVCTL_LIVE_PHP_RELEASES") != "1" {
		t.Skip("set DEVCTL_LIVE_PHP_RELEASES=1 to hit real GitHub")
	}
	// Clear test env overrides so we talk to the real API.
	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", "")
	t.Setenv("DEVCTL_PHP_RELEASES_DOWNLOAD_BASE", "")

	manifest, err := LatestReleaseManifest(context.Background())
	if err != nil {
		t.Fatalf("LatestReleaseManifest: %v", err)
	}
	if len(manifest.Assets) == 0 {
		t.Fatal("expected at least one PHP minor in assets")
	}
	for _, minor := range []string{"8.1", "8.2", "8.3", "8.4"} {
		if _, ok := manifest.Assets[minor]; !ok {
			t.Errorf("missing assets for %s", minor)
		}
	}
	// 7.4 is listed in the UI historically but never published.
	if _, ok := manifest.Assets["7.4"]; ok {
		t.Log("note: 7.4 assets unexpectedly present")
	}

	cliURL, fpmURL, _, err := AssetURLsForMinor(context.Background(), "8.4")
	if err != nil {
		t.Fatalf("AssetURLsForMinor(8.4): %v", err)
	}
	if !strings.Contains(cliURL, "php-8.4-cli") || !strings.Contains(fpmURL, "php-8.4-fpm") {
		t.Fatalf("unexpected urls cli=%q fpm=%q", cliURL, fpmURL)
	}

	_, _, _, err = AssetURLsForMinor(context.Background(), "7.4")
	if err == nil {
		t.Fatal("expected 7.4 to be unavailable")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("7.4 error should say not available, got: %v", err)
	}
	t.Logf("release=%s assets=%v 7.4 err=%v", manifest.ReleaseTag, sortedAssetMinors(manifest), err)
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
