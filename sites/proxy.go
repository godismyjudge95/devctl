package sites

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
)

// Proxy makes a site a reverse proxy: Caddy sends every request for the site,
// WebSocket upgrades included, to Upstream instead of PHP-FPM. A site is a
// proxy site exactly when its settings JSON holds a "proxy" object.
type Proxy struct {
	Upstream Upstream `json:"upstream"`
	// PreserveHost sends the site's own Host header (myapp.test) upstream.
	// Off, the upstream sees its own host:port, which dev servers such as
	// Vite accept without allowedHosts config; X-Forwarded-Host still carries
	// the site domain.
	PreserveHost bool `json:"preserve_host"`
}

// UnmarshalJSON rejects a proxy without an upstream, so a decoded Proxy
// always has a valid one.
func (p *Proxy) UnmarshalJSON(b []byte) error {
	type plain Proxy
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if v.Upstream.host == "" {
		return errors.New("upstream is required")
	}
	*p = Proxy(v)
	return nil
}

// Upstream is a parsed http:// or https:// origin with no path. The zero
// value is not valid; build one with ParseUpstream.
type Upstream struct {
	https bool
	host  string // hostname or IP, no brackets
	port  string // "" when the URL has no port
}

// ParseUpstream accepts "127.0.0.1:5173", "http://localhost:5173", or
// "https://api.local:8443". A value without a scheme is http.
func ParseUpstream(raw string) (Upstream, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Upstream{}, errors.New("upstream is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Upstream{}, fmt.Errorf("upstream %q is not a URL: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme != "http" && scheme != "https":
		return Upstream{}, fmt.Errorf("upstream %q must start with http:// or https://", raw)
	case u.User != nil:
		return Upstream{}, fmt.Errorf("upstream %q must not contain a user or password", raw)
	case u.Path != "" && u.Path != "/", u.RawQuery != "", u.Fragment != "":
		return Upstream{}, fmt.Errorf("upstream %q must not have a path or query; give only scheme, host, and port", raw)
	case u.Hostname() == "":
		return Upstream{}, fmt.Errorf("upstream %q has no host", raw)
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return Upstream{}, fmt.Errorf("upstream %q has an invalid port", raw)
		}
	}
	return Upstream{https: scheme == "https", host: strings.ToLower(u.Hostname()), port: u.Port()}, nil
}

// String is the canonical form stored in settings and shown to the user.
func (u Upstream) String() string {
	scheme := "http"
	if u.https {
		scheme = "https"
	}
	host := u.host
	if u.port != "" {
		host = net.JoinHostPort(u.host, u.port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}

// Dial is the host:port Caddy connects to.
func (u Upstream) Dial() string {
	port := u.port
	if port == "" {
		port = "80"
		if u.https {
			port = "443"
		}
	}
	return net.JoinHostPort(u.host, port)
}

// loopback reports whether the upstream is on this machine. Caddy skips TLS
// verification for loopback https upstreams: dev servers use self-signed
// certificates, and the traffic never leaves the machine.
func (u Upstream) loopback() bool {
	if u.host == "localhost" {
		return true
	}
	ip := net.ParseIP(u.host)
	return ip != nil && ip.IsLoopback()
}

func (u Upstream) MarshalText() ([]byte, error) { return []byte(u.String()), nil }

func (u *Upstream) UnmarshalText(b []byte) error {
	parsed, err := ParseUpstream(string(b))
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}

// ServiceProxy is the proxy of a managed service vhost (reverb.test,
// meilisearch.test, …). It keeps the original Host: S3 signatures and
// devctl's own cross-origin check depend on it.
func ServiceProxy(dial string) *Proxy {
	host, port, _ := net.SplitHostPort(dial)
	return &Proxy{Upstream: Upstream{host: host, port: port}, PreserveHost: true}
}

// ProxyFromSettings returns the proxy stored in a site's settings JSON, or
// nil for a PHP site.
func ProxyFromSettings(settingsJSON string) *Proxy {
	var s struct {
		Proxy *Proxy `json:"proxy"`
	}
	if json.Unmarshal([]byte(settingsJSON), &s) != nil {
		return nil
	}
	return s.Proxy
}

// SettingsWithProxy returns settingsJSON with its "proxy" key set to p, or
// removed when p is nil. Other keys (worktree defaults) are kept.
func SettingsWithProxy(settingsJSON string, p *Proxy) (string, error) {
	m := map[string]json.RawMessage{}
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &m); err != nil {
			return "", fmt.Errorf("site settings: %w", err)
		}
	}
	if p == nil {
		delete(m, "proxy")
	} else {
		raw, err := json.Marshal(p)
		if err != nil {
			return "", err
		}
		m["proxy"] = raw
	}
	out, err := json.Marshal(m)
	return string(out), err
}

// VhostFromSite is the Caddy vhost of a stored site.
func VhostFromSite(site dbq.Site, serverRoot string) VhostConfig {
	var aliases []string
	_ = json.Unmarshal([]byte(site.Aliases), &aliases)
	return VhostConfig{
		ID:         "vhost-" + site.ID,
		Hosts:      append([]string{site.Domain}, aliases...),
		RootPath:   site.RootPath,
		PublicDir:  site.PublicDir,
		PHPVersion: site.PhpVersion,
		HTTPS:      site.Https == 1,
		Proxy:      ProxyFromSettings(site.Settings),
		ServerRoot: serverRoot,
		EnableCORS: site.Cors == 1,
	}
}

// ValidateProxyDomain checks the domain of a new proxy site. Caddy issues
// internal certificates only for *.test names.
func ValidateProxyDomain(domain string) error {
	if !strings.HasSuffix(domain, ".test") || len(domain) <= len(".test") {
		return fmt.Errorf("domain %q must end in .test", domain)
	}
	for _, r := range domain {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return fmt.Errorf("domain %q may contain only lowercase letters, digits, hyphens, and dots", domain)
		}
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("domain %q has an empty label or a label that starts or ends with a hyphen", domain)
		}
	}
	return nil
}
