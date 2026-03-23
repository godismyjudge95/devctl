//go:build integration

package apitest

import (
	"testing"
)

// resolvedSettingsKeys lists all keys that must be present in the response
// from GET /api/settings/resolved regardless of any user-saved overrides.
var resolvedSettingsKeys = []string{
	"devctl_host",
	"devctl_port",
	"dump_tcp_port",
	"service_poll_interval",
	"mailpit_http_port",
	"mailpit_smtp_port",
	"dns_port",
	"dns_target_ip",
	"dns_tld",
}

// TestGetSettings_StatusOK verifies that GET /api/settings returns HTTP 200.
func TestGetSettings_StatusOK(t *testing.T) {
	httpGet(t, "/api/settings")
}

// TestGetSettings_UnmarshalsToMap verifies the response can be decoded into a
// map[string]string. An empty object ({}) is valid in a fresh container where
// no settings have been saved yet.
func TestGetSettings_UnmarshalsToMap(t *testing.T) {
	body := httpGet(t, "/api/settings")
	_ = decodeJSON[map[string]string](t, body)
}

// TestGetSettingsResolved_StatusOK verifies that GET /api/settings/resolved
// returns HTTP 200.
func TestGetSettingsResolved_StatusOK(t *testing.T) {
	httpGet(t, "/api/settings/resolved")
}

// TestGetSettingsResolved_UnmarshalsToMap verifies the resolved settings
// response can be decoded into a map[string]string.
func TestGetSettingsResolved_UnmarshalsToMap(t *testing.T) {
	body := httpGet(t, "/api/settings/resolved")
	_ = decodeJSON[map[string]string](t, body)
}

// TestGetSettingsResolved_ContainsRequiredKeys verifies that all expected
// default keys are present in the resolved settings response.
func TestGetSettingsResolved_ContainsRequiredKeys(t *testing.T) {
	body := httpGet(t, "/api/settings/resolved")
	settings := decodeJSON[map[string]string](t, body)

	for _, key := range resolvedSettingsKeys {
		if _, ok := settings[key]; !ok {
			t.Errorf("GET /api/settings/resolved: missing key %q", key)
		}
	}
}

// TestGetSettingsResolved_DevctlPortIsDefault verifies that devctl_port is set
// to the expected default value of "4000" in a standard installation.
func TestGetSettingsResolved_DevctlPortIsDefault(t *testing.T) {
	body := httpGet(t, "/api/settings/resolved")
	settings := decodeJSON[map[string]string](t, body)

	port, ok := settings["devctl_port"]
	if !ok {
		t.Fatal("GET /api/settings/resolved: key \"devctl_port\" not found")
	}
	if port != "4000" {
		t.Errorf("GET /api/settings/resolved: devctl_port = %q, want \"4000\"", port)
	}
}

// TestGetSettingsResolved_DevctlHostPresent verifies that devctl_host is
// present in the resolved settings response.
func TestGetSettingsResolved_DevctlHostPresent(t *testing.T) {
	body := httpGet(t, "/api/settings/resolved")
	settings := decodeJSON[map[string]string](t, body)

	if _, ok := settings["devctl_host"]; !ok {
		t.Error("GET /api/settings/resolved: key \"devctl_host\" not found")
	}
}

// TestGetSettingsResolved_DNSTLDPresent verifies that dns_tld is present in
// the resolved settings response.
func TestGetSettingsResolved_DNSTLDPresent(t *testing.T) {
	body := httpGet(t, "/api/settings/resolved")
	settings := decodeJSON[map[string]string](t, body)

	if _, ok := settings["dns_tld"]; !ok {
		t.Error("GET /api/settings/resolved: key \"dns_tld\" not found")
	}
}
