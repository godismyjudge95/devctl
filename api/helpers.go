package api

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/tools"
)

func (s *Server) handleGetHelpers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, tools.States(r.Context(), paths.BinDir(s.serverRoot), s.versions.Snapshot()))
}

func (s *Server) handleHelperInstall(w http.ResponseWriter, r *http.Request) {
	s.streamHelperEnsure(w, r, false)
}

func (s *Server) handleHelperUpdate(w http.ResponseWriter, r *http.Request) {
	s.streamHelperEnsure(w, r, true)
}

func (s *Server) streamHelperEnsure(w http.ResponseWriter, r *http.Request, mustExist bool) {
	id := r.PathValue("id")
	t, ok := tools.Lookup(id)
	if !ok {
		writeError(w, fmt.Sprintf("helper %q not found", id), http.StatusNotFound)
		return
	}

	binDir := paths.BinDir(s.serverRoot)
	states := tools.States(r.Context(), binDir, s.versions.Snapshot())
	var st tools.State
	for _, row := range states {
		if row.ID == id {
			st = row
			break
		}
	}
	if mustExist && !st.Installed {
		writeError(w, fmt.Sprintf("helper %q is not installed", id), http.StatusConflict)
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
	if err := tools.EnsureLatest(r.Context(), t, binDir, pw); err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	s.DeleteLatestVersion(id)
	sendSSE(w, flusher, "done", map[string]string{"status": "ok"})
	go s.recheckHelperLatestVersion(context.Background(), t)
}

func (s *Server) handleHelperUninstall(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, ok := tools.Lookup(id)
	if !ok {
		writeError(w, fmt.Sprintf("helper %q not found", id), http.StatusNotFound)
		return
	}
	if err := tools.Uninstall(t, paths.BinDir(s.serverRoot)); err != nil {
		code := http.StatusInternalServerError
		if t.Default {
			code = http.StatusForbidden
		}
		writeError(w, err.Error(), code)
		return
	}
	s.DeleteLatestVersion(id)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) recheckHelperLatestVersion(ctx context.Context, t tools.Tool) {
	if t.LatestRelease == nil {
		return
	}
	rel, err := t.LatestRelease(ctx)
	if err != nil {
		log.Printf("helper-update-checker: recheck %s: %v", t.Name, err)
		return
	}
	s.SetLatestVersion(t.Name, rel.Version)
}

// RecheckHelpersLatest fetches upstream versions for every helper and stores
// them in the shared version cache. Called on startup and once a day.
func (s *Server) RecheckHelpersLatest(ctx context.Context) {
	for _, t := range tools.AllTools {
		if t.LatestRelease == nil {
			continue
		}
		rel, err := t.LatestRelease(ctx)
		if err != nil {
			log.Printf("helper-update-checker: %s: %v", t.Name, err)
			continue
		}
		s.SetLatestVersion(t.Name, rel.Version)
	}
}
