package dumps

import (
	"context"
	"database/sql"
	"fmt"
)

// Server wires together the TCP listener, WebSocket hub, and SQLite store.
type Server struct {
	Store *Store
	Hub   *Hub
	tcp   *TCPServer
}

// NewServer creates a fully-wired dumps Server.
func NewServer(db *sql.DB, maxEntries int64) *Server {
	hub := NewHub()
	store := NewStore(db, maxEntries)
	tcp := NewTCPServer(store, hub)
	return &Server{Store: store, Hub: hub, tcp: tcp}
}

// Run starts the TCP listener. It blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	if err := s.tcp.ListenAndServe(ctx, addr); err != nil {
		return fmt.Errorf("dumps server: %w", err)
	}
	return nil
}
