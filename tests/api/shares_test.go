//go:build integration

package apitest

import (
	"crypto/tls"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type shareRow struct {
	SiteID string `json:"site_id"`
	Domain string `json:"domain"`
	Status string `json:"status"`
	URL    string `json:"url"`
	Error  string `json:"error"`
}

var quickTunnelURLRe = regexp.MustCompile(`^https://[a-z0-9-]+\.trycloudflare\.com$`)

func TestShares_UnknownSiteIs404(t *testing.T) {
	body, status := httpPost(t, "/api/sites/no-such-site-for-share/share", nil)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", status, body)
	}
}

func TestShares_UnshareIsIdempotent(t *testing.T) {
	body, status := httpDelete(t, "/api/sites/no-such-site-for-share/share")
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", status, body)
	}
}

func findShare(t *testing.T, siteID string) (shareRow, bool) {
	t.Helper()
	for _, sh := range decodeJSON[[]shareRow](t, httpGet(t, "/api/shares")) {
		if sh.SiteID == siteID {
			return sh, true
		}
	}
	return shareRow{}, false
}

// createShareSite makes a static site with Force HTTPS on, so the redirect
// route exists. It returns the site and the text its index page serves.
func createShareSite(t *testing.T, domain string) (Site, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "devctl-share-site-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	// Caddy runs as the site user, not as the test runner.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := "devctl-share-marker-" + filepath.Base(dir)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, s := range decodeJSON[[]Site](t, httpGet(t, "/api/sites")) {
		if s.Domain == domain {
			httpDelete(t, "/api/sites/"+s.ID)
		}
	}
	body, status := httpPost(t, "/api/sites", map[string]any{"domain": domain, "root_path": dir, "https": 1})
	if status != http.StatusCreated {
		t.Fatalf("create site: %d %s", status, body)
	}
	site := decodeJSON[Site](t, body)
	t.Cleanup(func() {
		httpDelete(t, "/api/sites/"+site.ID+"/share")
		httpDelete(t, "/api/sites/"+site.ID)
	})
	return site, marker
}

// TestShares_OriginHopServesSite sends the request cloudflared sends to Caddy
// (share.CloudflaredArgs): HTTPS to 127.0.0.1 with the site's SNI and Host,
// and the public host in X-Forwarded-Host. It needs no internet.
func TestShares_OriginHopServesSite(t *testing.T) {
	const domain = "share-origin-go.test"
	_, marker := createShareSite(t, domain)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			ServerName:         domain,
			InsecureSkipVerify: true,
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	// Caddy issues the new site's certificate in the background.
	last := "no attempt"
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(time.Second) {
		req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1:443/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = domain
		req.Header.Set("X-Forwarded-Host", "rapid-orange-pine-delta.trycloudflare.com")
		resp, err := client.Do(req)
		if err != nil {
			last = err.Error()
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && strings.Contains(string(b), marker) {
			return
		}
		last = resp.Status + " Location=" + resp.Header.Get("Location") + " body=" + string(b)
	}
	t.Fatalf("origin hop never served the site page: %s", last)
}

// TestShares_ShareListUnshare drives the full API flow with a real
// cloudflared quick tunnel. The API assertions always run. The public-URL
// check skips when trycloudflare.com cannot be reached from the test machine.
func TestShares_ShareListUnshare(t *testing.T) {
	const domain = "share-test-go.test"
	site, marker := createShareSite(t, domain)

	body, status := httpPost(t, "/api/sites/"+site.ID+"/share", nil)
	if status == http.StatusBadGateway && strings.Contains(string(body), "install cloudflared") {
		t.Skipf("cloudflared could not be downloaded, so no tunnel can start: %s", body)
	}
	if status != http.StatusOK {
		t.Fatalf("share: %d %s", status, body)
	}
	first := decodeJSON[shareRow](t, body)
	if first.SiteID != site.ID || first.Domain != domain {
		t.Fatalf("share = %+v, want site %s / %s", first, site.ID, domain)
	}
	switch first.Status {
	case "active":
		if !quickTunnelURLRe.MatchString(first.URL) {
			t.Errorf("url = %q, want a trycloudflare.com URL", first.URL)
		}
	case "failed":
		if first.Error == "" {
			t.Error("failed share has no error message")
		}
	case "starting":
	default:
		t.Fatalf("status = %q", first.Status)
	}

	var helperInstalled bool
	for _, h := range decodeJSON[[]HelperState](t, httpGet(t, "/api/helpers")) {
		if h.ID == "cloudflared" {
			helperInstalled = h.Installed
		}
	}
	if !helperInstalled {
		t.Error("sharing must install the cloudflared helper")
	}

	listed, ok := findShare(t, site.ID)
	if !ok {
		t.Fatal("GET /api/shares does not list the new share")
	}
	if listed.Domain != domain {
		t.Errorf("listed domain = %q", listed.Domain)
	}

	if first.Status == "active" {
		body, status = httpPost(t, "/api/sites/"+site.ID+"/share", nil)
		again := decodeJSON[shareRow](t, body)
		if status != http.StatusOK || again.URL != first.URL {
			t.Errorf("second share: %d %+v, want the same URL %s", status, again, first.URL)
		}
	}

	reachErr := ""
	if first.Status == "active" {
		reachErr = fetchUntil(first.URL, marker, 90*time.Second)
	} else {
		reachErr = "tunnel did not become active: " + first.Status + " " + first.Error
	}

	body, status = httpDelete(t, "/api/sites/"+site.ID+"/share")
	if status != http.StatusNoContent {
		t.Fatalf("unshare: %d %s", status, body)
	}
	if _, ok := findShare(t, site.ID); ok {
		t.Error("share still listed after unshare")
	}

	if reachErr != "" {
		t.Skipf("API flow passed; trycloudflare.com public check skipped: %s", reachErr)
	}
}

// fetchUntil polls url until the body contains want. A new quick tunnel
// hostname takes a few seconds to resolve. It returns "" on success.
func fetchUntil(url, want string, timeout time.Duration) string {
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(timeout)
	last := "no attempt"
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			last = err.Error()
		} else {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.Contains(string(b), want) {
				return ""
			}
			last = resp.Status
		}
		time.Sleep(3 * time.Second)
	}
	return url + " never served the site: " + last
}
