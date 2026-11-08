//go:build integration

package apitest

import (
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/php"
)

type phpXdebugStatus struct {
	Available bool   `json:"available"`
	Enabled   bool   `json:"enabled"`
	Mode      string `json:"mode"`
}

type phpVersionWithXdebug struct {
	Version string          `json:"version"`
	Status  string          `json:"status"`
	Xdebug  phpXdebugStatus `json:"xdebug"`
}

func phpVersionInfo(t *testing.T, ver string) phpVersionWithXdebug {
	t.Helper()
	for _, v := range decodeJSON[[]phpVersionWithXdebug](t, httpGet(t, "/api/php/versions")) {
		if v.Version == ver {
			return v
		}
	}
	t.Fatalf("PHP %s missing from /api/php/versions", ver)
	return phpVersionWithXdebug{}
}

func xdebugURL(ver string) string { return "/api/php/versions/" + ver + "/xdebug" }

func TestPHPXdebug_ReportsUnavailableOnBuildWithoutIt(t *testing.T) {
	ver := firstPHPVersion(t)
	restorePHPIniAfter(t)
	if _, err := os.Stat(php.XdebugPath(ver, phpTestServerRoot())); err == nil {
		t.Skip("this PHP build ships xdebug.so")
	}

	if st := phpVersionInfo(t, ver).Xdebug; st.Available || st.Enabled {
		t.Errorf("versions list xdebug = %+v, want unavailable and off", st)
	}
	body, status := httpPut(t, xdebugURL(ver), map[string]any{"enabled": true})
	if status != http.StatusConflict || !strings.Contains(string(body), "not available in this PHP build") {
		t.Errorf("PUT on: %d %s, want 409 not available in this PHP build", status, body)
	}
	if ds := decodeJSON[[]phpIniDirective](t, httpGet(t, "/api/php/ini?scope="+ver)); slices.ContainsFunc(ds, func(d phpIniDirective) bool { return d.Key == "zend_extension" }) {
		t.Errorf("refused toggle still stored directives: %+v", ds)
	}

	body, status = httpPut(t, xdebugURL(ver), map[string]any{"enabled": false})
	if status != http.StatusOK {
		t.Fatalf("PUT off: %d %s", status, body)
	}
	if st := decodeJSON[phpXdebugStatus](t, body); st.Enabled {
		t.Errorf("off response = %+v", st)
	}
}

// The test PHP is a stub, so a placeholder xdebug.so stands in for a build
// that ships one. FPM is not running, so nothing tries to load it.
func TestPHPXdebug_OnOffWritesManagedBlock(t *testing.T) {
	ver := firstPHPVersion(t)
	restorePHPIniAfter(t)
	root := phpTestServerRoot()
	so := php.XdebugPath(ver, root)
	if _, err := os.Stat(so); os.IsNotExist(err) {
		if phpVersionInfo(t, ver).Status == "running" {
			t.Skip("no xdebug.so in this PHP build and its FPM pool is running: a placeholder would be loaded on restart")
		}
		if err := os.WriteFile(so, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(so) })
	}
	iniPath := php.PHPIniPath(ver, root)

	body, status := httpPut(t, xdebugURL(ver), map[string]any{"enabled": true, "mode": "debug,develop"})
	if status != http.StatusOK {
		t.Fatalf("PUT on: %d %s", status, body)
	}
	if st := decodeJSON[phpXdebugStatus](t, body); !st.Available || !st.Enabled || st.Mode != "debug,develop" {
		t.Errorf("on response = %+v", st)
	}
	ini := mustReadFile(t, iniPath)
	at := strings.Index(ini, "devctl managed directives")
	if at < 0 {
		t.Fatalf("php.ini has no managed block:\n%s", ini)
	}
	block := ini[at:]
	for _, line := range []string{"zend_extension = " + so, "xdebug.mode = debug,develop", "xdebug.start_with_request = trigger"} {
		if !strings.Contains(block, line+"\n") {
			t.Errorf("managed block missing %q:\n%s", line, block)
		}
	}
	if st := phpVersionInfo(t, ver).Xdebug; !st.Enabled {
		t.Errorf("versions list after on = %+v", st)
	}

	body, status = httpPut(t, xdebugURL(ver), map[string]any{"enabled": false})
	if status != http.StatusOK {
		t.Fatalf("PUT off: %d %s", status, body)
	}
	if st := decodeJSON[phpXdebugStatus](t, body); st.Enabled || !st.Available {
		t.Errorf("off response = %+v", st)
	}
	if ini := mustReadFile(t, iniPath); strings.Contains(ini, "zend_extension = "+so) || strings.Contains(ini, "xdebug.mode =") {
		t.Errorf("php.ini still loads Xdebug after off:\n%s", ini)
	}
}

func TestPHPXdebug_RejectsBadInput(t *testing.T) {
	ver := firstPHPVersion(t)
	restorePHPIniAfter(t)
	cases := []struct {
		path string
		body any
		want int
	}{
		{xdebugURL(ver), map[string]any{"enabled": true, "mode": "step"}, http.StatusBadRequest},
		{xdebugURL("all"), map[string]any{"enabled": true}, http.StatusBadRequest},
		{xdebugURL("9.9"), map[string]any{"enabled": true}, http.StatusNotFound},
		{xdebugURL("latest"), map[string]any{"enabled": true}, http.StatusBadRequest},
	}
	for _, c := range cases {
		if body, status := httpPut(t, c.path, c.body); status != c.want {
			t.Errorf("PUT %s %v: %d %s, want %d", c.path, c.body, status, body, c.want)
		}
	}
}
