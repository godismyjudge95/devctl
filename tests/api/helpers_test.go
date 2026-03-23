//go:build integration

package apitest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// baseURL returns the base URL for the devctl API, reading DEVCTL_BASE_URL
// from the environment and defaulting to http://127.0.0.1:4000.
func baseURL() string {
	if u := os.Getenv("DEVCTL_BASE_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:4000"
}

// httpGet performs a GET request to the given path (relative to baseURL),
// asserts the response status is 200 OK, and returns the response body bytes.
// The test is marked as failed and stopped immediately on any error.
func httpGet(t *testing.T, path string) []byte {
	t.Helper()
	url := fmt.Sprintf("%s%s", baseURL(), path)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("GET %s: request failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: expected status 200, got %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: failed to read body: %v", url, err)
	}
	return body
}

// httpGetStatus performs a GET request to the given path and asserts the
// response status matches wantStatus. It returns the response body bytes.
func httpGetStatus(t *testing.T, path string, wantStatus int) []byte {
	t.Helper()
	url := fmt.Sprintf("%s%s", baseURL(), path)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("GET %s: request failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s: expected status %d, got %d", url, wantStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: failed to read body: %v", url, err)
	}
	return body
}

// httpPost performs a POST request to the given path with an optional JSON body.
// jsonBody may be nil for requests with no body.
// Returns the response body bytes and the HTTP status code.
func httpPost(t *testing.T, path string, jsonBody any) ([]byte, int) {
	t.Helper()
	url := fmt.Sprintf("%s%s", baseURL(), path)

	var bodyReader io.Reader
	if jsonBody != nil {
		encoded, err := json.Marshal(jsonBody)
		if err != nil {
			t.Fatalf("POST %s: marshal body: %v", url, err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(http.MethodPost, url, bodyReader)
	if err != nil {
		t.Fatalf("POST %s: build request: %v", url, err)
	}
	if jsonBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: request failed: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("POST %s: read body: %v", url, err)
	}
	return body, resp.StatusCode
}

// httpPut performs a PUT request with a JSON body. Returns body + status code.
func httpPut(t *testing.T, path string, jsonBody any) ([]byte, int) {
	t.Helper()
	url := fmt.Sprintf("%s%s", baseURL(), path)

	encoded, err := json.Marshal(jsonBody)
	if err != nil {
		t.Fatalf("PUT %s: marshal body: %v", url, err)
	}

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("PUT %s: build request: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: request failed: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("PUT %s: read body: %v", url, err)
	}
	return body, resp.StatusCode
}

// httpDelete performs a DELETE request to the given path.
// Returns the response body bytes and the HTTP status code.
func httpDelete(t *testing.T, path string) ([]byte, int) {
	t.Helper()
	url := fmt.Sprintf("%s%s", baseURL(), path)

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("DELETE %s: build request: %v", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: request failed: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("DELETE %s: read body: %v", url, err)
	}
	return body, resp.StatusCode
}

// SSEResult holds the outcome of consuming an SSE stream.
type SSEResult struct {
	// LastEvent is the final event name received (e.g. "done" or "error").
	LastEvent string
	// Events is the ordered list of all event names received.
	Events []string
	// LastData is the data payload of the last event.
	LastData string
}

// httpSSE opens an SSE stream via the given method and path, reads until the
// stream closes or a "done"/"error" event is received, and returns the result.
// timeout controls how long to wait for the stream to complete.
func httpSSE(t *testing.T, method, path string, timeout time.Duration) SSEResult {
	t.Helper()
	url := fmt.Sprintf("%s%s", baseURL(), path)

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("SSE %s %s: build request: %v", method, url, err)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("SSE %s %s: request failed: %v", method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("SSE %s %s: expected status 200, got %d: %s", method, url, resp.StatusCode, string(body))
	}

	var result SSEResult
	scanner := bufio.NewScanner(resp.Body)
	var currentEvent, currentData string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentData = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentEvent != "" {
			// Blank line = end of event
			result.Events = append(result.Events, currentEvent)
			result.LastEvent = currentEvent
			result.LastData = currentData
			if currentEvent == "done" || currentEvent == "error" {
				return result
			}
			currentEvent = ""
			currentData = ""
		}
	}

	return result
}

// pollServiceStatus polls GET /api/services until the named service reaches
// the expected status, or until the timeout is exceeded.
func pollServiceStatus(t *testing.T, id, wantStatus string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body := httpGet(t, "/api/services")
		services := decodeJSON[[]ServiceState](t, body)
		for _, svc := range services {
			if svc.ID == id {
				if svc.Status == wantStatus {
					return
				}
				break
			}
		}
		time.Sleep(time.Second)
	}
	// Final check with assertion
	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)
	for _, svc := range services {
		if svc.ID == id {
			if svc.Status != wantStatus {
				t.Fatalf("pollServiceStatus: service %q: want status %q, got %q after %v", id, wantStatus, svc.Status, timeout)
			}
			return
		}
	}
	t.Fatalf("pollServiceStatus: service %q not found in services list", id)
}

// pollServiceInstalled polls GET /api/services until the named service's
// installed field matches want, or until timeout.
func pollServiceInstalled(t *testing.T, id string, want bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body := httpGet(t, "/api/services")
		services := decodeJSON[[]ServiceState](t, body)
		for _, svc := range services {
			if svc.ID == id {
				if svc.Installed == want {
					return
				}
				break
			}
		}
		time.Sleep(time.Second)
	}
	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)
	for _, svc := range services {
		if svc.ID == id {
			if svc.Installed != want {
				t.Fatalf("pollServiceInstalled: service %q: want installed=%v, got installed=%v after %v", id, want, svc.Installed, timeout)
			}
			return
		}
	}
	t.Fatalf("pollServiceInstalled: service %q not found in services list", id)
}

// decodeJSON unmarshals body into a value of type T.
// The test is marked as failed and stopped immediately on any parse error.
func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("JSON unmarshal into %T: %v\nbody: %s", v, err, string(body))
	}
	return v
}
