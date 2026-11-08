package doctor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/miekg/dns"
)

// HTTPServerHeader sends a plain HTTP request to addr and returns the
// Server response header. Caddy always sets "Caddy".
func HTTPServerHeader(ctx context.Context, addr, host string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "http://"+addr+"/", nil)
	if err != nil {
		return "", err
	}
	req.Host = host
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	return resp.Header.Get("Server"), nil
}

// TLSIssuer completes a TLS handshake with addr using SNI serverName and
// returns the leaf certificate issuer. It does not verify trust: the
// question is who holds the port, not whether the OS trusts it.
func TLSIssuer(ctx context.Context, addr, serverName string) (string, error) {
	d := tls.Dialer{Config: &tls.Config{ServerName: serverName, InsecureSkipVerify: true}}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", errors.New("no certificate presented")
	}
	return certs[0].Issuer.String(), nil
}

// ResolveA asks the DNS server at addr for the A record of name.
func ResolveA(ctx context.Context, addr, name string) ([]net.IP, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	resp, _, err := new(dns.Client).ExchangeContext(ctx, m, addr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	for _, rr := range resp.Answer {
		if a, ok := rr.(*dns.A); ok {
			ips = append(ips, a.A)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no A record (rcode %s)", dns.RcodeToString[resp.Rcode])
	}
	return ips, nil
}

// BundleHasCert reports whether the PEM bundle contains every certificate in certPEM.
func BundleHasCert(bundle, certPEM []byte) bool {
	have := map[string]bool{}
	for rest := bundle; ; {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		have[string(b.Bytes)] = true
	}
	found := false
	for rest := certPEM; ; {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		if !have[string(b.Bytes)] {
			return false
		}
		found = true
	}
	return found
}

// FreeBytes returns the bytes available to an unprivileged user on the
// filesystem that holds path.
func FreeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// shellConfigFiles are the files selfinstall.WritePATHSetup may write.
var shellConfigFiles = []string{".zshenv", ".bashrc", ".bash_profile", ".profile"}

// ShellPathFiles returns the shell config files in home that prepend binDir
// to PATH with the devctl PATH block.
func ShellPathFiles(home, binDir string) []string {
	line := []byte(`export PATH="` + binDir + `:$PATH"`)
	var out []string
	for _, f := range shellConfigFiles {
		p := filepath.Join(home, f)
		data, err := os.ReadFile(p)
		if err == nil && bytes.Contains(data, line) {
			out = append(out, p)
		}
	}
	return out
}

// XMLWellFormed reports a parse error in the XML file at path.
func XMLWellFormed(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := xml.NewDecoder(f)
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// HumanBytes formats n as a short binary size (for example "1.5 GiB").
func HumanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// IsCaddyIssuer reports whether a certificate issuer string names Caddy's internal CA.
func IsCaddyIssuer(issuer string) bool {
	return strings.Contains(issuer, "Caddy Local Authority")
}
