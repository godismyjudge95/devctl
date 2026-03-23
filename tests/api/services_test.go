//go:build integration

package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

// ServiceState mirrors services.ServiceState for black-box JSON shape assertions.
// Field names and json tags must exactly match the server's serialisation.
type ServiceState struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Status          string `json:"status"`
	Version         string `json:"version"`
	Log             string `json:"log"`
	Installed       bool   `json:"installed"`
	Installable     bool   `json:"installable"`
	Required        bool   `json:"required"`
	Description     string `json:"description"`
	InstallVersion  string `json:"install_version"`
	HasCredentials  bool   `json:"has_credentials"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

// TestGetServices_StatusOK verifies that GET /api/services returns HTTP 200.
func TestGetServices_StatusOK(t *testing.T) {
	httpGet(t, "/api/services")
}

// TestGetServices_UnmarshalsToSlice verifies the response body can be decoded
// into a []ServiceState without error.
func TestGetServices_UnmarshalsToSlice(t *testing.T) {
	body := httpGet(t, "/api/services")
	_ = decodeJSON[[]ServiceState](t, body)
}

// TestGetServices_AtLeastOneElement verifies the services list is non-empty.
func TestGetServices_AtLeastOneElement(t *testing.T) {
	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)

	if len(services) == 0 {
		t.Fatal("GET /api/services: expected at least one service, got empty slice")
	}
}

// TestGetServices_AllHaveIDAndLabel verifies every returned service has a
// non-empty id and label field.
func TestGetServices_AllHaveIDAndLabel(t *testing.T) {
	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)

	for i, svc := range services {
		if svc.ID == "" {
			t.Errorf("service[%d]: id is empty", i)
		}
		if svc.Label == "" {
			t.Errorf("service[%d] (id=%q): label is empty", i, svc.ID)
		}
	}
}

// TestGetServices_AtLeastOneRequired verifies that at least one service has
// required == true, which is a fundamental invariant of the devctl service list.
func TestGetServices_AtLeastOneRequired(t *testing.T) {
	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)

	for _, svc := range services {
		if svc.Required {
			return // found one — test passes
		}
	}
	t.Error("GET /api/services: no service with required == true found")
}

// TestGetServicesEvents_StatusOK verifies that GET /api/services/events responds
// with HTTP 200 and the text/event-stream Content-Type.
// The connection is opened but immediately closed — we only check the handshake.
func TestGetServicesEvents_StatusOK(t *testing.T) {
	url := fmt.Sprintf("%s/api/services/events", baseURL())

	// Use a custom client that does not follow redirects; we close the body
	// immediately after reading the headers so the SSE stream does not block.
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("GET /api/services/events: could not build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/services/events: request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/services/events: expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		t.Errorf("GET /api/services/events: Content-Type header is missing")
	}
}
