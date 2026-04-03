//go:build integration

package apitest

import (
	"testing"
)

// LogEntry mirrors the JSON shape returned by GET /api/logs.
type LogEntry struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

// TestGetLogs_StatusOK verifies GET /api/logs returns HTTP 200.
func TestGetLogs_StatusOK(t *testing.T) {
	httpGet(t, "/api/logs")
}

// TestGetLogs_ReturnsSlice verifies the response is a JSON array.
func TestGetLogs_ReturnsSlice(t *testing.T) {
	body := httpGet(t, "/api/logs")
	_ = decodeJSON[[]LogEntry](t, body)
}

// TestGetLogs_AtLeastOnEntry verifies the log list contains at least one entry.
func TestGetLogs_AtLeastOneEntry(t *testing.T) {
	body := httpGet(t, "/api/logs")
	logs := decodeJSON[[]LogEntry](t, body)
	if len(logs) == 0 {
		t.Fatal("GET /api/logs: expected at least one log entry")
	}
}

// TestGetLogs_AllHaveIDAndLabel verifies every log entry has id and label.
func TestGetLogs_AllHaveIDAndLabel(t *testing.T) {
	body := httpGet(t, "/api/logs")
	logs := decodeJSON[[]LogEntry](t, body)
	for i, l := range logs {
		if l.ID == "" {
			t.Errorf("log[%d]: id is empty", i)
		}
		if l.Label == "" {
			t.Errorf("log[%d] (id=%q): label is empty", i, l.ID)
		}
	}
}

// TestGetLogTail_StatusOK verifies GET /api/logs/{id}/tail returns HTTP 200
// for a valid log ID. Uses "caddy" which is always present.
func TestGetLogTail_StatusOK(t *testing.T) {
	httpGet(t, "/api/logs/caddy/tail")
}

// TestGetLogTail_UnknownID_Returns404 verifies that requesting the tail for a
// non-existent log ID returns HTTP 404.
func TestGetLogTail_UnknownID_Returns404(t *testing.T) {
	httpGetStatus(t, "/api/logs/does-not-exist/tail", 404)
}
