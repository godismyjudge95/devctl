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
	if phpSvc.LatestVersion != "8.4.23" {
		t.Fatalf("latest_version = %q, want %q", phpSvc.LatestVersion, "8.4.23")
	}
	if phpSvc.Version == "" {
		t.Fatal("version is empty for php-fpm-8.4")
	}

	// Install must have pulled the newest fixture tag (served via curl shim / fake GH).
	out, err := exec.Command("sh", "-c", `SERVER_ROOT=$(systemctl show devctl --property=Environment | tr ' ' '\n' | sed -n 's/^DEVCTL_SERVER_ROOT=//p'); "$SERVER_ROOT/php/8.4/php" -v`).CombinedOutput()
	if err != nil {
		t.Fatalf("php 8.4 -v failed: %v out=%s", err, string(out))
	}
	if !bytes.Contains(out, []byte("PHP 8.4.")) {
		t.Fatalf("php 8.4 -v output missing PHP 8.4.: %s", string(out))
	}
}

func TestPHPInstall_Legacy70FromLatestRelease(t *testing.T) {
	body, status := httpPost(t, "/api/php/versions/7.0/install", map[string]any{})
	if status != 200 {
		t.Fatalf("install php 7.0: expected 200, got %d: %s", status, string(body))
	}

	versions := decodeJSON[[]PHPVersion](t, httpGet(t, "/api/php/versions"))
	var found *PHPVersion
	for i := range versions {
		if versions[i].Version == "7.0" {
			found = &versions[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("PHP 7.0 not listed after install; versions=%v", versions)
	}
	if found.PatchVersion != "" && found.PatchVersion != "7.0.33" {
		// Patch may be empty until binary -v is readable; when present it must match release.
		t.Fatalf("php 7.0 patch_version = %q, want 7.0.33 or empty", found.PatchVersion)
	}
	if found.LatestVersion != "7.0.33" {
		t.Fatalf("php 7.0 latest_version = %q, want 7.0.33", found.LatestVersion)
	}

	services := decodeJSON[[]ServiceState](t, httpGet(t, "/api/services"))
	var phpSvc *ServiceState
	for i := range services {
		if services[i].ID == "php-fpm-7.0" {
			phpSvc = &services[i]
			break
		}
	}
	if phpSvc == nil {
		t.Fatal("php-fpm-7.0 not found in services list after install")
	}
	if !phpSvc.Installed {
		t.Fatal("php-fpm-7.0 installed=false after install")
	}

	// Binary must run and report 7.0.x
	out, err := exec.Command("sh", "-c", `SERVER_ROOT=$(systemctl show devctl --property=Environment | tr ' ' '\n' | sed -n 's/^DEVCTL_SERVER_ROOT=//p'); "$SERVER_ROOT/php/7.0/php" -v`).CombinedOutput()
	if err != nil {
		t.Fatalf("php 7.0 -v failed: %v out=%s", err, string(out))
	}
	if !bytes.Contains(out, []byte("PHP 7.0.")) {
		t.Fatalf("php 7.0 -v output missing PHP 7.0.: %s", string(out))
	}

	// Spot-check extensions that the legacy static build is expected to ship.
	mods, err := exec.Command("sh", "-c", `SERVER_ROOT=$(systemctl show devctl --property=Environment | tr ' ' '\n' | sed -n 's/^DEVCTL_SERVER_ROOT=//p'); "$SERVER_ROOT/php/7.0/php" -m`).CombinedOutput()
	if err != nil {
		t.Fatalf("php 7.0 -m failed: %v out=%s", err, string(mods))
	}
	for _, want := range []string{"gd", "openssl", "redis", "mbstring", "zip"} {
		if !bytes.Contains(mods, []byte(want)) {
			t.Errorf("php 7.0 modules missing %q; got:\n%s", want, string(mods))
		}
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
		if svc.LatestVersion != "8.4.23" {
			t.Fatalf("latest_version = %q, want %q", svc.LatestVersion, "8.4.23")
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
		if v.LatestVersion != "8.4.23" {
			t.Fatalf("latest_version = %q, want %q", v.LatestVersion, "8.4.23")
		}
		return
	}
	t.Fatal(fmt.Sprintf("PHP 8.4 not found in %v", versions))
}
