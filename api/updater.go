package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/danielgormly/devctl/selfupdate"
)

// ---------------------------------------------------------------------------
// Self-update status helpers
// ---------------------------------------------------------------------------

// SetSelfLatestVersion stores the latest fetched devctl release version.
// Safe for concurrent use.
func (s *Server) SetSelfLatestVersion(version string) {
	s.latestSelfVersionMu.Lock()
	s.latestSelfVersion = version
	s.latestSelfVersionMu.Unlock()
}

// GetSelfLatestVersion returns the cached latest devctl release version, or "".
// Safe for concurrent use.
func (s *Server) GetSelfLatestVersion() string {
	s.latestSelfVersionMu.RLock()
	v := s.latestSelfVersion
	s.latestSelfVersionMu.RUnlock()
	return v
}

// DeleteSelfLatestVersion clears the cached latest devctl release version.
// Called after a successful update so the badge clears immediately.
func (s *Server) DeleteSelfLatestVersion() {
	s.latestSelfVersionMu.Lock()
	s.latestSelfVersion = ""
	s.latestSelfVersionMu.Unlock()
}

// selfUpdateAvailable returns true when a newer release is known and the
// current binary is not a dev build.
func (s *Server) selfUpdateAvailable() bool {
	latest := s.GetSelfLatestVersion()
	return latest != "" && latest != s.selfVersion
}

// ---------------------------------------------------------------------------
// GET /api/self/update/status
// ---------------------------------------------------------------------------

type selfUpdateStatus struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

func (s *Server) handleGetSelfUpdateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, selfUpdateStatus{
		CurrentVersion:  s.selfVersion,
		LatestVersion:   s.GetSelfLatestVersion(),
		UpdateAvailable: s.selfUpdateAvailable(),
	})
}

// ---------------------------------------------------------------------------
// POST /api/self/update/apply  (SSE stream)
// ---------------------------------------------------------------------------

// handleApplySelfUpdate downloads the latest devctl binary, replaces the
// current binary, and schedules a systemctl restart.
//
// SSE events:
//
//	output — a line of progress text (JSON string)
//	done   — {"status":"ok"} on success
//	error  — {"error":"..."} on failure
func (s *Server) handleApplySelfUpdate(w http.ResponseWriter, r *http.Request) {
	version := s.GetSelfLatestVersion()
	if version == "" {
		writeError(w, "no latest version available — run a version check first", http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	pw := &sseLineWriter{w: w, flusher: flusher, event: "output"}

	// Resolve the current binary path.
	binaryPath, err := os.Executable()
	if err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": fmt.Sprintf("resolve binary path: %v", err)})
		return
	}

	if err := selfupdate.Update(r.Context(), binaryPath, version, pw); err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	// Clear the stale cached version BEFORE sending "done" so clients see
	// update_available=false immediately.
	s.DeleteSelfLatestVersion()

	sendSSE(w, flusher, "done", map[string]string{"status": "ok"})

	// Schedule a service restart after the HTTP response has been flushed.
	// Same pattern as api/restart.go.
	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := exec.Command("systemctl", "restart", "devctl").Run(); err != nil {
			log.Printf("selfupdate: systemctl restart: %v", err)
		}
	}()
}

// ---------------------------------------------------------------------------
// recheckSelfLatestVersion — called after a successful update
// ---------------------------------------------------------------------------

// recheckSelfLatestVersion fetches the latest devctl release version and
// updates the in-memory cache. Called as a goroutine after a successful update
// so the badge reflects the true post-update state.
func (s *Server) recheckSelfLatestVersion(ctx context.Context) {
	latest, err := selfupdate.LatestVersion(ctx)
	if err != nil {
		log.Printf("selfupdate: recheck: %v", err)
		return
	}
	s.SetSelfLatestVersion(latest)
}

// ---------------------------------------------------------------------------
// /_testing/self/latest-version — inject a fake version (DEVCTL_TESTING only)
// ---------------------------------------------------------------------------

func (s *Server) handleTestingSetSelfLatestVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Version == "" {
		http.Error(w, "version is required", http.StatusBadRequest)
		return
	}
	s.SetSelfLatestVersion(body.Version)
	writeJSON(w, map[string]string{"status": "ok", "version": body.Version})
}
