package sites

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
}
