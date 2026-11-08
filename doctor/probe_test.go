package doctor

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestHTTPServerHeader_ReturnsServerHeader(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Header().Set("Server", "Caddy")
	}))
	defer srv.Close()

	got, err := HTTPServerHeader(context.Background(), srv.Listener.Addr().String(), "devctl.test")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Caddy" {
		t.Errorf("server = %q, want Caddy", got)
	}
	if gotHost != "devctl.test" {
		t.Errorf("host = %q, want devctl.test", gotHost)
	}
}

func TestHTTPServerHeader_ErrorsWhenNothingListens(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if _, err := HTTPServerHeader(context.Background(), addr, "devctl.test"); err == nil {
		t.Fatal("want an error for a closed port")
	}
}

func TestTLSIssuer_ReportsLeafIssuer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	issuer, err := TLSIssuer(context.Background(), srv.Listener.Addr().String(), "devctl.test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(issuer, "Acme Co") {
		t.Errorf("issuer = %q, want the httptest CA", issuer)
	}
	if IsCaddyIssuer(issuer) {
		t.Error("httptest issuer must not count as Caddy")
	}
	if !IsCaddyIssuer("CN=Caddy Local Authority - ECC Intermediate") {
		t.Error("Caddy intermediate issuer must count as Caddy")
	}
}

func TestResolveA_AnswersAndMisses(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := dns.NewServeMux()
	mux.HandleFunc("test.", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.ParseIP("10.0.0.7"),
		}}
		w.WriteMsg(m)
	})
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(m)
	})
	srv := &dns.Server{PacketConn: pc, Handler: mux}
	go srv.ActivateAndServe()
	defer srv.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := ResolveA(ctx, pc.LocalAddr().String(), "devctl.test")
	if err != nil {
		t.Fatal(err)
	}
	if !ips[0].Equal(net.ParseIP("10.0.0.7")) {
		t.Errorf("ip = %v, want 10.0.0.7", ips[0])
	}
	if _, err := ResolveA(ctx, pc.LocalAddr().String(), "devctl.example"); err == nil {
		t.Error("want an error when the server returns no A record")
	}
}

func testCertPEM(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestBundleHasCert(t *testing.T) {
	system := testCertPEM(t, "system")
	caddy := testCertPEM(t, "caddy root")
	other := testCertPEM(t, "rotated root")
	bundle := append(append([]byte{}, system...), caddy...)

	if !BundleHasCert(bundle, caddy) {
		t.Error("bundle with the Caddy root must match")
	}
	if BundleHasCert(bundle, other) {
		t.Error("bundle without the rotated root must not match")
	}
	if BundleHasCert(bundle, []byte("not pem")) {
		t.Error("empty cert input must not match")
	}
}

func TestShellPathFiles(t *testing.T) {
	home := t.TempDir()
	bin := "/home/alice/ddev/sites/server/bin"
	block := "# >>> devctl PATH >>>\nexport PATH=\"" + bin + ":$PATH\"\n# <<< devctl PATH <<<\n"
	if err := os.WriteFile(filepath.Join(home, ".zshenv"), []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := "export PATH=\"/old/server/bin:$PATH\"\n"
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ShellPathFiles(home, bin)
	if len(got) != 1 || got[0] != filepath.Join(home, ".zshenv") {
		t.Errorf("files = %v, want only .zshenv", got)
	}
	if got := ShellPathFiles(t.TempDir(), bin); len(got) != 0 {
		t.Errorf("empty home: files = %v, want none", got)
	}
}

func TestXMLWellFormed(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.xml")
	bad := filepath.Join(dir, "bad.xml")
	os.WriteFile(good, []byte("<clickhouse><a>1</a></clickhouse>"), 0o644)
	os.WriteFile(bad, []byte("<clickhouse><a>1</clickhouse>"), 0o644)

	if err := XMLWellFormed(good); err != nil {
		t.Errorf("good xml: %v", err)
	}
	if err := XMLWellFormed(bad); err == nil {
		t.Error("bad xml: want a parse error")
	}
}

func TestFreeBytesAndHumanBytes(t *testing.T) {
	free, err := FreeBytes(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if free == 0 {
		t.Error("free bytes = 0 on the test filesystem")
	}
	for n, want := range map[uint64]string{512: "512 B", 1536: "1.5 KiB", 3 << 30: "3.0 GiB"} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
