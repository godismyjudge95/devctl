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
}

// UpsertVhost adds or replaces a vhost route in the Caddy HTTP server config.
// It uses the Caddy object-ID API: PATCH /id/{@id} if it exists, PUT otherwise.
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
		return nil
	}

	// If not found, append to the routes array.
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
	return nil
}

// DeleteVhost removes a vhost route by its @id.
func (c *CaddyClient) DeleteVhost(id string) error {
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
			"disable_redirects": false,
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

// buildRoute constructs the Caddy route JSON for a PHP site or WS proxy.
func buildRoute(cfg VhostConfig) map[string]interface{} {
	if cfg.SiteType == "ws" {
		return map[string]interface{}{
			"@id":      cfg.ID,
			"match":    []map[string]interface{}{{"host": cfg.Hosts}},
			"terminal": true,
			"handle": []map[string]interface{}{
				{
					"handler":   "reverse_proxy",
					"upstreams": []map[string]interface{}{{"dial": cfg.WSUpstream}},
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

	return map[string]interface{}{
		"@id":      cfg.ID,
		"match":    []map[string]interface{}{{"host": cfg.Hosts}},
		"terminal": true,
		"handle": []map[string]interface{}{
			{
				"handler": "subroute",
				"routes": []map[string]interface{}{
					// 1. Canonical-path redirect: if the path (without trailing slash)
					// maps to a directory that has an index.php, redirect to add
					// the trailing slash (308). Mirrors the first block of Caddy's
					// php_fastcgi expanded form and prevents /wp-admin → /wp-admin/
					// redirect loops in WordPress.
					{
						"match": []map[string]interface{}{
							{
								"file": map[string]interface{}{
									"root":      effectiveRoot,
									"try_files": []string{"{http.request.uri.path}/index.php"},
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
					// index, or root index.php). Uses try_policy first_exist_fallback
					// so index.php is always the final fallback even if not on disk.
					{
						"match": []map[string]interface{}{
							{"file": map[string]interface{}{
								"root":       effectiveRoot,
								"try_files":  []string{"{http.request.uri.path}", "{http.request.uri.path}/index.php", "index.php"},
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
				},
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
