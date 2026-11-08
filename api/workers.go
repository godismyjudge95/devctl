package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgormly/devctl/workers"
)

func writeWorkerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workers.ErrNotFound):
		writeError(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, workers.ErrInvalid):
		writeError(w, err.Error(), http.StatusBadRequest)
	default:
		writeError(w, err.Error(), http.StatusInternalServerError)
	}
}

// GET /api/workers?site_id=<id>
func (s *Server) handleListWorkers(w http.ResponseWriter, r *http.Request) {
	list, err := s.workers.List(r.Context(), r.URL.Query().Get("site_id"))
	if err != nil {
		writeWorkerError(w, err)
		return
	}
	writeJSON(w, list)
}

// POST /api/workers {"site_id", "type", "command"}
func (s *Server) handleCreateWorker(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteID  string       `json:"site_id"`
		Type    workers.Type `json:"type"`
		Command string       `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	st, err := s.workers.Add(r.Context(), req.SiteID, req.Type, req.Command)
	if err != nil {
		writeWorkerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(st)
}

func workerID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, "invalid worker id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// GET /api/workers/{id}
func (s *Server) handleGetWorker(w http.ResponseWriter, r *http.Request) {
	id, ok := workerID(w, r)
	if !ok {
		return
	}
	st, err := s.workers.Get(r.Context(), id)
	if err != nil {
		writeWorkerError(w, err)
		return
	}
	writeJSON(w, st)
}

// DELETE /api/workers/{id}
func (s *Server) handleDeleteWorker(w http.ResponseWriter, r *http.Request) {
	id, ok := workerID(w, r)
	if !ok {
		return
	}
	if err := s.workers.Remove(r.Context(), id); err != nil {
		writeWorkerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// workerAction serves POST /api/workers/{id}/start|stop|restart.
func (s *Server) workerAction(action func(context.Context, int64) (workers.State, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := workerID(w, r)
		if !ok {
			return
		}
		st, err := action(r.Context(), id)
		if err != nil {
			writeWorkerError(w, err)
			return
		}
		writeJSON(w, st)
	}
}
