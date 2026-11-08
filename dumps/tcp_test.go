package dumps

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielgormly/devctl/db"
	"github.com/danielgormly/devctl/inspector"
)

// TestListen_BindsLoopback: a dump or inspector frame from the network could
// forge entries, so the listener must accept only this machine.
func TestListen_BindsLoopback(t *testing.T) {
	ln, err := Listen("0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if ip := ln.Addr().(*net.TCPAddr).IP; !ip.IsLoopback() {
		t.Errorf("listener bound to %s, want a loopback address", ip)
	}
	if got := Addr("9912"); got != "127.0.0.1:9912" {
		t.Errorf("Addr(9912) = %q", got)
	}
}

func startServer(t *testing.T) (*Server, string) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	s := NewServer(sqlDB, 50, "0")
	ln, err := Listen("0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.tcp.Serve(ctx, ln)
	return s, ln.Addr().String()
}

func send(t *testing.T, addr string, lines ...string) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatal(err)
	}
}

func dumpLine(t *testing.T, d Dump) string {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestTCPServer_RoutesFramesAndLinksDumps sends a dump tagged with a unit id,
// an inspector frame for that unit, and a plain dump on one connection.
func TestTCPServer_RoutesFramesAndLinksDumps(t *testing.T) {
	s, addr := startServer(t)
	frame := `{"v":1,"type":"unit","rid":"9f2c4e1a7b3d5c60","seq":0,"final":true,` +
		`"meta":{"kind":"http","host":"shop.test"},"events":[{"kind":"log","t":1.5,"level":"info","message":"hi"}]}`
	send(t, addr,
		dumpLine(t, Dump{Source: DumpSource{File: "/app/a.php", Line: 3}, Host: "shop.test", Nodes: []Node{json.RawMessage(`{"type":"scalar"}`)}, RID: "9f2c4e1a7b3d5c60"}),
		frame,
		dumpLine(t, Dump{Source: DumpSource{File: "/app/b.php", Line: 4}, Nodes: []Node{}}),
	)

	waitFor(t, "two stored dumps", func() bool {
		rows, err := s.Store.List(context.Background(), "", 10, 0)
		return err == nil && len(rows) == 2
	})
	waitFor(t, "the unit with its event and dump", func() bool {
		d, ok := s.Requests.Get("9f2c4e1a7b3d5c60")
		return ok && len(d.Events) == 1 && len(d.Dumps) == 1
	})
	d, _ := s.Requests.Get("9f2c4e1a7b3d5c60")
	if d.Meta.Host != "shop.test" || !d.Final {
		t.Errorf("unit meta = %+v final=%v", d.Meta, d.Final)
	}
	if !strings.Contains(string(d.Dumps[0]), "/app/a.php") {
		t.Errorf("linked dump = %s, want the a.php dump", d.Dumps[0])
	}
	if n := len(s.Requests.List(inspector.Filter{})); n != 1 {
		t.Errorf("ring holds %d units, want 1 (the plain dump must not create one)", n)
	}
}

// TestTCPServer_LargeFrame: a frame of up to 200 events with long SQL is
// far over the old 1 MB line limit.
func TestTCPServer_LargeFrame(t *testing.T) {
	s, addr := startServer(t)
	sql := strings.Repeat("x", 10_000)
	var events []string
	for i := 0; i < 200; i++ {
		events = append(events, `{"kind":"query","t":1,"sql":"select '`+sql+`'","ms":1}`)
	}
	frame := `{"v":1,"type":"unit","rid":"abc","seq":0,"final":true,"meta":{"kind":"command"},"events":[` + strings.Join(events, ",") + `]}`
	send(t, addr, frame)
	waitFor(t, "the large frame", func() bool {
		d, ok := s.Requests.Get("abc")
		return ok && len(d.Events) == 200
	})
}
