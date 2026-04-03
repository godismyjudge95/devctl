package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// ListDumps — the API returns a plain JSON array, NOT a {dumps:[],total:N} object.
// ---------------------------------------------------------------------------

// TestListDumps_ParsesPlainArray verifies that ListDumps correctly unmarshals
// the plain []Dump array returned by GET /api/dumps.
func TestListDumps_ParsesPlainArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// This is exactly what the real API returns — a plain array.
		_, _ = w.Write([]byte(`[{"id":1,"file":"app.php","line":42,"nodes":"\"hello\"","timestamp":1700000000,"site_domain":"myapp.test"}]`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	dumps, err := c.ListDumps("")
	if err != nil {
		t.Fatalf("ListDumps returned error: %v", err)
	}
	if len(dumps) != 1 {
		t.Fatalf("expected 1 dump, got %d", len(dumps))
	}
	if dumps[0].ID != 1 {
		t.Errorf("dump ID: got %d, want 1", dumps[0].ID)
	}
}

// TestListDumps_EmptyArray verifies that an empty array is handled without error.
func TestListDumps_EmptyArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	dumps, err := c.ListDumps("")
	if err != nil {
		t.Fatalf("ListDumps returned error: %v", err)
	}
	if len(dumps) != 0 {
		t.Errorf("expected 0 dumps, got %d", len(dumps))
	}
}
