package api

import (
	"context"
	"embed"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	"github.com/danielgormly/devctl/doctor"
	"github.com/danielgormly/devctl/paths"
)

func TestCheckElevateDaemonReusesConnection(t *testing.T) {
	// Short base dir: unix socket paths are limited to ~104 bytes.
	root, err := os.MkdirTemp("/tmp", "elev")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.MkdirAll(paths.DevctlDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", paths.ElevateSocket(root))
	if err != nil {
		t.Fatal(err)
	}
	var conns atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	})
	srv := &http.Server{
		Handler: mux,
		ConnState: func(_ net.Conn, st http.ConnState) {
			if st == http.StateNew {
				conns.Add(1)
			}
		},
	}
	go srv.Serve(ln)
	defer srv.Close()

	s := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, embed.FS{}, root, "", "", "", "test")
	for i := 0; i < 5; i++ {
		if f := s.checkElevateDaemon(context.Background()); f.Severity != doctor.OK {
			t.Fatalf("run %d: check failed: %+v", i, f)
		}
	}
	if n := conns.Load(); n != 1 {
		t.Fatalf("server saw %d connections over 5 checks, want 1", n)
	}
}
