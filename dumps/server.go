package dumps

import (
	"context"
	"database/sql"
	"fmt"
	"net"

	"github.com/danielgormly/devctl/inspector"
)

// Server wires together the TCP listener, the WebSocket hubs, the SQLite
// dump store, and the request inspector ring.
type Server struct {
	Store       *Store
	Hub         *Hub
	Requests    *inspector.Ring
	RequestsHub *Hub
	Port        string // the dump_tcp_port setting the listener uses
	tcp         *TCPServer
}

// NewServer creates a fully-wired dumps Server that listens on port.
func NewServer(db *sql.DB, maxEntries int64, port string) *Server {
	s := &Server{
		Store:       NewStore(db, maxEntries),
		Hub:         NewHub(),
		Requests:    inspector.NewRing(inspector.DefaultMaxUnits, inspector.DefaultMaxBytes),
		RequestsHub: NewHub(),
		Port:        port,
	}
	s.tcp = &TCPServer{store: s.Store, hub: s.Hub, ring: s.Requests, requestsHub: s.RequestsHub}
	return s
}

// Addr is the address PHP sends dumps and inspector frames to. Only this
// machine may send: a frame or dump from the network could forge entries.
func Addr(port string) string {
	return net.JoinHostPort("127.0.0.1", port)
}

// Listen opens the dump TCP listener on Addr(port).
func Listen(port string) (net.Listener, error) {
	ln, err := net.Listen("tcp", Addr(port))
	if err != nil {
		return nil, fmt.Errorf("dumps tcp listen: %w", err)
	}
	return ln, nil
}

// Run starts the TCP listener. It blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	ln, err := Listen(s.Port)
	if err != nil {
		return fmt.Errorf("dumps server: %w", err)
	}
	s.tcp.Serve(ctx, ln)
	return nil
}
