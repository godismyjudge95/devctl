package dnsserver

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/miekg/dns"
)

func TestParseResolvConfSkipsLoopback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	body := "nameserver 127.0.0.1\nnameserver ::1\nnameserver 127.0.0.53\nnameserver 1.1.1.1\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got := parseResolvConf(path)
	if got != "1.1.1.1:53" {
		t.Fatalf("got %q want 1.1.1.1:53", got)
	}
}

func TestMatchesTLD(t *testing.T) {
	s := New(Config{TLDs: []string{".test"}, TargetIP: "192.0.2.1", Upstream: "1.1.1.1:53"})
	if !s.matchesTLD("meilisearch.test.") {
		t.Fatal("meilisearch.test should match")
	}
	if s.matchesTLD("example.com.") {
		t.Fatal("example.com should not match")
	}
}

func TestAnswerIP(t *testing.T) {
	loop := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
	v6loop := &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345}
	lan := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 50), Port: 53}

	pinned := New(Config{TargetIP: "192.0.2.1", Upstream: "1.1.1.1:53"})
	if pinned.answerIP(loop) != "192.0.2.1" {
		t.Fatalf("pinned loop = %q", pinned.answerIP(loop))
	}
	if pinned.answerIP(lan) != "192.0.2.1" {
		t.Fatalf("pinned lan = %q", pinned.answerIP(lan))
	}

	auto := New(Config{Upstream: "1.1.1.1:53"})
	if auto.answerIP(loop) != "127.0.0.1" {
		t.Fatalf("loopback query = %q want 127.0.0.1", auto.answerIP(loop))
	}
	if auto.answerIP(v6loop) != "127.0.0.1" {
		t.Fatalf("ipv6 loopback query = %q want 127.0.0.1", auto.answerIP(v6loop))
	}
	got := auto.answerIP(lan)
	if got != DetectLANIP() {
		t.Fatalf("lan query = %q want %q", got, DetectLANIP())
	}
}

type recRW struct {
	msg *dns.Msg
}

func (r *recRW) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5354}
}
func (r *recRW) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
}
func (r *recRW) WriteMsg(m *dns.Msg) error   { r.msg = m; return nil }
func (r *recRW) Write(b []byte) (int, error) { return len(b), nil }
func (r *recRW) Close() error                { return nil }
func (r *recRW) TsigStatus() error           { return nil }
func (r *recRW) TsigTimersOnly(bool)         {}
func (r *recRW) Hijack()                     {}

func TestHandleQueryAAndNoData(t *testing.T) {
	s := New(Config{TargetIP: "192.0.2.9", TLDs: []string{".test"}, Upstream: "1.1.1.1:53"})
	h := s.handleQuery(io.Discard)

	req := new(dns.Msg)
	req.SetQuestion("meilisearch.test.", dns.TypeA)
	rw := &recRW{}
	h(rw, req)
	if rw.msg == nil || len(rw.msg.Answer) != 1 {
		t.Fatalf("A: %+v", rw.msg)
	}
	a, ok := rw.msg.Answer[0].(*dns.A)
	if !ok || !a.A.Equal(net.ParseIP("192.0.2.9")) {
		t.Fatalf("A record = %+v", rw.msg.Answer[0])
	}

	req6 := new(dns.Msg)
	req6.SetQuestion("meilisearch.test.", dns.TypeAAAA)
	rw6 := &recRW{}
	h(rw6, req6)
	if rw6.msg == nil || len(rw6.msg.Answer) != 0 || !rw6.msg.Authoritative {
		t.Fatalf("AAAA NODATA: %+v", rw6.msg)
	}
}
