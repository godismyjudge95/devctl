package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestSplitPHPArgs_FlagsAfterPositionals(t *testing.T) {
	pos, ver, j, err := splitPHPArgs([]string{"memory_limit=1G", "--version=8.4", "--json"}, "version")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pos, []string{"memory_limit=1G"}) || ver != "8.4" || !j {
		t.Errorf("got pos=%v ver=%q json=%v", pos, ver, j)
	}

	_, ver, _, err = splitPHPArgs([]string{"memory_limit=1G", "--version", "8.3"}, "version")
	if err != nil || ver != "8.3" {
		t.Errorf("space form: ver=%q err=%v", ver, err)
	}
}

func TestSplitPHPArgs_NegativeValueStaysPositional(t *testing.T) {
	pos, _, _, err := splitPHPArgs([]string{"all", "memory_limit", "-1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pos, []string{"all", "memory_limit", "-1"}) {
		t.Errorf("pos = %v", pos)
	}
}

func TestSplitPHPArgs_RejectsVersionWhereScopeIsPositional(t *testing.T) {
	if _, _, _, err := splitPHPArgs([]string{"all", "k", "v", "--version=8.4"}, ""); err == nil {
		t.Error("want error for --version on a scope-positional command")
	}
}

func TestSetPHPSettings_SendsOnlyGivenKeysAndVersion(t *testing.T) {
	var gotQuery string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"memory_limit":"1G","scope":"8.4","overrides":["memory_limit"]}`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	out, err := c.SetPHPSettings("8.4", map[string]string{"memory_limit": "1G"})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "version=8.4" {
		t.Errorf("query = %q, want version=8.4", gotQuery)
	}
	if len(gotBody) != 1 || gotBody["memory_limit"] != "1G" {
		t.Errorf("body = %v, want only memory_limit", gotBody)
	}
	if out.Scope != "8.4" || !slices.Equal(out.Overrides, []string{"memory_limit"}) {
		t.Errorf("decoded %+v", out)
	}
}

func TestSetPHPIni_EscapesKeyInPath(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.EscapedPath(), r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scope":"all","key":"opcache.jit","value":"tracing"}`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	if _, err := c.SetPHPIni("all", "opcache.jit", "tracing"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/php/ini/all/opcache.jit" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
}

func TestSplitPHPArgs_DoubleDashEndsFlags(t *testing.T) {
	pos, ver, j, err := splitPHPArgs([]string{"--version=8.4", "all", "--", "error_reporting", "--json"}, "version")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pos, []string{"all", "error_reporting", "--json"}) || ver != "8.4" || j {
		t.Errorf("got pos=%v ver=%q json=%v", pos, ver, j)
	}
}

func TestSplitPHPArgs_VersionWithoutValue(t *testing.T) {
	_, _, _, err := splitPHPArgs([]string{"memory_limit=1G", "--version"}, "version")
	if err == nil || err.Error() != "--version needs a value" {
		t.Errorf("err = %v, want --version needs a value", err)
	}
}

func TestPHPSettings_VersionFlagOrPositional(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scope":"8.4","overrides":[]}`))
	}))
	defer srv.Close()
	c := &Client{base: srv.URL, http: srv.Client()}
	cmd := Find("php:settings")

	for _, args := range [][]string{{"--version=8.4"}, {"--version", "8.4"}, {"8.4"}} {
		gotQuery = ""
		if err := cmd.Handler(c, args, true); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if gotQuery != "version=8.4" {
			t.Errorf("%v: query = %q, want version=8.4", args, gotQuery)
		}
	}

	gotQuery = "unset"
	if err := cmd.Handler(c, []string{"8.3", "--version=8.4"}, true); err == nil {
		t.Error("want error when the positional and --version disagree")
	}
	if gotQuery != "unset" {
		t.Errorf("request sent despite conflicting versions: %q", gotQuery)
	}
}

func TestPHPXdebug_SendsStateAndMode(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"available":true,"enabled":true,"mode":"debug,develop"}`))
	}))
	defer srv.Close()
	c := &Client{base: srv.URL, http: srv.Client()}
	cmd := Find("php:xdebug")

	if err := cmd.Handler(c, []string{"8.4", "on", "--mode=debug,develop", "--json"}, false); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/php/versions/8.4/xdebug" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
	if gotBody["enabled"] != true || gotBody["mode"] != "debug,develop" {
		t.Errorf("body = %v", gotBody)
	}

	if err := cmd.Handler(c, []string{"8.4", "off"}, true); err != nil {
		t.Fatal(err)
	}
	if gotBody["enabled"] != false || gotBody["mode"] != "" {
		t.Errorf("off body = %v", gotBody)
	}
}

func TestPHPXdebug_RejectsBadArgsWithoutCallingDaemon(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	c := &Client{base: srv.URL, http: srv.Client()}
	cmd := Find("php:xdebug")

	for _, args := range [][]string{{"8.4"}, {"8.4", "yes"}, {"8.4", "off", "--mode=debug"}, {"8.4", "on", "--mode"}} {
		if err := cmd.Handler(c, args, true); err == nil {
			t.Errorf("%v: want error", args)
		}
	}
	if called {
		t.Error("daemon called for invalid arguments")
	}
}

func TestPHPXdebug_ShowsDaemonError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"Xdebug is not available in this PHP build (PHP 8.4)"}`))
	}))
	defer srv.Close()
	c := &Client{base: srv.URL, http: srv.Client()}

	err := Find("php:xdebug").Handler(c, []string{"8.4", "on"}, true)
	if err == nil || !strings.Contains(err.Error(), "not available in this PHP build") {
		t.Errorf("err = %v", err)
	}
}
