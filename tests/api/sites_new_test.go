//go:build integration

package apitest

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type siteTemplate struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Options []struct {
		ID      string `json:"id"`
		Default string `json:"default"`
		Choices []struct {
			Value string `json:"value"`
		} `json:"choices"`
	} `json:"options"`
}

type newSiteDone struct {
	Site Site   `json:"site"`
	URL  string `json:"url"`
}

func newSiteName(prefix string) string {
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func sitesWatchDir(t *testing.T) string {
	t.Helper()
	dir := decodeJSON[map[string]string](t, httpGet(t, "/api/settings"))["sites_watch_dir"]
	if dir == "" {
		t.Fatal("sites_watch_dir is not set")
	}
	return dir
}

func siteByDomain(t *testing.T, domain string) (Site, bool) {
	t.Helper()
	for _, s := range decodeJSON[[]Site](t, httpGet(t, "/api/sites")) {
		if s.Domain == domain {
			return s, true
		}
	}
	return Site{}, false
}

// runningPHPVersion returns a PHP version whose FPM pool runs, or "".
func runningPHPVersion(t *testing.T) string {
	t.Helper()
	for _, s := range decodeJSON[[]ServiceState](t, httpGet(t, "/api/services")) {
		if v, ok := strings.CutPrefix(s.ID, "php-fpm-"); ok && s.Status == "running" {
			return v
		}
	}
	return ""
}

func TestSitesNew_TemplatesListsEveryTemplate(t *testing.T) {
	resp := decodeJSON[struct {
		Templates []siteTemplate `json:"templates"`
	}](t, httpGet(t, "/api/sites/templates"))
	ids := []string{}
	byID := map[string]siteTemplate{}
	for _, tpl := range resp.Templates {
		ids = append(ids, tpl.ID)
		byID[tpl.ID] = tpl
	}
	if strings.Join(ids, ",") != "laravel,statamic,wordpress,php" {
		t.Fatalf("template IDs = %v", ids)
	}
	var kits []string
	for _, o := range byID["laravel"].Options {
		if o.ID == "starter_kit" {
			for _, c := range o.Choices {
				kits = append(kits, c.Value)
			}
		}
	}
	if strings.Join(kits, ",") != "none,react,vue,svelte,livewire" {
		t.Errorf("laravel starter kits = %v", kits)
	}
}

func TestSitesNew_ValidationErrors(t *testing.T) {
	root := sitesWatchDir(t)

	takenSite := newSiteName("newsite-taken")
	tmp := t.TempDir()
	body, status := httpPost(t, "/api/sites", map[string]any{"domain": takenSite + ".test", "root_path": tmp})
	if status != http.StatusCreated {
		t.Fatalf("create site: %d %s", status, body)
	}
	created := decodeJSON[Site](t, body)
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+created.ID) })

	takenDir := newSiteName("newsite-dir")
	if err := os.Mkdir(filepath.Join(root, takenDir), 0o777); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(root, takenDir))
		if s, ok := siteByDomain(t, takenDir+".test"); ok {
			httpDelete(t, "/api/sites/"+s.ID)
		}
	})

	cases := []struct {
		name   string
		body   map[string]any
		status int
		want   string
	}{
		{"invalid name", map[string]any{"name": "My App", "template": "php"}, 400, "invalid name"},
		{"reserved name", map[string]any{"name": "server", "template": "php"}, 400, "reserved"},
		{"unknown template", map[string]any{"name": newSiteName("newsite-x"), "template": "rails"}, 400, "unknown template"},
		{"unknown option", map[string]any{"name": newSiteName("newsite-x"), "template": "php", "options": map[string]string{"database": "mysql"}}, 400, "unknown option"},
		{"bad choice", map[string]any{"name": newSiteName("newsite-x"), "template": "laravel", "options": map[string]string{"starter_kit": "angular"}}, 400, "invalid starter_kit"},
		{"php not installed", map[string]any{"name": newSiteName("newsite-x"), "template": "php", "php_version": "5.1"}, 400, "PHP 5.1 is not installed"},
		{"site exists", map[string]any{"name": takenSite, "template": "php"}, 409, "already exists"},
		{"directory exists", map[string]any{"name": takenDir, "template": "php"}, 409, "already exists"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, status := httpPost(t, "/api/sites/new", tc.body)
			if status != tc.status || !strings.Contains(string(body), tc.want) {
				t.Fatalf("status %d body %s, want %d containing %q", status, body, tc.status, tc.want)
			}
		})
	}
}

// TestSitesNew_PlainPHPEndToEnd creates a plain PHP project without network
// access: directory, index.php, .env, site row, Caddy route, and an HTTPS
// response from PHP through Caddy.
func TestSitesNew_PlainPHPEndToEnd(t *testing.T) {
	root := sitesWatchDir(t)
	name := newSiteName("newsite-plain")
	phpVersion := runningPHPVersion(t)

	req := map[string]any{"name": name, "template": "php"}
	if phpVersion != "" {
		req["php_version"] = phpVersion
	}
	res := httpSSEBody(t, http.MethodPost, "/api/sites/new", req, 2*time.Minute)
	t.Cleanup(func() {
		if s, ok := siteByDomain(t, name+".test"); ok {
			httpDelete(t, "/api/sites/"+s.ID)
		}
		os.RemoveAll(filepath.Join(root, name))
	})
	if res.LastEvent != "done" {
		t.Fatalf("stream ended with %q: %s (events %v)", res.LastEvent, res.LastData, res.Events)
	}
	steps := 0
	for _, e := range res.Events {
		if e == "step" {
			steps++
		}
	}
	if steps != 3 {
		t.Errorf("step events = %d, want 3 (register, index.php, .env)", steps)
	}

	done := decodeJSON[newSiteDone](t, []byte(res.LastData))
	wantDir := filepath.Join(root, name)
	if done.URL != "https://"+name+".test" || done.Site.Domain != name+".test" || done.Site.RootPath != wantDir {
		t.Fatalf("done = %+v, want domain %s.test at %s", done, name, wantDir)
	}
	if phpVersion != "" && done.Site.PhpVersion != phpVersion {
		t.Errorf("php_version = %q, want %q", done.Site.PhpVersion, phpVersion)
	}
	if done.Site.AutoDiscovered != 0 {
		t.Error("the wizard's site must not be the watcher's auto-discovered row")
	}

	if _, err := os.Stat(filepath.Join(wantDir, "index.php")); err != nil {
		t.Errorf("index.php: %v", err)
	}
	env, err := os.ReadFile(filepath.Join(wantDir, ".env"))
	if err != nil || !strings.Contains(string(env), "APP_URL=https://"+name+".test\n") {
		t.Errorf(".env = %q, %v", env, err)
	}
	if !caddyRouteExists(t, "vhost-"+done.Site.ID) {
		t.Errorf("Caddy route vhost-%s missing", done.Site.ID)
	}

	again, status := httpPost(t, "/api/sites/new", map[string]any{"name": name, "template": "php"})
	if status != http.StatusConflict {
		t.Errorf("second create: status %d %s, want 409", status, again)
	}

	if phpVersion == "" {
		t.Skip("no PHP-FPM pool is running in this test machine; skipping the HTTPS check")
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Caddy internal CA
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, "127.0.0.1:443")
			},
		},
	}
	var got string
	var code int
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(done.URL + "/")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			got, code = string(b), resp.StatusCode
			if code == http.StatusOK && strings.Contains(got, "PHP "+phpVersion) {
				break
			}
		} else {
			got = err.Error()
		}
		time.Sleep(500 * time.Millisecond)
	}
	if code != http.StatusOK || !strings.Contains(got, "PHP "+phpVersion) || strings.Contains(got, "<?=") {
		t.Fatalf("GET %s: status %d body %q, want PHP %s output", done.URL, code, got, phpVersion)
	}
}
