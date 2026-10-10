package sites

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHasHTTPServer(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/config/apps/http/servers/devctl" {
				t.Errorf("path = %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		if !NewCaddyClient(srv.URL).HasHTTPServer() {
			t.Fatal("want true when server exists")
		}
	})
	t.Run("empty config", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		if NewCaddyClient(srv.URL).HasHTTPServer() {
			t.Fatal("want false when /config/ is empty")
		}
	})
	t.Run("down", func(t *testing.T) {
		if NewCaddyClient("http://127.0.0.1:1").HasHTTPServer() {
			t.Fatal("want false when admin API is down")
		}
	})
}

func TestEnsureHTTPServer_PatchesListenAndSkipsTrustInstall(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewCaddyClient(srv.URL)
	if err := c.EnsureHTTPServer("127.0.0.1:4000"); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "PATCH /config/apps/http/servers/devctl/listen") {
		t.Fatalf("missing PATCH listen, calls:\n%s", joined)
	}
	if !strings.Contains(joined, "PUT /config/apps/http/http_port") {
		t.Fatalf("missing PUT http_port, calls:\n%s", joined)
	}
	if !strings.Contains(joined, "PUT /config/apps/http/https_port") {
		t.Fatalf("missing PUT https_port, calls:\n%s", joined)
	}
	foundPKI := false
	for _, call := range calls {
		if !strings.Contains(call, "PUT /config/apps/pki") {
			continue
		}
		foundPKI = true
		var payload map[string]any
		idx := strings.Index(call, "{")
		if idx < 0 {
			t.Fatalf("pki call has no json: %s", call)
		}
		if err := json.Unmarshal([]byte(call[idx:]), &payload); err != nil {
			t.Fatal(err)
		}
		local := payload["certificate_authorities"].(map[string]any)["local"].(map[string]any)
		if local["install_trust"] != false {
			t.Fatalf("install_trust = %#v, want false", local["install_trust"])
		}
	}
	if !foundPKI {
		t.Fatalf("missing PUT pki, calls:\n%s", joined)
	}
	foundTLS := false
	for _, call := range calls {
		if !strings.Contains(call, "PUT /config/apps/tls") {
			continue
		}
		foundTLS = true
		if !strings.Contains(call, `"*.*.*.test"`) {
			t.Fatalf("tls policy missing *.*.*.test (needed for bucket.s3.maxio.test), call:\n%s", call)
		}
	}
	if !foundTLS {
		t.Fatalf("missing PUT tls, calls:\n%s", joined)
	}
}
