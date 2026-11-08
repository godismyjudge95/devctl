package sites

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/danielgormly/devctl/db"
)

func TestParseUpstream(t *testing.T) {
	cases := []struct {
		in, str, dial string
	}{
		{"127.0.0.1:5173", "http://127.0.0.1:5173", "127.0.0.1:5173"},
		{"http://localhost:5173", "http://localhost:5173", "localhost:5173"},
		{"http://localhost:5173/", "http://localhost:5173", "localhost:5173"},
		{" HTTP://LocalHost:3000 ", "http://localhost:3000", "localhost:3000"},
		{"https://api.example.com", "https://api.example.com", "api.example.com:443"},
		{"http://myapi.lan", "http://myapi.lan", "myapi.lan:80"},
		{"https://127.0.0.1:8443", "https://127.0.0.1:8443", "127.0.0.1:8443"},
		{"http://[::1]:5173", "http://[::1]:5173", "[::1]:5173"},
		{"[::1]:5173", "http://[::1]:5173", "[::1]:5173"},
	}
	for _, c := range cases {
		u, err := ParseUpstream(c.in)
		if err != nil {
			t.Errorf("ParseUpstream(%q): %v", c.in, err)
			continue
		}
		if u.String() != c.str || u.Dial() != c.dial {
			t.Errorf("ParseUpstream(%q) = %s dial %s, want %s dial %s", c.in, u, u.Dial(), c.str, c.dial)
		}
	}

	for _, in := range []string{
		"",
		"ftp://127.0.0.1:21",
		"ws://127.0.0.1:6001",
		"http://127.0.0.1:5173/app",
		"http://127.0.0.1:5173?x=1",
		"http://user:pw@127.0.0.1:5173",
		"http://127.0.0.1:0",
		"http://127.0.0.1:70000",
		"http://:5173",
		"http://127.0.0.1:abc",
	} {
		if u, err := ParseUpstream(in); err == nil {
			t.Errorf("ParseUpstream(%q) = %s, want an error", in, u)
		}
	}
}

func TestProxyJSONRejectsBadUpstream(t *testing.T) {
	var p Proxy
	if err := json.Unmarshal([]byte(`{"upstream":"ftp://x:1"}`), &p); err == nil {
		t.Fatal("decoding a proxy with an ftp upstream must fail")
	}
	if err := json.Unmarshal([]byte(`{"preserve_host":true}`), &p); err == nil {
		t.Fatal("decoding a proxy without an upstream must fail")
	}
	if err := json.Unmarshal([]byte(`{"upstream":"localhost:5173","preserve_host":true}`), &p); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(p)
	if string(out) != `{"upstream":"http://localhost:5173","preserve_host":true}` {
		t.Errorf("proxy JSON = %s", out)
	}
}

func TestValidateProxyDomain(t *testing.T) {
	for _, d := range []string{"vite.test", "vite.myapp.test", "my-api.test"} {
		if err := ValidateProxyDomain(d); err != nil {
			t.Errorf("ValidateProxyDomain(%q): %v", d, err)
		}
	}
	for _, d := range []string{"", ".test", "vite.com", "Vite.test", "vi te.test", "http://vite.test", "vite..test", "-vite.test", "vite.test:443"} {
		if ValidateProxyDomain(d) == nil {
			t.Errorf("ValidateProxyDomain(%q) = nil, want an error", d)
		}
	}
}

func TestSettingsWithProxy(t *testing.T) {
	up, _ := ParseUpstream("127.0.0.1:5173")
	settings, err := SettingsWithProxy(`{"worktree_symlinks":["vendor"]}`, &Proxy{Upstream: up})
	if err != nil {
		t.Fatal(err)
	}
	got := ProxyFromSettings(settings)
	if got == nil || got.Upstream.String() != "http://127.0.0.1:5173" || got.PreserveHost {
		t.Fatalf("ProxyFromSettings(%s) = %+v", settings, got)
	}
	if cfg := ConfigFromSettings(settings, t.TempDir()); len(cfg.Symlinks) != 1 {
		t.Errorf("worktree settings lost: %s", settings)
	}

	cleared, err := SettingsWithProxy(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ProxyFromSettings(cleared) != nil {
		t.Errorf("settings still hold a proxy: %s", cleared)
	}
	if ProxyFromSettings(`{"site_type":"php"}`) != nil || ProxyFromSettings("not json") != nil {
		t.Error("a PHP site has no proxy")
	}
}

// caddyProxyHandler is the part of Caddy's reverse_proxy JSON a proxy site sets.
type caddyProxyHandler struct {
	Handler   string `json:"handler"`
	Upstreams []struct {
		Dial string `json:"dial"`
	} `json:"upstreams"`
	Headers struct {
		Request struct {
			Set map[string][]string `json:"set"`
		} `json:"request"`
	} `json:"headers"`
	Transport *struct {
		Protocol string `json:"protocol"`
		TLS      *struct {
			InsecureSkipVerify bool `json:"insecure_skip_verify"`
		} `json:"tls"`
	} `json:"transport"`
}

// proxyRouteHandler returns the reverse_proxy handler of a proxy route without
// CORS: the route's only handler, with no matcher besides the host, so every
// request (WebSocket upgrades included) reaches it.
func proxyRouteHandler(t *testing.T, p Proxy) caddyProxyHandler {
	t.Helper()
	raw, err := json.Marshal(buildRoute(VhostConfig{ID: "vhost-vite-test", Hosts: []string{"vite.test"}, Proxy: &p}))
	if err != nil {
		t.Fatal(err)
	}
	var route struct {
		Match  []map[string]json.RawMessage `json:"match"`
		Handle []caddyProxyHandler          `json:"handle"`
	}
	if err := json.Unmarshal(raw, &route); err != nil {
		t.Fatal(err)
	}
	if len(route.Match) != 1 || len(route.Match[0]) != 1 || route.Match[0]["host"] == nil {
		t.Fatalf("route must match on host only: %s", raw)
	}
	if len(route.Handle) != 1 || route.Handle[0].Handler != "reverse_proxy" {
		t.Fatalf("route must hand every request to reverse_proxy: %s", raw)
	}
	return route.Handle[0]
}

func TestBuildRoute_ProxySite(t *testing.T) {
	mustUp := func(s string) Upstream {
		u, err := ParseUpstream(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	cases := []struct {
		name     string
		proxy    Proxy
		dial     string
		host     string
		tls      bool
		insecure bool
	}{
		{"http sends upstream host", Proxy{Upstream: mustUp("http://localhost:5173")}, "localhost:5173", "{http.reverse_proxy.upstream.hostport}", false, false},
		{"preserve host", Proxy{Upstream: mustUp("127.0.0.1:3000"), PreserveHost: true}, "127.0.0.1:3000", "{http.request.hostport}", false, false},
		{"https loopback skips verify", Proxy{Upstream: mustUp("https://127.0.0.1:5173")}, "127.0.0.1:5173", "{http.reverse_proxy.upstream.hostport}", true, true},
		{"https remote verifies", Proxy{Upstream: mustUp("https://api.example.com")}, "api.example.com:443", "{http.reverse_proxy.upstream.hostport}", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := proxyRouteHandler(t, c.proxy)
			if len(h.Upstreams) != 1 || h.Upstreams[0].Dial != c.dial {
				t.Errorf("upstreams = %+v, want dial %s", h.Upstreams, c.dial)
			}
			if got := h.Headers.Request.Set["Host"]; len(got) != 1 || got[0] != c.host {
				t.Errorf("Host header = %v, want %s", got, c.host)
			}
			gotTLS := h.Transport != nil && h.Transport.TLS != nil
			if gotTLS != c.tls {
				t.Fatalf("tls transport = %v, want %v", gotTLS, c.tls)
			}
			if gotTLS && h.Transport.TLS.InsecureSkipVerify != c.insecure {
				t.Errorf("insecure_skip_verify = %v, want %v", h.Transport.TLS.InsecureSkipVerify, c.insecure)
			}
		})
	}
}

func TestServiceProxyKeepsHost(t *testing.T) {
	h := proxyRouteHandler(t, *ServiceProxy("127.0.0.1:7383"))
	if h.Upstreams[0].Dial != "127.0.0.1:7383" || h.Headers.Request.Set["Host"][0] != "{http.request.hostport}" || h.Transport != nil {
		t.Errorf("service proxy handler = %+v", h)
	}
}

func TestCreate_ProxySiteHasNoPHP(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	m := NewManager(sqlDB, NewCaddyClient("http://127.0.0.1:1"), t.TempDir())

	up, _ := ParseUpstream("localhost:5173")
	site, err := m.Create(context.Background(), CreateSiteInput{Domain: "vite.test", HTTPS: true, Proxy: &Proxy{Upstream: up}})
	if err != nil {
		t.Fatal(err)
	}
	if site.PhpVersion != "" || site.RootPath != "" {
		t.Errorf("proxy site php=%q root=%q, want both empty", site.PhpVersion, site.RootPath)
	}
	vhost := VhostFromSite(site, t.TempDir())
	if vhost.Proxy == nil || vhost.Proxy.Upstream.String() != "http://localhost:5173" {
		t.Errorf("vhost proxy = %+v", vhost.Proxy)
	}
	if _, err := m.CreateWorktree(context.Background(), site.ID, "main", false, WorktreeSetupConfig{}); err == nil {
		t.Error("a proxy site must not get a worktree")
	}
}
