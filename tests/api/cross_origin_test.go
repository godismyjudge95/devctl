//go:build integration

package apitest

import (
	"net/http"
	"strings"
	"testing"
)

// The body is not JSON, so a request that gets past the origin check stops
// in the handler with 400 and runs no SQL.
func postDBQuery(t *testing.T, header map[string]string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL()+"/api/databases/sqlite/query", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/databases/sqlite/query: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestCrossOriginPostToDBQueryIsForbidden(t *testing.T) {
	got := postDBQuery(t, map[string]string{
		"Origin":         "https://evil.example",
		"Sec-Fetch-Site": "cross-site",
	})
	if got != http.StatusForbidden {
		t.Fatalf("cross-site POST: status = %d, want 403", got)
	}
}

func TestPostToDBQueryWithoutBrowserHeadersReachesHandler(t *testing.T) {
	got := postDBQuery(t, nil)
	if got != http.StatusBadRequest {
		t.Fatalf("POST without Origin: status = %d, want 400 from the handler", got)
	}
}
