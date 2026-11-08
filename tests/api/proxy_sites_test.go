//go:build integration

package apitest

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type proxySite struct {
	Site
	Proxy *struct {
		Upstream     string `json:"upstream"`
		PreserveHost bool   `json:"preserve_host"`
	} `json:"proxy"`
}

// seenRequest is what the test upstream echoes back about a request.
type seenRequest struct {
	Marker string `json:"marker"`
	Host   string `json:"host"`
	XFHost string `json:"x_forwarded_host"`
	XFProt string `json:"x_forwarded_proto"`
}

// startUpstream runs a dev server stand-in on 127.0.0.1. GET /ws upgrades
// to a WebSocket that echoes one message; every other path echoes what it
// saw as JSON.
func startUpstream(t *testing.T, marker string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			if typ, msg, err := conn.ReadMessage(); err == nil {
				_ = conn.WriteMessage(typ, []byte(marker+":"+string(msg)))
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(seenRequest{
			Marker: marker,
			Host:   r.Host,
			XFHost: r.Header.Get("X-Forwarded-Host"),
			XFProt: r.Header.Get("X-Forwarded-Proto"),
		})
	})}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// dialCaddy sends every connection to Caddy's HTTPS listener, the way a
// browser that resolves *.test to this machine (or the share tunnel) does.
func dialCaddy(ctx context.Context, network, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, "127.0.0.1:443")
}

// fetchThroughCaddy GETs https://<domain>/ until the upstream with marker
// answers. Caddy issues the certificate of a new site in the background.
func fetchThroughCaddy(t *testing.T, domain, marker string) seenRequest {
	t.Helper()
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext:     dialCaddy,
			TLSClientConfig: &tls.Config{ServerName: domain, InsecureSkipVerify: true},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	last := "no attempt"
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(time.Second) {
		resp, err := client.Get("https://" + domain + "/")
		if err != nil {
			last = err.Error()
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var seen seenRequest
		if resp.StatusCode == http.StatusOK && json.Unmarshal(b, &seen) == nil && seen.Marker == marker {
			return seen
		}
		last = resp.Status + " body=" + string(b)
	}
	t.Fatalf("https://%s never reached upstream %s: %s", domain, marker, last)
	return seenRequest{}
}

func deleteSiteByDomain(t *testing.T, domain string) {
	t.Helper()
	for _, s := range decodeJSON[[]Site](t, httpGet(t, "/api/sites")) {
		if s.Domain == domain {
			httpDelete(t, "/api/sites/"+s.ID)
		}
	}
}

// TestProxySites_ForwardsHTTPAndWebSocket creates a proxy site to a local
// server, reaches it over HTTPS and a WebSocket through Caddy, points it at a
// second server, and deletes it.
func TestProxySites_ForwardsHTTPAndWebSocket(t *testing.T) {
	const domain = "proxy-go.test"
	deleteSiteByDomain(t, domain)
	port := startUpstream(t, "first")
	upstream := "http://127.0.0.1:" + strconv.Itoa(port)

	body, status := httpPost(t, "/api/sites", map[string]any{
		"domain": domain, "https": 1, "proxy": map[string]any{"upstream": upstream},
	})
	if status != http.StatusCreated {
		t.Fatalf("create proxy site: %d %s", status, body)
	}
	site := decodeJSON[proxySite](t, body)
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+site.ID) })
	if site.Proxy == nil || site.Proxy.Upstream != upstream || site.Proxy.PreserveHost {
		t.Fatalf("created site proxy = %+v, want %s without preserve_host", site.Proxy, upstream)
	}
	if site.PhpVersion != "" || site.RootPath != "" {
		t.Errorf("proxy site php_version=%q root_path=%q, want both empty", site.PhpVersion, site.RootPath)
	}
	if !caddyRouteExists(t, "vhost-"+site.ID) {
		t.Fatal("no Caddy route for the proxy site")
	}

	t.Log("HTTPS request reaches the upstream with its own Host and the site in X-Forwarded-Host")
	seen := fetchThroughCaddy(t, domain, "first")
	if seen.Host != "127.0.0.1:"+strconv.Itoa(port) || seen.XFHost != domain || seen.XFProt != "https" {
		t.Errorf("upstream saw %+v, want Host 127.0.0.1:%d, X-Forwarded-Host %s, X-Forwarded-Proto https", seen, port, domain)
	}

	t.Log("WebSocket upgrade passes through the proxy")
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		NetDialContext:   dialCaddy,
		TLSClientConfig:  &tls.Config{ServerName: domain, InsecureSkipVerify: true},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, resp, err := dialer.DialContext(ctx, "wss://"+domain+"/ws", http.Header{"Origin": {"https://" + domain}})
	if err != nil {
		if resp != nil {
			t.Fatalf("dial wss://%s/ws: %v (status %d)", domain, err, resp.StatusCode)
		}
		t.Fatalf("dial wss://%s/ws: %v", domain, err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	if _, msg, err := conn.ReadMessage(); err != nil || string(msg) != "first:ping" {
		t.Fatalf("websocket echo = %q, %v; want first:ping", msg, err)
	}

	t.Log("update points the site at a second server and keeps the site Host")
	port2 := startUpstream(t, "second")
	body, status = httpPut(t, "/api/sites/"+site.ID, map[string]any{
		"domain": domain, "https": 1, "aliases": []string{},
		"proxy": map[string]any{"upstream": "127.0.0.1:" + strconv.Itoa(port2), "preserve_host": true},
	})
	if status != http.StatusOK {
		t.Fatalf("update proxy site: %d %s", status, body)
	}
	updated := decodeJSON[proxySite](t, body)
	if updated.Proxy == nil || updated.Proxy.Upstream != "http://127.0.0.1:"+strconv.Itoa(port2) || !updated.Proxy.PreserveHost {
		t.Fatalf("updated proxy = %+v", updated.Proxy)
	}
	if seen := fetchThroughCaddy(t, domain, "second"); seen.Host != domain {
		t.Errorf("with preserve_host the upstream saw Host %q, want %s", seen.Host, domain)
	}

	t.Log("an edit without a proxy object (CLI sites:cors) keeps the upstream")
	body, status = httpPut(t, "/api/sites/"+site.ID, map[string]any{"domain": domain, "https": 1, "cors": 1, "aliases": []string{}})
	if status != http.StatusOK {
		t.Fatalf("cors update: %d %s", status, body)
	}
	if kept := decodeJSON[proxySite](t, body); kept.Proxy == nil || !strings.HasSuffix(kept.Proxy.Upstream, strconv.Itoa(port2)) || kept.CORS != 1 {
		t.Errorf("after cors update: cors=%d proxy=%+v", kept.CORS, kept.Proxy)
	}

	t.Log("delete removes the site and its Caddy route")
	if _, status := httpDelete(t, "/api/sites/"+site.ID); status != http.StatusNoContent {
		t.Fatalf("delete: %d", status)
	}
	if caddyRouteExists(t, "vhost-"+site.ID) {
		t.Error("Caddy route still present after delete")
	}
	if _, status := httpGetRaw(t, "/api/sites/"+site.ID); status != http.StatusNotFound {
		t.Errorf("GET deleted site: %d, want 404", status)
	}
}

// TestProxySites_Validation checks the request boundary: bad upstreams and
// domains, PHP fields on a proxy site, and mixing the two site types.
func TestProxySites_Validation(t *testing.T) {
	const domain = "proxy-valid-go.test"
	deleteSiteByDomain(t, domain)

	for _, c := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"ftp upstream", map[string]any{"domain": domain, "proxy": map[string]any{"upstream": "ftp://127.0.0.1:21"}}, http.StatusBadRequest},
		{"upstream with path", map[string]any{"domain": domain, "proxy": map[string]any{"upstream": "http://127.0.0.1:5173/app"}}, http.StatusBadRequest},
		{"empty upstream", map[string]any{"domain": domain, "proxy": map[string]any{"upstream": ""}}, http.StatusBadRequest},
		{"no upstream", map[string]any{"domain": domain, "proxy": map[string]any{}}, http.StatusBadRequest},
		{"non .test domain", map[string]any{"domain": "proxy.example.com", "proxy": map[string]any{"upstream": "127.0.0.1:5173"}}, http.StatusBadRequest},
		{"root path on a proxy", map[string]any{"domain": domain, "root_path": "/tmp", "proxy": map[string]any{"upstream": "127.0.0.1:5173"}}, http.StatusBadRequest},
	} {
		if body, status := httpPost(t, "/api/sites", c.body); status != c.want {
			t.Errorf("%s: status %d, want %d: %s", c.name, status, c.want, body)
		}
	}

	body, status := httpPost(t, "/api/sites", map[string]any{"domain": domain, "proxy": map[string]any{"upstream": "127.0.0.1:5173"}})
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, body)
	}
	site := decodeJSON[proxySite](t, body)
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+site.ID) })

	if body, status := httpPost(t, "/api/sites", map[string]any{"domain": domain, "proxy": map[string]any{"upstream": "127.0.0.1:5174"}}); status != http.StatusConflict {
		t.Errorf("duplicate domain: %d, want 409: %s", status, body)
	}
	if body, status := httpPut(t, "/api/sites/"+site.ID, map[string]any{"domain": domain, "php_version": "8.4"}); status != http.StatusBadRequest {
		t.Errorf("php_version on a proxy site: %d, want 400: %s", status, body)
	}
	if body, status := httpPost(t, "/api/workers", map[string]any{"site_id": site.ID, "type": "custom", "command": "sleep 60"}); status != http.StatusBadRequest {
		t.Errorf("worker on a proxy site: %d, want 400: %s", status, body)
	}
	if body, status := httpPost(t, "/api/sites/"+site.ID+"/config/init", nil); status != http.StatusBadRequest {
		t.Errorf("config init on a proxy site: %d, want 400: %s", status, body)
	}

	checks := getDoctor(t)
	for _, id := range []string{"site-php", "site-roots"} {
		if strings.Contains(strings.Join(checks[id].Details, "\n"), domain) {
			t.Errorf("doctor %s flags the proxy site: %+v", id, checks[id])
		}
	}

	phpDir := t.TempDir()
	body, status = httpPost(t, "/api/sites", map[string]any{"domain": "proxy-php-go.test", "root_path": phpDir})
	if status != http.StatusCreated {
		t.Fatalf("create PHP site: %d %s", status, body)
	}
	php := decodeJSON[Site](t, body)
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+php.ID) })
	if body, status := httpPut(t, "/api/sites/"+php.ID, map[string]any{
		"domain": php.Domain, "root_path": phpDir, "proxy": map[string]any{"upstream": "127.0.0.1:5173"},
	}); status != http.StatusBadRequest {
		t.Errorf("proxy on a PHP site: %d, want 400: %s", status, body)
	}
}
