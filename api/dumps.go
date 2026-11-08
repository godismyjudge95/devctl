package api

import (
	"context"
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/danielgormly/devctl/dumps"
)

// wsUpgrader keeps gorilla's default origin check: an upgrade whose Origin
// host differs from Host gets 403, so other pages cannot read dumps or mail.
var wsUpgrader = websocket.Upgrader{}

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
	serveHub(w, r, s.dumps.Hub)
}

// serveHub upgrades to a WebSocket that receives every hub broadcast.
func serveHub(w http.ResponseWriter, r *http.Request, hub *dumps.Hub) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	hub.Register(conn)
	defer hub.Unregister(conn)

	// Read loop — discard incoming messages, detect disconnect.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
