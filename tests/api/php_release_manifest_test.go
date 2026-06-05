//go:build integration

package apitest

import (
	"bytes"
	"fmt"
	"os/exec"
	"testing"
	"time"
)

func TestPHPInstall_UsesTaggedReleaseManifest(t *testing.T) {
	body, status := httpPost(t, "/api/php/versions/8.4/install", map[string]any{})
	if status != 200 {
		t.Fatalf("install php 8.4: expected 200, got %d: %s", status, string(body))
	}

	services := decodeJSON[[]ServiceState](t, httpGet(t, "/api/services"))
	var phpSvc *ServiceState
	for i := range services {
		if services[i].ID == "php-fpm-8.4" {
			phpSvc = &services[i]
			break
		}
	}
	if phpSvc == nil {
		t.Fatal("php-fpm-8.4 not found in services list")
	}
	if phpSvc.LatestVersion != "8.4.19" {
		t.Fatalf("latest_version = %q, want %q", phpSvc.LatestVersion, "8.4.19")
	}
	if phpSvc.Version == "" {
		t.Fatal("version is empty for php-fpm-8.4")
	}

	out, err := exec.Command("sh", "-c", "journalctl -u devctl --no-pager | grep 'curl-shim: served php-binaries-20260422.1-php-8.4-cli-linux-x86_64 from cache' | tail -n 1").CombinedOutput()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		t.Fatalf("expected journal to show tagged PHP asset served from cache, got err=%v out=%s", err, string(out))
	}
}

func TestPHPServiceState_UsesManifestPatchMetadata(t *testing.T) {
	pollServiceStatus(t, "php-fpm-8.4", "running", 30*time.Second)

	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)
	for _, svc := range services {
		if svc.ID != "php-fpm-8.4" {
			continue
		}
		if svc.Version == "" {
			t.Fatal("php-fpm-8.4 version is empty")
		}
		if svc.LatestVersion != "8.4.19" {
			t.Fatalf("latest_version = %q, want %q", svc.LatestVersion, "8.4.19")
		}
		if svc.Version != svc.LatestVersion && !svc.UpdateAvailable {
			t.Fatalf("expected update_available for php-fpm-8.4 when version=%q latest=%q", svc.Version, svc.LatestVersion)
		}
		return
	}
	t.Fatal("php-fpm-8.4 not found in services list")
}

func TestPHPServiceUpdate_InstallsNewestTaggedAssets(t *testing.T) {
	result := httpSSE(t, "POST", "/api/services/php-fpm-8.4/update", 5*time.Minute)
	if result.LastEvent != "done" {
		t.Fatalf("update php 8.4: last event = %q, want done; data=%s", result.LastEvent, result.LastData)
	}
	pollServiceStatus(t, "php-fpm-8.4", "running", 30*time.Second)

	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)
	for _, svc := range services {
		if svc.ID != "php-fpm-8.4" {
			continue
		}
		if svc.Version != svc.LatestVersion {
			t.Fatalf("expected php-fpm-8.4 to be updated: version=%q latest=%q", svc.Version, svc.LatestVersion)
		}
		if svc.UpdateAvailable {
			t.Fatalf("expected update_available=false after php update; version=%q latest=%q", svc.Version, svc.LatestVersion)
		}
		return
	}
	t.Fatal("php-fpm-8.4 not found in services list")
}

func TestPHPVersionsEndpoint_SurfacesPatchMetadata(t *testing.T) {
	body := httpGet(t, "/api/php/versions")
	versions := decodeJSON[[]PHPVersion](t, body)
	for _, v := range versions {
		if v.Version != "8.4" {
			continue
		}
		if v.PatchVersion == "" {
			t.Fatal("php versions endpoint missing patch_version for 8.4")
		}
		if v.LatestVersion != "8.4.19" {
			t.Fatalf("latest_version = %q, want %q", v.LatestVersion, "8.4.19")
		}
		return
	}
	t.Fatal(fmt.Sprintf("PHP 8.4 not found in %v", versions))
}
