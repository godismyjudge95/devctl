package dumps

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/danielgormly/devctl/inspector"
)

// maxLine is the longest line the receiver reads. An inspector frame
// carries up to 200 events of up to 10 KB of SQL each.
const maxLine = 4 << 20

// TCPServer receives one message per line. A line that starts with { is a
// request inspector frame (base64 has no {). Any other line is a dump:
// base64-encoded JSON.
type TCPServer struct {
	store       *Store
	hub         *Hub
	ring        *inspector.Ring
	requestsHub *Hub
}

// Serve accepts connections on ln until ctx is cancelled.
func (t *TCPServer) Serve(ctx context.Context, ln net.Listener) {
	log.Printf("dumps: TCP listening on %s", ln.Addr())

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
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
	scanner.Buffer(make([]byte, 64*1024), maxLine)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if line[0] == '{' {
			unit, err := t.ring.Ingest(line)
			if err != nil {
				log.Printf("dumps: %v", err)
				continue
			}
			t.requestsHub.Broadcast(unit)
			continue
		}

		dump, err := decodeLine(string(line))
		if err != nil {
			log.Printf("dumps: decode error: %v", err)
			continue
		}

		row, err := t.store.Insert(ctx, dump)
		if err != nil {
			log.Printf("dumps: store error: %v", err)
			continue
		}
		t.hub.Broadcast(row)

		if dump.RID != "" {
			raw, _ := json.Marshal(row)
			unit, err := t.ring.LinkDump(dump.RID, raw)
			if err != nil {
				log.Printf("dumps: link dump %d: %v", row.ID, err)
				continue
			}
			t.requestsHub.Broadcast(unit)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("dumps: read: %v", err)
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
