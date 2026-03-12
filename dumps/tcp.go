package dumps

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
)

// TCPServer listens for PHP dump payloads sent as base64-encoded JSON lines.
type TCPServer struct {
	store *Store
	hub   *Hub
}

// NewTCPServer creates a TCPServer.
func NewTCPServer(store *Store, hub *Hub) *TCPServer {
	return &TCPServer{store: store, hub: hub}
}

// ListenAndServe starts listening on addr (e.g. ":9912") and blocks
// until ctx is cancelled.
func (t *TCPServer) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("dumps tcp listen: %w", err)
	}

	log.Printf("dumps: TCP listening on %s", addr)

	// Close listener when context is done.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("dumps: accept error: %v", err)
				continue
			}
		}
		go t.handleConn(ctx, conn)
	}
}

func (t *TCPServer) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	// Allow large lines (e.g. big objects)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		dump, err := decodeLine(line)
		if err != nil {
			log.Printf("dumps: decode error: %v", err)
			continue
		}

		row, err := t.store.Insert(ctx, dump)
		if err != nil {
			log.Printf("dumps: store error: %v", err)
			continue
		}

		// Broadcast to WebSocket clients.
		t.hub.Broadcast(row)
	}
}

// decodeLine decodes a single TCP line.
// Wire format: base64(serialize([null, $context])) + "\n"
// We expect the Go server to receive base64-encoded JSON (new format from
// the rewritten prepend.php).
func decodeLine(line string) (Dump, error) {
	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		// Try without padding.
		raw, err = base64.RawStdEncoding.DecodeString(line)
		if err != nil {
			return Dump{}, fmt.Errorf("base64 decode: %w", err)
		}
	}

	var dump Dump
	if err := json.Unmarshal(raw, &dump); err != nil {
		return Dump{}, fmt.Errorf("json unmarshal: %w", err)
	}

	return dump, nil
}
