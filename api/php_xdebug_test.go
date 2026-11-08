package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/db"
	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/services"
)

func newXdebugTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	dir := php.PHPDir("8.4", root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"php-fpm": "#!/bin/sh\n", "php.ini": "[PHP]\nmemory_limit = 128M\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	s := &Server{mux: http.NewServeMux(), queries: dbq.New(sqlDB), serverRoot: root, supervisor: services.NewSupervisor(root)}
	s.mux.HandleFunc("GET /api/php/versions", s.handleGetPHPVersions)
	s.mux.HandleFunc("PUT /api/php/versions/{ver}/xdebug", s.handleSetPHPXdebug)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv, root
}

func putXdebug(t *testing.T, srv *httptest.Server, ver, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/php/versions/"+ver+"/xdebug", strings.NewReader(body))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func listedXdebug(t *testing.T, srv *httptest.Server) php.XdebugStatus {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/api/php/versions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var versions []php.Version
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil || len(versions) != 1 {
		t.Fatalf("versions: %v %+v", err, versions)
	}
	return versions[0].Xdebug
}

func TestSetPHPXdebug_RefusesBuildWithoutXdebug(t *testing.T) {
	srv, root := newXdebugTestServer(t)
	code, body := putXdebug(t, srv, "8.4", `{"enabled":true}`)
	if code != http.StatusConflict || !strings.Contains(body, "not available in this PHP build") {
		t.Errorf("on: %d %s", code, body)
	}
	if ini, _ := os.ReadFile(php.PHPIniPath("8.4", root)); strings.Contains(string(ini), "zend_extension") {
		t.Errorf("php.ini changed:\n%s", ini)
	}
	if st := listedXdebug(t, srv); st.Available || st.Enabled {
		t.Errorf("listed = %+v", st)
	}
	if code, body := putXdebug(t, srv, "8.4", `{"enabled":false}`); code != http.StatusOK {
		t.Errorf("off without Xdebug: %d %s", code, body)
	}
}

func TestSetPHPXdebug_OnThenOffRewritesOnlyThatVersion(t *testing.T) {
	srv, root := newXdebugTestServer(t)
	so := php.XdebugPath("8.4", root)
	if err := os.WriteFile(so, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	iniPath := php.PHPIniPath("8.4", root)

	code, body := putXdebug(t, srv, "8.4", `{"enabled":true}`)
	if code != http.StatusOK {
		t.Fatalf("on: %d %s", code, body)
	}
	var st php.XdebugStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil || !st.Enabled || st.Mode != php.DefaultXdebugMode {
		t.Errorf("on response = %+v (%v)", st, err)
	}
	ini, _ := os.ReadFile(iniPath)
	for _, line := range []string{"zend_extension = " + so, "xdebug.mode = debug", "xdebug.start_with_request = trigger"} {
		if !strings.Contains(string(ini), line+"\n") {
			t.Errorf("php.ini missing %q:\n%s", line, ini)
		}
	}
	if st := listedXdebug(t, srv); !st.Enabled {
		t.Errorf("listed after on = %+v", st)
	}

	if code, body := putXdebug(t, srv, "8.4", `{"enabled":false}`); code != http.StatusOK {
		t.Fatalf("off: %d %s", code, body)
	}
	ini, _ = os.ReadFile(iniPath)
	if strings.Contains(string(ini), "zend_extension") || strings.Contains(string(ini), "xdebug.mode") {
		t.Errorf("php.ini still loads Xdebug:\n%s", ini)
	}
	if st := listedXdebug(t, srv); st.Enabled || !st.Available {
		t.Errorf("listed after off = %+v", st)
	}
}

func TestSetPHPXdebug_RejectsBadRequests(t *testing.T) {
	srv, root := newXdebugTestServer(t)
	if err := os.WriteFile(php.XdebugPath("8.4", root), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ver, body string
		want      int
	}{
		{"8.4", `{"enabled":true,"mode":"off"}`, http.StatusBadRequest},
		{"8.4", `not json`, http.StatusBadRequest},
		{"all", `{"enabled":true}`, http.StatusBadRequest},
		{"9.9", `{"enabled":true}`, http.StatusNotFound},
	} {
		if code, body := putXdebug(t, srv, c.ver, c.body); code != c.want {
			t.Errorf("%s %s: %d %s, want %d", c.ver, c.body, code, body, c.want)
		}
	}
}
