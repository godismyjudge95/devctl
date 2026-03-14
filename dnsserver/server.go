// Package dnsserver implements a lightweight DNS server that intercepts
// configurable TLDs (e.g. ".test") and returns a fixed A record, forwarding
// all other queries to the system upstream resolver.
package dnsserver

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

// Config holds the runtime configuration for the DNS server.
type Config struct {
	// Port is the UDP/TCP port to listen on (e.g. "5354").
	Port string
	// TargetIP is the IPv4 address returned for intercepted TLD queries.
	// When empty, DetectLANIP() is used as a fallback.
	TargetIP string
	// TLDs is a list of TLD strings to intercept (e.g. [".test"]).
	// Each entry may or may not include a leading dot; the server normalises them.
	TLDs []string
	// Upstream is the upstream DNS address (host:port) for non-intercepted queries.
	// When empty, SystemUpstream() is used.
	Upstream string
}

// Server is the embedded DNS server.
type Server struct {
	cfg Config
}

// New creates a new Server with the given Config.
func New(cfg Config) *Server {
	if cfg.Port == "" {
		cfg.Port = "5354"
	}
	if cfg.TargetIP == "" {
		cfg.TargetIP = DetectLANIP()
	}
	if len(cfg.TLDs) == 0 {
		cfg.TLDs = []string{".test"}
	}
	if cfg.Upstream == "" {
		cfg.Upstream = SystemUpstream()
	}
	// Normalise TLDs: ensure each starts with a dot.
	for i, t := range cfg.TLDs {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, ".") {
			cfg.TLDs[i] = "." + t
		} else {
			cfg.TLDs[i] = t
		}
	}
	return &Server{cfg: cfg}
}

// Run starts the UDP and TCP listeners and blocks until ctx is cancelled.
// It writes log lines to logW.
func (s *Server) Run(ctx context.Context, logW io.Writer) error {
	addr := ":" + s.cfg.Port

	mux := dns.NewServeMux()
	mux.HandleFunc(".", s.handleQuery(logW))

	udpServer := &dns.Server{Addr: addr, Net: "udp", Handler: mux, ReusePort: true}
	tcpServer := &dns.Server{Addr: addr, Net: "tcp", Handler: mux, ReusePort: true}

	errCh := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		fmt.Fprintf(logW, "dns: listening on UDP %s (target=%s tlds=%v upstream=%s)\n",
			addr, s.cfg.TargetIP, s.cfg.TLDs, s.cfg.Upstream)
		if err := udpServer.ListenAndServe(); err != nil {
			errCh <- fmt.Errorf("udp: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := tcpServer.ListenAndServe(); err != nil {
			errCh <- fmt.Errorf("tcp: %w", err)
		}
	}()

	// Wait for ctx cancellation then shut both servers down.
	go func() {
		<-ctx.Done()
		_ = udpServer.Shutdown()
		_ = tcpServer.Shutdown()
	}()

	// Block until both goroutines finish.
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case err := <-errCh:
		// One server failed to start — cancel both.
		_ = udpServer.Shutdown()
		_ = tcpServer.Shutdown()
		<-doneCh
		return err
	case <-doneCh:
		return nil
	}
}

// handleQuery returns a dns.HandlerFunc that intercepts configured TLDs and
// forwards everything else upstream.
func (s *Server) handleQuery(logW io.Writer) dns.HandlerFunc {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		if len(r.Question) == 0 {
			dns.HandleFailed(w, r)
			return
		}

		q := r.Question[0]
		name := strings.ToLower(q.Name) // FQDN with trailing dot

		if q.Qtype == dns.TypeA && s.matchesTLD(name) {
			s.replyWithIP(w, r, name, logW)
			return
		}

		s.forward(w, r, logW)
	}
}

// matchesTLD reports whether the FQDN (with trailing dot) belongs to one of
// the intercepted TLDs.
func (s *Server) matchesTLD(fqdn string) bool {
	// fqdn looks like "foo.test." — strip trailing dot for comparison.
	name := strings.TrimSuffix(fqdn, ".")
	for _, tld := range s.cfg.TLDs {
		// tld is ".test" (with leading dot, no trailing dot)
		suffix := tld // e.g. ".test"
		if strings.HasSuffix(name, suffix) || name == strings.TrimPrefix(suffix, ".") {
			return true
		}
	}
	return false
}

// replyWithIP writes an A record response pointing to TargetIP.
func (s *Server) replyWithIP(w dns.ResponseWriter, r *dns.Msg, name string, logW io.Writer) {
	ip := net.ParseIP(s.cfg.TargetIP).To4()
	if ip == nil {
		dns.HandleFailed(w, r)
		return
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer = append(m.Answer, &dns.A{
		Hdr: dns.RR_Header{
			Name:   r.Question[0].Name,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    60,
		},
		A: ip,
	})
	if err := w.WriteMsg(m); err != nil {
		fmt.Fprintf(logW, "dns: write reply for %s: %v\n", name, err)
	}
}

// forward proxies the query to the upstream resolver.
func (s *Server) forward(w dns.ResponseWriter, r *dns.Msg, logW io.Writer) {
	c := new(dns.Client)
	// Use the same network protocol as the incoming connection where possible.
	resp, _, err := c.Exchange(r, s.cfg.Upstream)
	if err != nil {
		log.Printf("[dns] forward %s: %v", r.Question[0].Name, err)
		dns.HandleFailed(w, r)
		return
	}
	if err := w.WriteMsg(resp); err != nil {
		fmt.Fprintf(logW, "dns: write forward reply: %v\n", err)
	}
}

// DetectLANIP returns the primary LAN IPv4 address of the machine by
// initiating a dummy UDP connection (no packets sent) to 8.8.8.8:80 and
// reading the local address. Falls back to "127.0.0.1" on error.
func DetectLANIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP.String()
}

// SystemUpstream returns the first non-loopback nameserver from the system
// resolver configuration files, skipping 127.0.0.53 (systemd-resolved stub).
// Search order: /run/systemd/resolve/resolv.conf → /etc/resolv.conf → 8.8.8.8:53.
func SystemUpstream() string {
	candidates := []string{
		"/run/systemd/resolve/resolv.conf",
		"/etc/resolv.conf",
	}
	for _, path := range candidates {
		if ns := parseResolvConf(path); ns != "" {
			return ns
		}
	}
	return "8.8.8.8:53"
}

// parseResolvConf reads a resolv.conf-style file and returns the first
// nameserver entry that is not 127.0.0.53, formatted as host:53.
func parseResolvConf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "nameserver") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ns := fields[1]
		if ns == "127.0.0.53" {
			continue
		}
		return ns + ":53"
	}
	return ""
}
