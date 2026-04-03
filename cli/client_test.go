package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// ---------------------------------------------------------------------------
// StreamLog
// ---------------------------------------------------------------------------

// TestStreamLog_DecodesSSELines verifies that StreamLog correctly extracts the
// JSON-encoded string payload from SSE data lines and writes them to the writer.
func TestStreamLog_DecodesSSELines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Two SSE data lines followed by a blank line (standard SSE format).
		_, _ = w.Write([]byte("data: \"line one\\n\"\n"))
		_, _ = w.Write([]byte("data: \"line two\\n\"\n"))
		_, _ = w.Write([]byte("\n"))
		// Flush so the client receives both lines before the handler returns.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c := &Client{base: srv.URL, http: srv.Client()}
	var buf bytes.Buffer
	err := c.StreamLog(ctx, "caddy", &buf)
	// Context cancellation or EOF both result in nil error.
	if err != nil {
		t.Fatalf("StreamLog returned unexpected error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "line one") {
		t.Errorf("output missing 'line one'; got: %q", got)
	}
	if !strings.Contains(got, "line two") {
		t.Errorf("output missing 'line two'; got: %q", got)
	}
}

// TestStreamLog_CancelContext verifies that StreamLog returns nil (not an error)
// when its context is cancelled by the caller.
func TestStreamLog_CancelContext(t *testing.T) {
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(ready)
		// Block until the client disconnects.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{base: srv.URL, http: srv.Client()}

	done := make(chan error, 1)
	go func() {
		done <- c.StreamLog(ctx, "caddy", &bytes.Buffer{})
	}()

	// Wait for the handler to be reached, then cancel.
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server to be reached")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("StreamLog returned error after cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("StreamLog did not return after context cancellation")
	}
}

// TestStreamLog_404ReturnsError verifies that StreamLog returns a non-nil error
// when the server responds with a 404 (unknown log ID).
func TestStreamLog_404ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "log not found", http.StatusNotFound)
	}))
	defer srv.Close()

	ctx := context.Background()
	c := &Client{base: srv.URL, http: srv.Client()}
	err := c.StreamLog(ctx, "does-not-exist", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected non-nil error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "log not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// logs:tail handler — flag parsing with positional arg before flag
// ---------------------------------------------------------------------------

// TestLogsTail_FollowFlagAfterPositionalArg verifies that `devctl logs:tail
// <log-id> -f` (flag after the positional argument) correctly activates
// follow mode and calls the SSE stream endpoint rather than the tail endpoint.
//
// This is a regression test for a bug where Go's flag.FlagSet stops parsing
// at the first non-flag argument, so "-f" was silently ignored when it came
// after the log-id.
func TestLogsTail_FollowFlagAfterPositionalArg(t *testing.T) {
	// StreamLog calls GET /api/logs/<id>  (no /tail suffix).
	// GetLogTail calls  GET /api/logs/<id>/tail.
	// We detect which path is hit to know whether follow mode activated.
	streamHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tail") {
			streamHit = true
			// Return a minimal SSE response so StreamLog can exit cleanly.
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Let the handler return to cause EOF, which StreamLog treats as
			// a clean exit.
			return
		}
		// /tail path — non-follow mode was used instead.
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("some log line\n"))
	}))
	defer srv.Close()

	cmd := Find("logs:tail")
	if cmd == nil {
		t.Fatal("logs:tail command not registered")
	}

	c := &Client{base: srv.URL, http: srv.Client()}
	// Simulate: devctl logs:tail php-fpm-8.4 -f
	// The bug: flag.FlagSet stops at "php-fpm-8.4" and never parses "-f".
	err := cmd.Handler(c, []string{"php-fpm-8.4", "-f"}, false)
	if err != nil {
		t.Fatalf("Handler returned unexpected error: %v", err)
	}
	if !streamHit {
		t.Error("expected SSE /stream endpoint to be called (follow mode), but it was not; " +
			"-f flag was not parsed when it came after the positional argument")
	}
}
