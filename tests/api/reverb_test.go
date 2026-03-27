//go:build integration

package apitest

import (
	"net/http"
	"testing"
	"time"
)

// TestReverbInstall_InstallPurgeCycle exercises the full install → running → purge
// lifecycle for Laravel Reverb.
//
// This test verifies that:
//  1. The install SSE stream ends with a "done" event (not "error").
//  2. At least one "output" SSE event is emitted during install.
//  3. The reverb service appears as installed after the stream completes.
//  4. The service can be purged cleanly.
//
// The test previously failed with:
//
//	"laravel new: exit status 127"
//
// because the installer tried to run the Laravel CLI installer binary at
// ~/.config/composer/vendor/bin/laravel, which is not installed on this system.
// The correct approach is to use `composer create-project laravel/laravel reverb`
// since composer IS available at {serverRoot}/bin/composer.
func TestReverbInstall_InstallPurgeCycle(t *testing.T) {
	const id = "reverb"
	const installTimeout = 10 * time.Minute
	const purgeTimeout = 2 * time.Minute

	// ── 0. Pre-condition: ensure reverb is not already installed ──────────────
	{
		body := httpGet(t, "/api/services")
		services := decodeJSON[[]ServiceState](t, body)
		for _, svc := range services {
			if svc.ID == id && svc.Installed {
				t.Logf("pre-condition: %s already installed — purging before test", id)
				res := httpSSE(t, http.MethodDelete, "/api/services/"+id, purgeTimeout)
				if res.LastEvent != "done" {
					t.Fatalf("pre-condition purge: last event = %q, want \"done\"; last data: %s", res.LastEvent, res.LastData)
				}
				pollServiceInstalled(t, id, false, 30*time.Second)
			}
		}
	}

	// ── 1. Install ─────────────────────────────────────────────────────────────
	t.Log("step 1: install reverb")
	installResult := httpSSE(t, http.MethodPost, "/api/services/"+id+"/install", installTimeout)

	// The install must end with "done", not "error".
	// If it ends with "error" the test fails and shows the last error data.
	if installResult.LastEvent != "done" {
		t.Fatalf("install: last SSE event = %q, want \"done\"; last data: %s",
			installResult.LastEvent, installResult.LastData)
	}

	// "output" events should have been emitted during install.
	outputCount := 0
	for _, ev := range installResult.Events {
		if ev == "output" {
			outputCount++
		}
	}
	if outputCount == 0 {
		t.Error("install: expected at least one 'output' SSE event, got none")
	}

	// ── 2. Verify installed flag ───────────────────────────────────────────────
	t.Log("step 2: verify installed flag")
	pollServiceInstalled(t, id, true, 30*time.Second)

	// ── 3. Verify service auto-started and is running ─────────────────────────
	t.Log("step 3: verify service is running")
	pollServiceStatus(t, id, "running", 30*time.Second)

	// ── 4. Purge ───────────────────────────────────────────────────────────────
	t.Log("step 4: purge reverb")
	purgeResult := httpSSE(t, http.MethodDelete, "/api/services/"+id, purgeTimeout)
	if purgeResult.LastEvent != "done" {
		t.Fatalf("purge: last SSE event = %q, want \"done\"; last data: %s",
			purgeResult.LastEvent, purgeResult.LastData)
	}

	// ── 5. Verify not installed after purge ───────────────────────────────────
	t.Log("step 5: verify not installed after purge")
	pollServiceInstalled(t, id, false, 30*time.Second)
}

// TestReverbInstall_OutputEmittedOnInstall verifies that the install SSE stream
// for reverb emits at least one "output" event before any terminal event.
// This is a lighter test that does not require the full install to succeed —
// it only checks that output is streamed (not silently failed).
//
// This test specifically catches the "exit status 127" regression where the
// installer silently swallowed the error or returned it without any output events.
func TestReverbInstall_ErrorIncludesOutput(t *testing.T) {
	const id = "reverb"
	const installTimeout = 3 * time.Minute

	// Only run this test if reverb is NOT installed (we need to trigger install).
	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)
	for _, svc := range services {
		if svc.ID == id && svc.Installed {
			t.Skip("reverb is already installed — skipping error output test")
		}
	}

	// Trigger the install and consume the SSE stream.
	result := httpSSE(t, http.MethodPost, "/api/services/"+id+"/install", installTimeout)

	// Regardless of whether it succeeds or fails, we expect output events.
	outputCount := 0
	for _, ev := range result.Events {
		if ev == "output" {
			outputCount++
		}
	}
	if outputCount == 0 {
		t.Errorf("install: no 'output' SSE events received before terminal event %q (data: %s); "+
			"this suggests the installer failed before writing any output — likely a command-not-found error",
			result.LastEvent, result.LastData)
	}

	// If install succeeded, clean up.
	if result.LastEvent == "done" {
		t.Log("install succeeded — purging to restore clean state")
		purgeResult := httpSSE(t, http.MethodDelete, "/api/services/"+id, 2*time.Minute)
		if purgeResult.LastEvent != "done" {
			t.Logf("cleanup purge: last event = %q, last data: %s", purgeResult.LastEvent, purgeResult.LastData)
		}
	}
}
