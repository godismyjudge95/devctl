//go:build integration

package apitest

import (
	"testing"
)

// DumpEntry mirrors the JSON shape returned by GET /api/dumps.
type DumpEntry struct {
	ID      int    `json:"id"`
	Domain  string `json:"domain"`
	Payload string `json:"payload"`
}

// TestGetDumps_StatusOK verifies GET /api/dumps returns HTTP 200.
func TestGetDumps_StatusOK(t *testing.T) {
	httpGet(t, "/api/dumps")
}

// TestGetDumps_ReturnsSlice verifies the response is a JSON array.
func TestGetDumps_ReturnsSlice(t *testing.T) {
	body := httpGet(t, "/api/dumps")
	_ = decodeJSON[[]DumpEntry](t, body)
}

// TestClearDumps_StatusOK verifies DELETE /api/dumps returns HTTP 200.
func TestClearDumps_StatusOK(t *testing.T) {
	_, status := httpDelete(t, "/api/dumps")
	if status != 200 {
		t.Fatalf("DELETE /api/dumps: expected 200, got %d", status)
	}
}

// TestClearDumps_EmptiesTheList verifies that after DELETE /api/dumps the
// list is empty.
func TestClearDumps_EmptiesTheList(t *testing.T) {
	_, status := httpDelete(t, "/api/dumps")
	if status != 200 {
		t.Fatalf("DELETE /api/dumps: expected 200, got %d", status)
	}

	body := httpGet(t, "/api/dumps")
	dumps := decodeJSON[[]DumpEntry](t, body)
	if len(dumps) != 0 {
		t.Errorf("GET /api/dumps after clear: expected 0 entries, got %d", len(dumps))
	}
}
