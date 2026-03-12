package api

import (
	"context"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) handleGetDumps(w http.ResponseWriter, r *http.Request) {
	site := r.URL.Query().Get("site")
	limit, offset := parsePage(r)

	rows, err := s.dumps.Store.List(context.Background(), site, limit, offset)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

func (s *Server) handleClearDumps(w http.ResponseWriter, r *http.Request) {
	if err := s.dumps.Store.Clear(context.Background()); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDumpsWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	s.dumps.Hub.Register(conn)
	defer s.dumps.Hub.Unregister(conn)

	// Read loop — discard incoming messages, detect disconnect.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
