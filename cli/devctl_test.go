package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSelfUpdateDaemon answers the check endpoint with latest and records
// whether the CLI asked to install.
func fakeSelfUpdateDaemon(t *testing.T, checkStatus int, latest string) (*Client, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /api/self/update/check":
			if checkStatus != http.StatusOK {
				http.Error(w, `{"error":"selfupdate: github version check: HTTP 403"}`, checkStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"current_version":"v0.19.0","latest_version":"` + latest + `","update_available":` + map[bool]string{true: "true", false: "false"}[latest != "v0.19.0"] + `}`))
		case "POST /api/self/update/apply":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("event: done\ndata: {\"status\":\"ok\"}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{base: srv.URL, http: srv.Client()}, &calls
}

func TestDevctlUpdate_ChecksNowAndInstalls(t *testing.T) {
	c, calls := fakeSelfUpdateDaemon(t, http.StatusOK, "v0.19.1")
	if err := Find("devctl:update").Handler(c, nil, false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*calls, ","); got != "POST /api/self/update/check,POST /api/self/update/apply" {
		t.Errorf("calls = %s, want a check then an install", got)
	}
}

func TestDevctlUpdate_CheckFlagDoesNotInstall(t *testing.T) {
	c, calls := fakeSelfUpdateDaemon(t, http.StatusOK, "v0.19.1")
	if err := Find("devctl:update").Handler(c, []string{"--check"}, true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*calls, ","); got != "POST /api/self/update/check" {
		t.Errorf("calls = %s, want only the check", got)
	}
}

func TestDevctlUpdate_UpToDateDoesNotInstall(t *testing.T) {
	c, calls := fakeSelfUpdateDaemon(t, http.StatusOK, "v0.19.0")
	if err := Find("devctl:update").Handler(c, nil, false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*calls, ","); got != "POST /api/self/update/check" {
		t.Errorf("calls = %s, want only the check", got)
	}
}

func TestDevctlUpdate_CheckFailureIsAnError(t *testing.T) {
	c, calls := fakeSelfUpdateDaemon(t, http.StatusBadGateway, "")
	err := Find("devctl:update").Handler(c, nil, false)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("err = %v, want the GitHub error", err)
	}
	if len(*calls) != 1 {
		t.Errorf("calls = %v, want only the check", *calls)
	}
}
