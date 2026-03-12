package dumps

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"

	dbq "github.com/danielgormly/devctl/db/queries"
)

// Hub manages active WebSocket connections and broadcasts new dumps.
type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
}

// NewHub creates a Hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]struct{})}
}

// Register adds a connection to the hub.
func (h *Hub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
}

// Unregister removes a connection from the hub.
func (h *Hub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
}

// Broadcast sends a dump to all connected WebSocket clients.
func (h *Hub) Broadcast(dump dbq.Dump) {
	msg, err := json.Marshal(dump)
	if err != nil {
		log.Printf("hub: marshal: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("hub: write to client: %v", err)
		}
	}
}
