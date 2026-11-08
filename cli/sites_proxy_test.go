package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSitesProxy_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:proxy", wantArgs: true})
}
func TestSitesProxyRm_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:proxy:rm", wantArgs: true})
}

type proxyCall struct {
	route string
	body  map[string]any
}

// proxyAPI fakes the daemon with a PHP site shop.test and a proxy site
// vite.test, and records every write request.
func proxyAPI(t *testing.T) (*Client, *[]proxyCall) {
	t.Helper()
	var calls []proxyCall
	const vite = `{"id":"vite-test","domain":"vite.test","https":1,"cors":0,"aliases":"[\"hmr.vite.test\"]","proxy":{"upstream":"http://localhost:5173","preserve_host":false}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		route := r.Method + " " + r.URL.Path
		if route == "GET /api/sites" {
			w.Write([]byte(`[{"id":"shop-test","domain":"shop.test","root_path":"/srv/shop","php_version":"8.4"},` + vite + `]`))
			return
		}
		var body map[string]any
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &body)
		}
		calls = append(calls, proxyCall{route, body})
		switch route {
		case "POST /api/sites":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"api-test","domain":"api.test","https":1,"proxy":{"upstream":"http://127.0.0.1:3000","preserve_host":true}}`))
		case "PUT /api/sites/vite-test":
			w.Write([]byte(vite))
		case "DELETE /api/sites/vite-test":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{base: srv.URL, http: srv.Client()}, &calls
}

func TestSitesProxy_CreatesNewSite(t *testing.T) {
	c, calls := proxyAPI(t)
	if err := Find("sites:proxy").Handler(c, []string{"API.test", "127.0.0.1:3000", "--preserve-host"}, true); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].route != "POST /api/sites" {
		t.Fatalf("calls = %+v, want one create", *calls)
	}
	body := (*calls)[0].body
	proxy, _ := body["proxy"].(map[string]any)
	if body["domain"] != "api.test" || proxy["upstream"] != "127.0.0.1:3000" || proxy["preserve_host"] != true {
		t.Errorf("create body = %v", body)
	}
}

func TestSitesProxy_UpdatesExistingProxyKeepingAliases(t *testing.T) {
	c, calls := proxyAPI(t)
	if err := Find("sites:proxy").Handler(c, []string{"vite.test", "http://localhost:5174"}, true); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].route != "PUT /api/sites/vite-test" {
		t.Fatalf("calls = %+v, want one update", *calls)
	}
	body := (*calls)[0].body
	proxy, _ := body["proxy"].(map[string]any)
	aliases, _ := body["aliases"].([]any)
	if proxy["upstream"] != "http://localhost:5174" || len(aliases) != 1 || body["https"] != float64(1) {
		t.Errorf("update body = %v", body)
	}
}

func TestSitesProxy_RefusesPHPSite(t *testing.T) {
	c, calls := proxyAPI(t)
	err := Find("sites:proxy").Handler(c, []string{"shop.test", "127.0.0.1:3000"}, true)
	if err == nil || !strings.Contains(err.Error(), "PHP site") {
		t.Fatalf("err = %v, want a PHP site error", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %+v, want none", *calls)
	}
}

func TestSitesProxy_Usage(t *testing.T) {
	c, _ := proxyAPI(t)
	for _, args := range [][]string{{"vite.test"}, {"vite.test", "x", "y"}, {"vite.test", "x", "--bogus"}} {
		if err := Find("sites:proxy").Handler(c, args, true); err == nil {
			t.Errorf("args %v: want a usage error", args)
		}
	}
}

func TestSitesProxyRm_DeletesOnlyProxySites(t *testing.T) {
	c, calls := proxyAPI(t)
	if err := Find("sites:proxy:rm").Handler(c, []string{"shop.test"}, true); err == nil {
		t.Fatal("sites:proxy:rm must refuse a PHP site")
	}
	if err := Find("sites:proxy:rm").Handler(c, []string{"vite.test"}, true); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].route != "DELETE /api/sites/vite-test" {
		t.Fatalf("calls = %+v, want one delete", *calls)
	}
}
