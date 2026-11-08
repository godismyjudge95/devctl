package dumps

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub manages active WebSocket connections and broadcasts JSON messages
// (new dumps, or request inspector unit updates).
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

// Broadcast sends v as JSON to all connected WebSocket clients.
func (h *Hub) Broadcast(v any) {
	msg, err := json.Marshal(v)
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
