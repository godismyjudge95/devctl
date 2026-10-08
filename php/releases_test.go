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

func TestLatestReleaseTag_NumericSuffixBeatsLexicographic(t *testing.T) {
	// .9 sorts after .24 as strings, but .24 is the newer build.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "php-binaries-20260723.9"},
			{"tag_name": "php-binaries-20260723.24"},
			{"tag_name": "php-binaries-20260722.99"},
			{"tag_name": "php-binaries-latest"},
		})
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	tag, err := LatestReleaseTag(context.Background())
	if err != nil {
		t.Fatalf("LatestReleaseTag: %v", err)
	}
	if tag != "php-binaries-20260723.24" {
		t.Fatalf("tag = %q, want %q", tag, "php-binaries-20260723.24")
	}
}

func TestComparePHPReleaseTags(t *testing.T) {
	if comparePHPReleaseTags("php-binaries-20260723.9", "php-binaries-20260723.24") >= 0 {
		t.Fatal("expected .24 > .9 numerically")
	}
	if comparePHPReleaseTags("php-binaries-20260723.24", "php-binaries-20260722.99") <= 0 {
		t.Fatal("expected newer date to win even if N is smaller")
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
		case "/download/php-binaries-20260422.1/php-binaries.json":
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
		{"php-8.4-cli-macos-aarch64", "8.4", "cli", true},
		{"php-8.4-fpm-macos-aarch64", "8.4", "fpm", true},
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
		case "/download/php-binaries-latest/php-binaries.json":
			http.NotFound(w, r)
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

func TestParsePHPBinaryAsset_Platform(t *testing.T) {
	minor, kind, platform, ok := parsePHPBinaryAsset("php-8.4-cli-macos-aarch64")
	if !ok || minor != "8.4" || kind != "cli" || platform != "macos-aarch64" {
		t.Fatalf("got (%q, %q, %q, %v)", minor, kind, platform, ok)
	}
	_, _, linuxPlat, ok := parsePHPBinaryAsset("php-8.4-cli-linux-x86_64")
	if !ok || linuxPlat != "linux-x86_64" {
		t.Fatalf("linux platform = %q ok=%v", linuxPlat, ok)
	}
}

func TestReleaseManifestLookup_PlatformAssets(t *testing.T) {
	m := &ReleaseManifest{
		Assets: map[string]ReleaseAssets{
			"8.4": {CLI: "php-8.4-cli-linux-x86_64", FPM: "php-8.4-fpm-linux-x86_64"},
		},
		PlatformAssets: map[string]map[string]ReleaseAssets{
			"linux-x86_64": {
				"8.4": {CLI: "php-8.4-cli-linux-x86_64", FPM: "php-8.4-fpm-linux-x86_64"},
			},
			"macos-aarch64": {
				"8.4": {CLI: "php-8.4-cli-macos-aarch64", FPM: "php-8.4-fpm-macos-aarch64"},
			},
		},
	}
	a, ok := m.Lookup("8.4", "macos-aarch64")
	if !ok || a.CLI != "php-8.4-cli-macos-aarch64" {
		t.Fatalf("macos lookup = %+v ok=%v", a, ok)
	}
	a, ok = m.Lookup("8.4", "linux-x86_64")
	if !ok || a.CLI != "php-8.4-cli-linux-x86_64" {
		t.Fatalf("linux lookup = %+v ok=%v", a, ok)
	}
	if _, ok = m.Lookup("7.0", "macos-aarch64"); ok {
		t.Fatal("macos should not have 7.0")
	}
}

func TestReleaseManifestLookup_OldLinuxOnlyManifest(t *testing.T) {
	m := &ReleaseManifest{
		Assets: map[string]ReleaseAssets{
			"8.4": {CLI: "php-8.4-cli-linux-x86_64", FPM: "php-8.4-fpm-linux-x86_64"},
		},
	}
	a, ok := m.Lookup("8.4", "linux-x86_64")
	if !ok || a.FPM != "php-8.4-fpm-linux-x86_64" {
		t.Fatalf("old linux lookup = %+v ok=%v", a, ok)
	}
	if _, ok = m.Lookup("8.4", "macos-aarch64"); ok {
		t.Fatal("old manifest must not invent macos assets")
	}
}

func TestAssetURLsForMinorOn_SelectsMacOS(t *testing.T) {
	var serverURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "php-binaries-20261006.1"}})
		case "/releases/tags/php-binaries-20261006.1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "php-binaries-20261006.1",
				"assets": []map[string]any{{
					"name":                 manifestAssetName,
					"browser_download_url": serverURL + "/download/php-binaries.json",
				}},
			})
		case "/download/php-binaries-20261006.1/php-binaries.json":
			_ = json.NewEncoder(w).Encode(ReleaseManifest{
				ReleaseTag:  "php-binaries-20261006.1",
				PHPVersions: map[string]string{"8.4": "8.4.23"},
				Assets: map[string]ReleaseAssets{
					"8.4": {CLI: "php-8.4-cli-linux-x86_64", FPM: "php-8.4-fpm-linux-x86_64"},
				},
				PlatformAssets: map[string]map[string]ReleaseAssets{
					"linux-x86_64": {
						"8.4": {CLI: "php-8.4-cli-linux-x86_64", FPM: "php-8.4-fpm-linux-x86_64"},
					},
					"macos-aarch64": {
						"8.4": {CLI: "php-8.4-cli-macos-aarch64", FPM: "php-8.4-fpm-macos-aarch64"},
					},
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
	cliURL, fpmURL, _, err := assetURLsForMinorOn(context.Background(), "8.4", "macos-aarch64")
	if err != nil {
		t.Fatalf("assetURLsForMinorOn: %v", err)
	}
	wantCLI := serverURL + "/download/php-binaries-20261006.1/php-8.4-cli-macos-aarch64"
	wantFPM := serverURL + "/download/php-binaries-20261006.1/php-8.4-fpm-macos-aarch64"
	if cliURL != wantCLI || fpmURL != wantFPM {
		t.Fatalf("cli=%q fpm=%q", cliURL, fpmURL)
	}
}

func TestFetchReleaseManifest_SynthesizesMacOSAssets(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/tags/php-binaries-latest" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "php-binaries-latest",
			"assets": []map[string]any{
				{"name": "php-8.4-cli-linux-x86_64"},
				{"name": "php-8.4-fpm-linux-x86_64"},
				{"name": "php-8.4-cli-macos-aarch64"},
				{"name": "php-8.4-fpm-macos-aarch64"},
			},
		})
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	manifest, err := FetchReleaseManifest(context.Background(), "php-binaries-latest")
	if err != nil {
		t.Fatalf("FetchReleaseManifest: %v", err)
	}
	a, ok := manifest.Lookup("8.4", "macos-aarch64")
	if !ok || a.CLI != "php-8.4-cli-macos-aarch64" {
		t.Fatalf("synthesized macos = %+v ok=%v", a, ok)
	}
	if manifest.Assets["8.4"].CLI != "php-8.4-cli-linux-x86_64" {
		t.Fatalf("linux assets key = %q", manifest.Assets["8.4"].CLI)
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

func TestLatestReleaseTag_FallsBackToAtomOn403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for 1.2.3.4"}`))
		case "/releases.atom":
			w.Header().Set("Content-Type", "application/atom+xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>tag:github.com,2008:Repository/1/v0.17.1</id>
    <link rel="alternate" href="https://github.com/godismyjudge95/devctl/releases/tag/v0.17.1"/>
    <title>v0.17.1</title>
  </entry>
  <entry>
    <id>tag:github.com,2008:Repository/1/php-binaries-20261007.1</id>
    <link rel="alternate" href="https://github.com/godismyjudge95/devctl/releases/tag/php-binaries-20261007.1"/>
    <title>PHP Binaries — php-binaries-20261007.1</title>
  </entry>
  <entry>
    <id>tag:github.com,2008:Repository/1/php-binaries-20261006.1</id>
    <link rel="alternate" href="https://github.com/godismyjudge95/devctl/releases/tag/php-binaries-20261006.1"/>
    <title>php-binaries-20261006.1</title>
  </entry>
</feed>`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	t.Setenv("DEVCTL_PHP_RELEASES_ATOM_URL", ts.URL+"/releases.atom")
	tag, err := LatestReleaseTag(context.Background())
	if err != nil {
		t.Fatalf("LatestReleaseTag: %v", err)
	}
	if tag != "php-binaries-20261007.1" {
		t.Fatalf("tag = %q, want php-binaries-20261007.1 from atom feed", tag)
	}
}

func TestLatestReleaseTag_403WithoutAtomIncludesGitHubMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for 1.2.3.4"}`))
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	t.Setenv("DEVCTL_PHP_RELEASES_ATOM_URL", "")
	_, err := LatestReleaseTag(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("error = %v, want HTTP 403", err)
	}
	if !strings.Contains(err.Error(), "API rate limit exceeded") {
		t.Fatalf("error = %v, want GitHub rate-limit message", err)
	}
}

func TestFetchReleaseManifest_DownloadsJSONWithoutReleaseAPI(t *testing.T) {
	var hitTagsAPI bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/php-binaries-20261007.1/php-binaries.json":
			_ = json.NewEncoder(w).Encode(ReleaseManifest{
				ReleaseTag:  "php-binaries-20261007.1",
				PHPVersions: map[string]string{"8.4": "8.4.23"},
				Assets: map[string]ReleaseAssets{
					"8.4": {CLI: "php-8.4-cli-linux-x86_64", FPM: "php-8.4-fpm-linux-x86_64"},
				},
			})
		case "/releases/tags/php-binaries-20261007.1":
			hitTagsAPI = true
			http.Error(w, "should not query releases API", http.StatusForbidden)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	t.Setenv("DEVCTL_PHP_RELEASES_API_BASE", ts.URL)
	t.Setenv("DEVCTL_PHP_RELEASES_DOWNLOAD_BASE", ts.URL+"/download")
	manifest, err := FetchReleaseManifest(context.Background(), "php-binaries-20261007.1")
	if err != nil {
		t.Fatalf("FetchReleaseManifest: %v", err)
	}
	if hitTagsAPI {
		t.Fatal("FetchReleaseManifest must not call /releases/tags when php-binaries.json is on the download URL")
	}
	if manifest.PHPVersions["8.4"] != "8.4.23" {
		t.Fatalf("php_versions[8.4] = %q", manifest.PHPVersions["8.4"])
	}
}

func TestNewGitHubRequest_SetsBearerToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("DEVCTL_GITHUB_TOKEN", "ghs_test_token")
	req, err := newGitHubRequest(context.Background(), "https://api.github.com/repos/godismyjudge95/devctl/releases")
	if err != nil {
		t.Fatalf("newGitHubRequest: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ghs_test_token" {
		t.Fatalf("Authorization = %q, want Bearer ghs_test_token", got)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
