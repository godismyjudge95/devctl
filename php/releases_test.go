package php

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
