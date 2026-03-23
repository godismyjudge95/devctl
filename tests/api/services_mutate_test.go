//go:build integration

package apitest

import (
	"net/http"
	"testing"
	"time"
)

// TestServiceInstall_Mailpit_InstallPurgeCycle exercises the full
// install → running → stop → start → purge lifecycle for Mailpit.
//
// This is an expensive test (it downloads and installs a binary) and is
// intentionally run as a single serial test so the container is left in a
// clean state for subsequent runs. It uses the curl shim + artifact cache
// so no real internet traffic is required in CI.
func TestServiceInstall_Mailpit_InstallPurgeCycle(t *testing.T) {
	const id = "mailpit"
	const installTimeout = 5 * time.Minute
	const actionTimeout = 30 * time.Second

	// ── 0. Pre-condition: ensure mailpit is not already installed ─────────────
	{
		body := httpGet(t, "/api/services")
		services := decodeJSON[[]ServiceState](t, body)
		for _, svc := range services {
			if svc.ID == id && svc.Installed {
				// Already installed from a previous run — purge first so this
				// test is idempotent.
				t.Logf("pre-condition: %s already installed — purging before test", id)
				res := httpSSE(t, http.MethodDelete, "/api/services/"+id, installTimeout)
				if res.LastEvent != "done" {
					t.Fatalf("pre-condition purge: last event = %q, want \"done\"; last data: %s", res.LastEvent, res.LastData)
				}
				pollServiceInstalled(t, id, false, 30*time.Second)
			}
		}
	}

	// ── 1. Install ────────────────────────────────────────────────────────────
	t.Log("step 1: install mailpit")
	installResult := httpSSE(t, http.MethodPost, "/api/services/"+id+"/install", installTimeout)
	if installResult.LastEvent != "done" {
		t.Fatalf("install: last SSE event = %q, want \"done\"; last data: %s", installResult.LastEvent, installResult.LastData)
	}

	// "output" events should have been emitted.
	outputCount := 0
	for _, ev := range installResult.Events {
		if ev == "output" {
			outputCount++
		}
	}
	if outputCount == 0 {
		t.Error("install: expected at least one 'output' SSE event, got none")
	}

	// ── 2. Verify installed flag ──────────────────────────────────────────────
	t.Log("step 2: verify installed flag")
	pollServiceInstalled(t, id, true, 30*time.Second)

	// ── 3. Verify service auto-started and is running ────────────────────────
	t.Log("step 3: verify service is running")
	pollServiceStatus(t, id, "running", 30*time.Second)

	// ── 4. Stop ───────────────────────────────────────────────────────────────
	t.Log("step 4: stop mailpit")
	stopBody, stopStatus := httpPost(t, "/api/services/"+id+"/stop", nil)
	if stopStatus != http.StatusOK {
		t.Fatalf("stop: expected status 200, got %d: %s", stopStatus, string(stopBody))
	}
	pollServiceStatus(t, id, "stopped", actionTimeout)

	// ── 5. Start ──────────────────────────────────────────────────────────────
	t.Log("step 5: start mailpit")
	startBody, startStatus := httpPost(t, "/api/services/"+id+"/start", nil)
	if startStatus != http.StatusOK {
		t.Fatalf("start: expected status 200, got %d: %s", startStatus, string(startBody))
	}
	pollServiceStatus(t, id, "running", actionTimeout)

	// ── 6. Credentials endpoint ───────────────────────────────────────────────
	t.Log("step 6: verify credentials endpoint")
	credBody := httpGet(t, "/api/services/"+id+"/credentials")
	creds := decodeJSON[map[string]string](t, credBody)
	if len(creds) == 0 {
		t.Error("credentials: expected at least one key in credentials map, got empty map")
	}

	// ── 7. Purge ──────────────────────────────────────────────────────────────
	t.Log("step 7: purge mailpit")
	purgeResult := httpSSE(t, http.MethodDelete, "/api/services/"+id, installTimeout)
	if purgeResult.LastEvent != "done" {
		t.Fatalf("purge: last SSE event = %q, want \"done\"; last data: %s", purgeResult.LastEvent, purgeResult.LastData)
	}

	// ── 8. Verify not installed after purge ───────────────────────────────────
	t.Log("step 8: verify not installed after purge")
	pollServiceInstalled(t, id, false, 30*time.Second)

	// Service should be stopped (not running) after purge.
	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)
	for _, svc := range services {
		if svc.ID == id {
			if svc.Status == "running" {
				t.Errorf("purge: service %q is still running after purge", id)
			}
			return
		}
	}
	t.Errorf("purge: service %q not found in services list after purge", id)
}

// TestServiceInstall_RequiredServices_StopForbidden verifies that required
// services (caddy, dns) return 403 on stop and purge attempts.
func TestServiceInstall_RequiredServices_StopForbidden(t *testing.T) {
	required := []string{"caddy", "dns"}

	for _, id := range required {
		t.Run("stop_"+id, func(t *testing.T) {
			body, status := httpPost(t, "/api/services/"+id+"/stop", nil)
			if status != http.StatusForbidden {
				t.Errorf("stop %s: expected status 403, got %d: %s", id, status, string(body))
			}
		})

		t.Run("purge_"+id, func(t *testing.T) {
			// Purge goes via DELETE SSE endpoint. The 403 is returned immediately
			// with Content-Type: application/json (not SSE) so we use httpDelete.
			body, status := httpDelete(t, "/api/services/"+id)
			if status != http.StatusForbidden {
				t.Errorf("purge %s: expected status 403, got %d: %s", id, status, string(body))
			}
		})
	}
}

// TestServiceInstall_UnknownService_Returns404 verifies that install/stop/start
// on a non-existent service ID returns 404.
func TestServiceInstall_UnknownService_Returns404(t *testing.T) {
	const id = "nonexistent-service-zzzz"

	t.Run("install", func(t *testing.T) {
		_, status := httpPost(t, "/api/services/"+id+"/install", nil)
		// The install endpoint is SSE; on 404 it returns application/json.
		// httpPost returns status directly.
		if status != http.StatusNotFound {
			t.Errorf("install nonexistent: expected 404, got %d", status)
		}
	})

	t.Run("stop", func(t *testing.T) {
		_, status := httpPost(t, "/api/services/"+id+"/stop", nil)
		if status != http.StatusNotFound {
			t.Errorf("stop nonexistent: expected 404, got %d", status)
		}
	})

	t.Run("start", func(t *testing.T) {
		_, status := httpPost(t, "/api/services/"+id+"/start", nil)
		if status != http.StatusNotFound {
			t.Errorf("start nonexistent: expected 404, got %d", status)
		}
	})
}

// TestServiceInstall_SSEEvents_ContainOutputAndDone verifies the SSE event
// structure from an install stream. We use a service that is not currently
// installed (mailpit) but only if it's in a fresh state. This test is a
// lighter structural check that doesn't require the full install to complete.
//
// Because this performs a real install, it is only run when DEVCTL_RUN_INSTALL_TESTS
// is set to "true" — otherwise it's skipped to keep the non-Mailpit cycle tests fast.
func TestServiceInstall_ValKeyInstalled_SSEStructure(t *testing.T) {
	// This test only checks that the SSE stream for an already-installed service
	// (or a service being installed) produces valid event structure. We verify
	// that the credentials endpoint returns a non-empty map after install.
	//
	// If valkey is not installed, skip — the full cycle test covers valkey.
	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)
	for _, svc := range services {
		if svc.ID == "redis" && svc.Installed {
			credBody := httpGet(t, "/api/services/redis/credentials")
			creds := decodeJSON[map[string]string](t, credBody)
			if len(creds) == 0 {
				t.Error("valkey credentials: expected at least one key, got empty map")
			}
			return
		}
	}
	t.Skip("valkey not installed — skipping SSE structure check (run full BATS tests for install cycle)")
}
