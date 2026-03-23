//go:build integration

package apitest

import (
	"bytes"
	"net/http"
	"testing"
)

// TestSettings_PutGet_RoundTrip verifies that a PUT /api/settings value can
// be read back via GET /api/settings with the same key+value.
func TestSettings_PutGet_RoundTrip(t *testing.T) {
	const key = "devctl_test_key"
	const value = "devctl_test_value_go"

	// ── 1. Save a custom setting ──────────────────────────────────────────────
	putBody, putStatus := httpPut(t, "/api/settings", map[string]string{
		key: value,
	})
	if putStatus != http.StatusOK {
		t.Fatalf("PUT /api/settings: expected status 200, got %d: %s", putStatus, string(putBody))
	}

	resp := decodeJSON[map[string]string](t, putBody)
	if resp["status"] != "ok" {
		t.Errorf("PUT /api/settings: response status = %q, want \"ok\"", resp["status"])
	}

	// ── 2. Read back via GET /api/settings ───────────────────────────────────
	getBody := httpGet(t, "/api/settings")
	settings := decodeJSON[map[string]string](t, getBody)

	if got, ok := settings[key]; !ok {
		t.Errorf("GET /api/settings: key %q not found after PUT", key)
	} else if got != value {
		t.Errorf("GET /api/settings: key %q = %q, want %q", key, got, value)
	}

	// ── 3. Key also appears in /api/settings/resolved ─────────────────────────
	resolvedBody := httpGet(t, "/api/settings/resolved")
	resolved := decodeJSON[map[string]string](t, resolvedBody)
	if got, ok := resolved[key]; !ok {
		t.Errorf("GET /api/settings/resolved: key %q not found after PUT", key)
	} else if got != value {
		t.Errorf("GET /api/settings/resolved: key %q = %q, want %q", key, got, value)
	}

	// ── 4. Cleanup: delete the test key by setting it empty ───────────────────
	httpPut(t, "/api/settings", map[string]string{key: ""}) //nolint:errcheck
}

// TestSettings_PutGet_MultiKey verifies that multiple keys can be saved and
// read back in a single PUT/GET round-trip.
func TestSettings_PutGet_MultiKey(t *testing.T) {
	input := map[string]string{
		"devctl_test_key_a": "alpha",
		"devctl_test_key_b": "beta",
	}

	_, putStatus := httpPut(t, "/api/settings", input)
	if putStatus != http.StatusOK {
		t.Fatalf("PUT /api/settings (multi-key): expected 200, got %d", putStatus)
	}

	getBody := httpGet(t, "/api/settings")
	settings := decodeJSON[map[string]string](t, getBody)

	for k, want := range input {
		if got, ok := settings[k]; !ok {
			t.Errorf("GET /api/settings: key %q missing after multi-key PUT", k)
		} else if got != want {
			t.Errorf("GET /api/settings: key %q = %q, want %q", k, got, want)
		}
	}

	// Cleanup.
	cleanup := make(map[string]string)
	for k := range input {
		cleanup[k] = ""
	}
	httpPut(t, "/api/settings", cleanup) //nolint:errcheck
}

// TestSettings_Put_InvalidBody_Returns400 verifies that a malformed JSON body
// returns HTTP 400.
func TestSettings_Put_InvalidBody_Returns400(t *testing.T) {
	url := baseURL() + "/api/settings"

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBufferString("{not valid json"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/settings with bad body: request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("PUT /api/settings with bad body: expected 400, got %d", resp.StatusCode)
	}
}

// TestSettings_Put_UpdateAndRestore exercises a realistic workflow: read the
// current dns_tld, change it, verify the change, then restore the original value.
func TestSettings_Put_UpdateAndRestore(t *testing.T) {
	const key = "dns_tld"

	// ── 1. Read current value ─────────────────────────────────────────────────
	resolvedBody := httpGet(t, "/api/settings/resolved")
	resolved := decodeJSON[map[string]string](t, resolvedBody)
	original, ok := resolved[key]
	if !ok {
		t.Fatalf("GET /api/settings/resolved: key %q not found", key)
	}

	// ── 2. Update to a test value ─────────────────────────────────────────────
	const testValue = ".gotest"
	_, putStatus := httpPut(t, "/api/settings", map[string]string{key: testValue})
	if putStatus != http.StatusOK {
		t.Fatalf("PUT /api/settings: expected 200, got %d", putStatus)
	}

	// ── 3. Verify the change ──────────────────────────────────────────────────
	resolvedBody2 := httpGet(t, "/api/settings/resolved")
	resolved2 := decodeJSON[map[string]string](t, resolvedBody2)
	if got := resolved2[key]; got != testValue {
		t.Errorf("after PUT: %s = %q, want %q", key, got, testValue)
	}

	// ── 4. Restore original ───────────────────────────────────────────────────
	_, restoreStatus := httpPut(t, "/api/settings", map[string]string{key: original})
	if restoreStatus != http.StatusOK {
		t.Fatalf("restore: PUT /api/settings: expected 200, got %d", restoreStatus)
	}

	resolvedBody3 := httpGet(t, "/api/settings/resolved")
	resolved3 := decodeJSON[map[string]string](t, resolvedBody3)
	if got := resolved3[key]; got != original {
		t.Errorf("after restore: %s = %q, want %q", key, got, original)
	}
}
