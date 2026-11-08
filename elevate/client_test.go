package elevate

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/danielgormly/devctl/paths"
)

func TestDaemonClientPing(t *testing.T) {
	// Short base dir: unix socket paths are limited to ~104 bytes.
	root, err := os.MkdirTemp("/tmp", "elev")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	c := NewDaemonClient(root)
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping with no socket: want an error")
	}

	if err := os.MkdirAll(paths.DevctlDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", paths.ElevateSocket(root))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping with a live socket: %v", err)
	}
}
