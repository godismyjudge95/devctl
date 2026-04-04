//go:build integration

package apitest

import (
	"testing"
)

// SpxProfile mirrors the JSON shape returned by GET /api/spx/profiles.
type SpxProfile struct {
	Key      string  `json:"key"`
	Endpoint string  `json:"endpoint"`
	WallTime float64 `json:"wall_time_ms"`
}

// TestGetSpxProfiles_StatusOK verifies GET /api/spx/profiles returns HTTP 200.
func TestGetSpxProfiles_StatusOK(t *testing.T) {
	httpGet(t, "/api/spx/profiles")
}

// TestGetSpxProfiles_ReturnsSlice verifies the response is a JSON array.
func TestGetSpxProfiles_ReturnsSlice(t *testing.T) {
	body := httpGet(t, "/api/spx/profiles")
	_ = decodeJSON[[]SpxProfile](t, body)
}

// TestGetSpxProfile_UnknownKey_Returns404 verifies that requesting a
// non-existent profile key returns HTTP 404.
func TestGetSpxProfile_UnknownKey_Returns404(t *testing.T) {
	httpGetStatus(t, "/api/spx/profiles/does-not-exist", 404)
}

// TestClearSpxProfiles_StatusOK verifies DELETE /api/spx/profiles returns 204.
func TestClearSpxProfiles_StatusOK(t *testing.T) {
	_, status := httpDelete(t, "/api/spx/profiles")
	if status != 204 {
		t.Fatalf("DELETE /api/spx/profiles: expected 204, got %d", status)
	}
}
