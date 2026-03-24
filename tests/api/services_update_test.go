//go:build integration

package apitest

import (
	"net/http"
	"testing"
	"time"
)

// TestServiceUpdate_Mailpit_UpdateCycle exercises the full update flow for Mailpit:
//   - inject a fake "latest" version via the /_testing/ endpoint
//   - verify update_available becomes true
//   - trigger POST /api/services/mailpit/update
//   - verify the service restarts (running)
//   - verify update_available clears (recheckLatestVersion ran)
//
// Prerequisites:
//   - devctl is running with DEVCTL_TESTING=true
//   - the curl shim + artifact cache are in place (mailpit-update-linux-amd64.tar.gz cached)
//   - Mailpit is installed (or will be installed as a pre-condition step)
func TestServiceUpdate_Mailpit_UpdateCycle(t *testing.T) {
	const id = "mailpit"
	const updateTimeout = 5 * time.Minute
	const actionTimeout = 30 * time.Second

	// ── Pre-condition: ensure mailpit is installed ────────────────────────────
	{
		body := httpGet(t, "/api/services")
		services := decodeJSON[[]ServiceState](t, body)
		installed := false
		for _, svc := range services {
			if svc.ID == id && svc.Installed {
				installed = true
				break
			}
		}
		if !installed {
			t.Logf("pre-condition: %s not installed — installing before update test", id)
			res := httpSSE(t, http.MethodPost, "/api/services/"+id+"/install", updateTimeout)
			if res.LastEvent != "done" {
				t.Fatalf("pre-condition install: last event = %q, want \"done\"; last data: %s", res.LastEvent, res.LastData)
			}
			pollServiceInstalled(t, id, true, actionTimeout)
			pollServiceStatus(t, id, "running", actionTimeout)
		}
	}

	// Ensure the service is running before we start.
	pollServiceStatus(t, id, "running", actionTimeout)

	// ── 1. Inject a fake "latest" version ────────────────────────────────────
	t.Log("step 1: inject fake latest version v9999.0.0")
	injectBody, injectStatus := httpPost(t, "/_testing/services/"+id+"/latest-version", map[string]string{
		"version": "v9999.0.0",
	})
	if injectStatus != http.StatusOK {
		t.Fatalf("inject latest-version: expected 200, got %d: %s", injectStatus, string(injectBody))
	}

	// ── 2. Verify update_available becomes true ───────────────────────────────
	t.Log("step 2: wait for update_available == true")
	pollUpdateAvailable(t, id, true, actionTimeout)

	// ── 3. Trigger update (SSE stream) ───────────────────────────────────────
	t.Log("step 3: trigger update")
	updateResult := httpSSE(t, http.MethodPost, "/api/services/"+id+"/update", updateTimeout)
	if updateResult.LastEvent != "done" {
		t.Fatalf("update: last SSE event = %q, want \"done\"; last data: %s", updateResult.LastEvent, updateResult.LastData)
	}
	outputCount := 0
	for _, ev := range updateResult.Events {
		if ev == "output" {
			outputCount++
		}
	}
	if outputCount == 0 {
		t.Error("update: expected at least one 'output' SSE event, got none")
	}

	// ── 4. Verify service restarted and is running ────────────────────────────
	t.Log("step 4: verify service is running after update")
	pollServiceStatus(t, id, "running", actionTimeout)

	// ── 5. Verify the injected fake version was cleared ──────────────────────
	// After a successful update, the stale cached "v9999.0.0" must be cleared
	// so clients don't keep seeing a false update badge. In connected
	// environments recheckLatestVersion may immediately re-populate a real
	// newer version (which is correct behaviour), so we only assert that the
	// injected fake is gone — not that update_available is false.
	t.Log("step 5: verify injected fake version v9999.0.0 was cleared")
	deadline := time.Now().Add(actionTimeout)
	cleared := false
	for time.Now().Before(deadline) {
		body := httpGet(t, "/api/services")
		svcs := decodeJSON[[]ServiceState](t, body)
		for _, svc := range svcs {
			if svc.ID == id {
				if svc.LatestVersion != "v9999.0.0" {
					cleared = true
				}
				break
			}
		}
		if cleared {
			break
		}
		time.Sleep(time.Second)
	}
	if !cleared {
		body := httpGet(t, "/api/services")
		svcs := decodeJSON[[]ServiceState](t, body)
		for _, svc := range svcs {
			if svc.ID == id {
				t.Errorf("update: latest_version is still %q after update — DeleteLatestVersion did not run", svc.LatestVersion)
				return
			}
		}
		t.Fatalf("update: service %q not found after update", id)
	}

	// Also confirm the latest_version was not left as the injected fake.
	body := httpGet(t, "/api/services")
	svcs := decodeJSON[[]ServiceState](t, body)
	for _, svc := range svcs {
		if svc.ID == id {
			if svc.LatestVersion == "v9999.0.0" {
				t.Errorf("update: latest_version is still %q after update — recheckLatestVersion did not run", svc.LatestVersion)
			}
			return
		}
	}
	t.Fatalf("update: service %q not found in services list after update", id)
}
