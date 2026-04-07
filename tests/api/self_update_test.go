//go:build integration

package apitest

import (
	"net/http"
	"testing"
	"time"
)

// TestSelfUpdate_Status verifies that GET /api/self/update/status returns the
// expected shape with all required fields.
func TestSelfUpdate_Status(t *testing.T) {
	body := httpGet(t, "/api/self/update/status")
	status := decodeJSON[selfUpdateStatus](t, body)

	// current_version must always be present (could be "dev" in test env).
	if status.CurrentVersion == "" {
		t.Error("expected current_version to be non-empty")
	}
	t.Logf("current_version=%q latest_version=%q update_available=%v",
		status.CurrentVersion, status.LatestVersion, status.UpdateAvailable)
}

// TestSelfUpdate_UpdateAvailableFlag verifies that injecting a fake latest
// version via /_testing/self/latest-version causes update_available to become
// true, and that clearing it (by injecting the same string as current_version)
// causes update_available to become false.
func TestSelfUpdate_UpdateAvailableFlag(t *testing.T) {
	const timeout = 10 * time.Second

	// Fetch current version first.
	body := httpGet(t, "/api/self/update/status")
	before := decodeJSON[selfUpdateStatus](t, body)
	t.Logf("before: current=%q latest=%q available=%v", before.CurrentVersion, before.LatestVersion, before.UpdateAvailable)

	// Inject a fake "latest" that is definitely different from current.
	injectBody, injectStatus := httpPost(t, "/_testing/self/latest-version", map[string]string{
		"version": "v9999.0.0",
	})
	if injectStatus != http.StatusOK {
		t.Fatalf("inject latest-version: expected 200, got %d: %s", injectStatus, string(injectBody))
	}

	// Poll until update_available becomes true.
	pollSelfUpdateAvailable(t, true, timeout)

	// Verify the latest_version field reflects what we injected.
	body = httpGet(t, "/api/self/update/status")
	after := decodeJSON[selfUpdateStatus](t, body)
	if after.LatestVersion != "v9999.0.0" {
		t.Errorf("latest_version: want %q, got %q", "v9999.0.0", after.LatestVersion)
	}
	if !after.UpdateAvailable {
		t.Error("update_available: want true after injecting v9999.0.0")
	}

	// Now inject the same value as current_version — update_available must clear.
	injectBody, injectStatus = httpPost(t, "/_testing/self/latest-version", map[string]string{
		"version": before.CurrentVersion,
	})
	if injectStatus != http.StatusOK {
		t.Fatalf("inject reset: expected 200, got %d: %s", injectStatus, string(injectBody))
	}

	pollSelfUpdateAvailable(t, false, timeout)
}

// TestSelfUpdate_ApplyUpdateCycle exercises the full self-update flow:
//   - inject a fake "latest" version
//   - verify update_available becomes true
//   - trigger POST /api/self/update/apply
//   - verify the SSE stream completes with a "done" event and has output lines
//   - verify update_available clears (DeleteSelfLatestVersion ran)
//   - verify devctl restarts and responds to health checks
//
// The binary download goes through the curl shim in the test environment, which
// serves the cached ./devctl binary from the artifact volume when available.
func TestSelfUpdate_ApplyUpdateCycle(t *testing.T) {
	const applyTimeout = 2 * time.Minute
	const actionTimeout = 30 * time.Second

	// ── 1. Inject a fake "latest" version ────────────────────────────────────
	t.Log("step 1: inject fake latest version v9999.0.0")
	injectBody, injectStatus := httpPost(t, "/_testing/self/latest-version", map[string]string{
		"version": "v9999.0.0",
	})
	if injectStatus != http.StatusOK {
		t.Fatalf("inject latest-version: expected 200, got %d: %s", injectStatus, string(injectBody))
	}

	// ── 2. Verify update_available becomes true ───────────────────────────────
	t.Log("step 2: wait for update_available == true")
	pollSelfUpdateAvailable(t, true, actionTimeout)

	// ── 3. Trigger update (SSE stream) ───────────────────────────────────────
	t.Log("step 3: trigger self-update via POST /api/self/update/apply")
	result := httpSSE(t, http.MethodPost, "/api/self/update/apply", applyTimeout)

	if result.LastEvent != "done" {
		t.Fatalf("apply: last SSE event = %q, want \"done\"; last data: %s", result.LastEvent, result.LastData)
	}

	// Verify we received at least one output line.
	outputCount := 0
	for _, ev := range result.Events {
		if ev == "output" {
			outputCount++
		}
	}
	if outputCount == 0 {
		t.Error("apply: expected at least one 'output' SSE event, got none")
	}
	t.Logf("step 3: received %d output events", outputCount)

	// ── 4. Verify the injected fake version was cleared ──────────────────────
	// After DeleteSelfLatestVersion the injected v9999.0.0 must be gone.
	t.Log("step 4: verify injected fake version v9999.0.0 was cleared")
	deadline := time.Now().Add(actionTimeout)
	cleared := false
	for time.Now().Before(deadline) {
		body := httpGet(t, "/api/self/update/status")
		st := decodeJSON[selfUpdateStatus](t, body)
		if st.LatestVersion != "v9999.0.0" {
			cleared = true
			break
		}
		time.Sleep(time.Second)
	}
	if !cleared {
		body := httpGet(t, "/api/self/update/status")
		st := decodeJSON[selfUpdateStatus](t, body)
		t.Errorf("apply: latest_version is still %q after update — DeleteSelfLatestVersion did not run", st.LatestVersion)
	}

	// ── 5. Wait for devctl to restart and respond ─────────────────────────────
	// systemctl restart devctl fires ~300ms after the "done" event. Give it
	// extra time since the service needs to come back up fully.
	t.Log("step 5: wait for devctl to restart and respond")
	waitForDevctlRestart(t, 30*time.Second)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// selfUpdateStatus mirrors the JSON shape of GET /api/self/update/status.
type selfUpdateStatus struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

// pollSelfUpdateAvailable polls GET /api/self/update/status until
// update_available matches want, or until timeout.
func pollSelfUpdateAvailable(t *testing.T, want bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body := httpGet(t, "/api/self/update/status")
		st := decodeJSON[selfUpdateStatus](t, body)
		if st.UpdateAvailable == want {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	body := httpGet(t, "/api/self/update/status")
	st := decodeJSON[selfUpdateStatus](t, body)
	if st.UpdateAvailable != want {
		t.Fatalf("pollSelfUpdateAvailable: want update_available=%v, got %v after %v", want, st.UpdateAvailable, timeout)
	}
}

// waitForDevctlRestart polls GET /api/self/update/status until devctl responds
// (handling the brief window when the process is being replaced). It waits up
// to timeout for the service to come back after the restart.
func waitForDevctlRestart(t *testing.T, timeout time.Duration) {
	t.Helper()
	// First, wait a moment so the restart can begin.
	time.Sleep(1 * time.Second)

	deadline := time.Now().Add(timeout)
	lastErr := ""
	for time.Now().Before(deadline) {
		_, err := http.Get(baseURL() + "/api/self/update/status") //nolint:noctx
		if err == nil {
			t.Log("devctl responded after restart")
			return
		}
		lastErr = err.Error()
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("waitForDevctlRestart: devctl did not respond within %v: %s", timeout, lastErr)
}
