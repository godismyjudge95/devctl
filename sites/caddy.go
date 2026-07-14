package sites

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgormly/devctl/php"
)

// CaddyClient wraps the Caddy Admin API.
type CaddyClient struct {
	adminURL string
	http     *http.Client
}

// NewCaddyClient creates a CaddyClient targeting the given admin URL
// (e.g. "http://localhost:2019").
func NewCaddyClient(adminURL string) *CaddyClient {
	return &CaddyClient{
		adminURL: strings.TrimRight(adminURL, "/"),
		http:     &http.Client{Timeout: 2 * time.Second},
	}
}

// AdminURL returns the base URL of the Caddy admin API.
func (c *CaddyClient) AdminURL() string { return c.adminURL }

// WaitForAdmin polls the Caddy Admin API until it responds or timeout elapses.
func (c *CaddyClient) WaitForAdmin(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(c.adminURL + "/config/")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s", timeout)
}

// vhostRoute is the JSON structure Caddy expects for a single vhost route.
type vhostRoute struct {
	ID       string        `json:"@id"`
	Match    []matchBlock  `json:"match"`
	Handle   []handleBlock `json:"handle"`
	Terminal bool          `json:"terminal"`
}

type matchBlock struct {
	Host []string `json:"host"`
}

type handleBlock struct {
	Handler string       `json:"handler"`
	Routes  []innerRoute `json:"routes,omitempty"`
}

type innerRoute struct {
	Match  []json.RawMessage `json:"match,omitempty"`
	Handle []json.RawMessage `json:"handle,omitempty"`
}

// VhostConfig is what callers pass to UpsertVhost.
type VhostConfig struct {
	ID         string   // unique route @id, e.g. "vhost-myapp-test"
	Hosts      []string // all domains (primary + aliases)
	RootPath   string
	PublicDir  string // subdirectory within RootPath to use as document root (e.g. "public")
	PHPVersion string // e.g. "8.3"
	HTTPS      bool
	// SiteType is "php" (default) or "ws" for a WebSocket reverse proxy.
	SiteType string
	// WSUpstream is the dial address for WS sites, e.g. "127.0.0.1:7383".
	WSUpstream string
	// ServerRoot is the devctl server root directory, used to locate the PHP-FPM socket.
	ServerRoot string
	// EnableCORS injects permissive Access-Control-* headers on every response.
	// Controlled per-site via the sites.cors column (same pattern as https).
	EnableCORS bool
}

// UpsertVhost adds or replaces a vhost route in the Caddy HTTP server config.
// It uses the Caddy object-ID API: PATCH /id/{@id} if it exists, PUT otherwise.
//
// For sites with HTTPS=true it also ensures an HTTP→HTTPS redirect route
// (inserted early in the routes list). When HTTPS=false any prior redirect
// for the vhost is removed. This makes the per-site "Force HTTPS" checkbox
// actually control redirects.
func (c *CaddyClient) UpsertVhost(cfg VhostConfig) error {
	route := buildRoute(cfg)
	body, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("marshal caddy route: %w", err)
	}

	// Try PATCH first (update existing).
	patchURL := fmt.Sprintf("%s/id/%s", c.adminURL, cfg.ID)
	resp, err := c.http.Do(mustRequest("PATCH", patchURL, body))
	if err != nil {
		return fmt.Errorf("caddy PATCH: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusOK {
		// Content updated. Now ensure/clean the redirect companion.
		c.syncHTTPSRedirect(cfg)
		return nil
	}

	// If not found, prepend to the routes array (at 0).
	putURL := fmt.Sprintf("%s/config/apps/http/servers/devctl/routes/0", c.adminURL)
	resp2, err := c.http.Do(mustRequest("PUT", putURL, body))
	if err != nil {
		return fmt.Errorf("caddy PUT: %w", err)
	}
	defer resp2.Body.Close()
	io.Copy(io.Discard, resp2.Body)

	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("caddy PUT returned %d", resp2.StatusCode)
	}

	c.syncHTTPSRedirect(cfg)
	return nil
}

// deleteRouteByID is a small helper to DELETE a specific @id route. It is
// best-effort and swallows not-found.
func (c *CaddyClient) deleteRouteByID(id string) error {
	url := fmt.Sprintf("%s/id/%s", c.adminURL, id)
	resp, err := c.http.Do(mustRequest("DELETE", url, nil))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("caddy DELETE %s returned %d", id, resp.StatusCode)
	}
	return nil
}

// syncHTTPSRedirect ensures the companion redirect route exists (and is
// positioned early) when cfg.HTTPS is true, or removes it when false.
func (c *CaddyClient) syncHTTPSRedirect(cfg VhostConfig) {
	redirectID := cfg.ID + "-https-redirect"
	if cfg.HTTPS {
		if err := c.upsertHTTPSRedirect(cfg.Hosts, redirectID); err != nil {
			fmt.Printf("sites: caddy https redirect upsert error for %s: %v\n", cfg.ID, err)
		}
	} else {
		// Remove redirect if it exists (best effort).
		_ = c.deleteRouteByID(redirectID)
	}
}

// upsertHTTPSRedirect creates or updates an explicit HTTP→HTTPS redirect
// route for the given hosts. It is only created for sites where Force HTTPS
// is enabled. The redirect only triggers on plain HTTP (via the protocol
// matcher) to avoid loops on HTTPS requests. The route is prepended (PUT
// /routes/0) so it is evaluated before the main content route.
func (c *CaddyClient) upsertHTTPSRedirect(hosts []string, id string) error {
	if len(hosts) == 0 {
		return nil
	}

	route := buildHTTPSRedirectRoute(hosts, id)

	body, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("marshal redirect route: %w", err)
	}

	// Try PATCH first (update existing redirect route).
	patchURL := fmt.Sprintf("%s/id/%s", c.adminURL, id)
	resp, err := c.http.Do(mustRequest("PATCH", patchURL, body))
	if err != nil {
		return fmt.Errorf("caddy redirect PATCH: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// Not present — prepend at routes/0 so the redirect takes precedence over
	// the main content route for the same hosts.
	putURL := fmt.Sprintf("%s/config/apps/http/servers/devctl/routes/0", c.adminURL)
	resp2, err := c.http.Do(mustRequest("PUT", putURL, body))
	if err != nil {
		return fmt.Errorf("caddy redirect PUT: %w", err)
	}
	defer resp2.Body.Close()
	io.Copy(io.Discard, resp2.Body)

	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("caddy redirect PUT returned %d", resp2.StatusCode)
	}
	return nil
}

// DeleteVhost removes a vhost route by its @id.
// It also cleans up any associated HTTPS redirect route for the same vhost.
func (c *CaddyClient) DeleteVhost(id string) error {
	// Delete main content route.
	url := fmt.Sprintf("%s/id/%s", c.adminURL, id)
	resp, err := c.http.Do(mustRequest("DELETE", url, nil))
	if err != nil {
		return fmt.Errorf("caddy DELETE: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("caddy DELETE returned %d", resp.StatusCode)
	}

	// Best-effort delete for the optional https-redirect companion route.
	_ = c.deleteRouteByID(id + "-https-redirect")
	return nil
}

// RootCert fetches the Caddy internal CA root certificate (PEM).
// It calls /pki/ca/local and extracts the root_certificate field from the JSON
// response (as opposed to /pki/ca/local/certificates which returns the
// intermediate certificate chain).
func (c *CaddyClient) RootCert() ([]byte, error) {
	resp, err := c.http.Get(c.adminURL + "/pki/ca/local")
	if err != nil {
		return nil, fmt.Errorf("caddy pki: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		RootCertificate string `json:"root_certificate"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("caddy pki decode: %w", err)
	}
	if result.RootCertificate == "" {
		return nil, fmt.Errorf("caddy pki: root_certificate field is empty")
	}
	return []byte(result.RootCertificate), nil
}

// EnsureHTTPServer ensures the Caddy config has an HTTP server named "devctl"
// listening on :80/:443, the TLS automation policy uses Caddy's internal CA
// for *.test domains, and a reverse-proxy vhost for devctl.test points at
// devctlAddr (e.g. "127.0.0.1:4000").
// This is idempotent — safe to call on startup or after Caddy restarts.
func (c *CaddyClient) EnsureHTTPServer(devctlAddr string) error {
	serverConfig := map[string]interface{}{
		"listen": []string{":80", ":443"},
		"routes": []interface{}{},
		"automatic_https": map[string]interface{}{
			"disable":           false,
			"disable_redirects": true, // per-site "Force HTTPS" controls explicit redirects instead
		},
	}

	// Only PUT the HTTP server config if it doesn't exist yet.
	checkURL := fmt.Sprintf("%s/config/apps/http/servers/devctl", c.adminURL)
	resp, err := c.http.Get(checkURL)
	if err != nil {
		return fmt.Errorf("caddy check server: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := json.Marshal(serverConfig)
		putURL := fmt.Sprintf("%s/config/apps/http/servers/devctl", c.adminURL)
		resp2, err := c.http.Do(mustRequest("PUT", putURL, body))
		if err != nil {
			return fmt.Errorf("caddy PUT server: %w", err)
		}
		defer resp2.Body.Close()
		io.Copy(io.Discard, resp2.Body)
	}

	// Configure the TLS app to use Caddy's internal CA for *.test domains.
	// Without this, automatic_https defaults to Let's Encrypt, which rejects
	// non-public TLDs like .test.
	tlsAutomation := map[string]interface{}{
		"automation": map[string]interface{}{
			"policies": []map[string]interface{}{
				{
					"subjects": []string{"*.test", "*.*.test", "devctl.test"},
					"issuers": []map[string]interface{}{
						{"module": "internal"},
					},
				},
			},
		},
	}

	// Always PUT the TLS automation policy so the internal CA is applied even if
	// Caddy was restarted and lost its in-memory state. This is idempotent.
	tlsBody, _ := json.Marshal(tlsAutomation)
	putTLSURL := fmt.Sprintf("%s/config/apps/tls", c.adminURL)
	respTLS2, err := c.http.Do(mustRequest("PUT", putTLSURL, tlsBody))
	if err != nil {
		return fmt.Errorf("caddy PUT tls: %w", err)
	}
	defer respTLS2.Body.Close()
	io.Copy(io.Discard, respTLS2.Body)

	// Ensure the devctl.test reverse-proxy route is present.
	if err := c.UpsertVhost(VhostConfig{
		ID:         "vhost-devctl-test",
		Hosts:      []string{"devctl.test"},
		HTTPS:      true,
		SiteType:   "ws",
		WSUpstream: devctlAddr,
	}); err != nil {
		return fmt.Errorf("caddy devctl.test vhost: %w", err)
	}

	return nil
}

// corsResponseHeaders are applied to every site so local *.test apps can make
// cross-origin requests without browser CORS enforcement. Caddy does not add
// these by default — devctl injects them into each vhost route.
var corsResponseHeaders = map[string]interface{}{
	"Access-Control-Allow-Origin":  []string{"*"},
	"Access-Control-Allow-Methods": []string{"*"},
	"Access-Control-Allow-Headers": []string{"*"},
}

// corsHeadersHandler returns a Caddy headers middleware that replaces any
// upstream Access-Control-* values with our permissive dev defaults.
//
// Must sit in the same handle chain as reverse_proxy/file_server (as an
// earlier middleware), not in a separate subroute — deferred ops only wrap
// handlers that are next() in the same chain.
//
// deferred=true so set runs when the upstream response is written, replacing
// backend CORS (e.g. MaxIO echoing Origin). Do NOT also delete the same
// header names: in Caddy, delete is applied after set and would wipe our
// values, leaving no Access-Control-Allow-Origin at all.
func corsHeadersHandler() map[string]interface{} {
	return map[string]interface{}{
		"handler": "headers",
		"response": map[string]interface{}{
			"deferred": true,
			"set":      corsResponseHeaders,
		},
	}
}

// corsPreflightRoute answers OPTIONS with 204 + permissive CORS headers
// without contacting the upstream (required for browser preflights).
func corsPreflightRoute() map[string]interface{} {
	return map[string]interface{}{
		"match": []map[string]interface{}{
			{"method": []string{"OPTIONS"}},
		},
		"terminal": true,
		"handle": []map[string]interface{}{
			{
				"handler":     "static_response",
				"status_code": 204,
				"headers":     corsResponseHeaders,
			},
		},
	}
}

// buildHTTPSRedirectRoute constructs the Caddy route JSON for an HTTP→HTTPS
// redirect. It uses the protocol matcher (not CEL expressions) so it works
// with the standard Caddy binary.
func buildHTTPSRedirectRoute(hosts []string, id string) map[string]interface{} {
	return map[string]interface{}{
		"@id": id,
		"match": []map[string]interface{}{
			{
				"host":     hosts,
				"protocol": "http",
			},
		},
		"terminal": true,
		"handle": []map[string]interface{}{
			{
				"handler":     "static_response",
				"status_code": 308,
				"headers": map[string]interface{}{
					"Location": []string{"https://{http.request.host}{http.request.uri}"},
				},
			},
		},
	}
}

// buildRoute constructs the Caddy route JSON for a PHP site or WS proxy.
func buildRoute(cfg VhostConfig) map[string]interface{} {
	if cfg.SiteType == "ws" {
		proxyHandler := map[string]interface{}{
			"handler":   "reverse_proxy",
			"upstreams": []map[string]interface{}{{"dial": cfg.WSUpstream}},
		}
		if !cfg.EnableCORS {
			return map[string]interface{}{
				"@id":      cfg.ID,
				"match":    []map[string]interface{}{{"host": cfg.Hosts}},
				"terminal": true,
				"handle":   []map[string]interface{}{proxyHandler},
			}
		}
		// OPTIONS preflight is terminal; everything else is headers→proxy in one
		// middleware chain so deferred delete+set replaces upstream CORS.
		return map[string]interface{}{
			"@id":      cfg.ID,
			"match":    []map[string]interface{}{{"host": cfg.Hosts}},
			"terminal": true,
			"handle": []map[string]interface{}{
				{
					"handler": "subroute",
					"routes": []map[string]interface{}{
						corsPreflightRoute(),
						{
							"handle": []map[string]interface{}{
								corsHeadersHandler(),
								proxyHandler,
							},
						},
					},
				},
			},
		}
	}

	sock := "unix/" + php.FPMSocket(cfg.PHPVersion, cfg.ServerRoot)

	// Compute the effective document root (project root + optional public subdirectory).
	effectiveRoot := cfg.RootPath
	if cfg.PublicDir != "" {
		effectiveRoot = filepath.Join(cfg.RootPath, cfg.PublicDir)
	}

	// Supported index files for directory indexes. Order controls precedence:
	// index.php before html so PHP wins when both index.php and index.html exist.
	indexFiles := []string{"index.php", "index.html", "index.htm"}

	// Build try_files list for the canonical dir redirect (any of these present triggers redirect).
	dirIndexTry := make([]string, len(indexFiles))
	for i, f := range indexFiles {
		dirIndexTry[i] = "{http.request.uri.path}/" + f
	}

	// Build try_files for rewrite: exact path, then {path}/index.*, then bare index.*
	// last entry is used by first_exist_fallback when nothing exists.
	rewriteTry := []string{"{http.request.uri.path}"}
	for _, f := range indexFiles {
		rewriteTry = append(rewriteTry, "{http.request.uri.path}/"+f)
	}
	rewriteTry = append(rewriteTry, "index.html", "index.htm", "index.php")

	routes := []map[string]interface{}{
		// 1. Canonical-path redirect: if the path (without trailing slash)
		// maps to a directory that has an index file (index.php / .html / .htm),
		// redirect to add the trailing slash (308). Mirrors the first block of
		// Caddy's php_fastcgi expanded form and prevents /wp-admin → /wp-admin/
		// redirect loops in WordPress.
		{
			"match": []map[string]interface{}{
				{
					"file": map[string]interface{}{
						"root":      effectiveRoot,
						"try_files": dirIndexTry,
					},
					"not": []map[string]interface{}{
						{"path": []string{"*/"}},
					},
				},
			},
			"handle": []map[string]interface{}{
				{
					"handler":     "static_response",
					"status_code": 308,
					"headers": map[string]interface{}{
						"Location": []string{"{http.request.orig_uri.path}/"},
					},
				},
			},
		},
		// 2. Rewrite to the best matching file (exact path, directory
		// index, or root index file). Uses try_policy first_exist_fallback
		// so the last entry is always the final fallback even if not on disk.
		{
			"match": []map[string]interface{}{
				{"file": map[string]interface{}{
					"root":       effectiveRoot,
					"try_files":  rewriteTry,
					"try_policy": "first_exist_fallback",
					"split_path": []string{".php"},
				}},
			},
			"handle": []map[string]interface{}{
				{
					"handler": "rewrite",
					"uri":     "{http.matchers.file.relative}",
				},
			},
		},
		// 3. Serve real static non-PHP files directly.
		{
			"match": []map[string]interface{}{
				{
					"file": map[string]interface{}{
						"root":      effectiveRoot,
						"try_files": []string{"{http.request.uri.path}"},
					},
					"not": []map[string]interface{}{
						{"path": []string{"*.php"}},
					},
				},
			},
			"handle": []map[string]interface{}{
				{"handler": "file_server", "root": effectiveRoot},
			},
		},
		// 4. Pass all *.php requests to PHP-FPM via FastCGI.
		{
			"match": []map[string]interface{}{
				{"path": []string{"*.php"}},
			},
			"handle": []map[string]interface{}{
				{
					"handler":   "reverse_proxy",
					"upstreams": []map[string]interface{}{{"dial": sock}},
					"transport": map[string]interface{}{
						"protocol":   "fastcgi",
						"root":       effectiveRoot,
						"split_path": []string{".php"},
						// Tell PHP it is behind HTTPS. Caddy terminates TLS for
						// all *.test sites, but the FastCGI protocol carries no
						// TLS signal by default. Without these, WordPress (and
						// other frameworks) think the request is plain HTTP and
						// issue an infinite HTTPS redirect loop.
						"env": map[string]string{
							"HTTPS":                  "on",
							"HTTP_X_FORWARDED_PROTO": "https",
						},
					},
				},
			},
		},
	}

	if cfg.EnableCORS {
		// Preflight is terminal inside the subroute; headers middleware wraps the
		// whole subroute so every response (static, PHP, redirects) gets a single
		// Access-Control-Allow-Origin after any upstream CORS is stripped.
		routes = append([]map[string]interface{}{corsPreflightRoute()}, routes...)
		return map[string]interface{}{
			"@id":      cfg.ID,
			"match":    []map[string]interface{}{{"host": cfg.Hosts}},
			"terminal": true,
			"handle": []map[string]interface{}{
				corsHeadersHandler(),
				{
					"handler": "subroute",
					"routes":  routes,
				},
			},
		}
	}

	return map[string]interface{}{
		"@id":      cfg.ID,
		"match":    []map[string]interface{}{{"host": cfg.Hosts}},
		"terminal": true,
		"handle": []map[string]interface{}{
			{
				"handler": "subroute",
				"routes":  routes,
			},
		},
	}
}

func mustRequest(method, url string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r, _ = http.NewRequest(method, url, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r, _ = http.NewRequest(method, url, nil)
	}
	return r
}
