package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestSplitPHPArgs_FlagsAfterPositionals(t *testing.T) {
	pos, ver, j, err := splitPHPArgs([]string{"memory_limit=1G", "--version=8.4", "--json"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pos, []string{"memory_limit=1G"}) || ver != "8.4" || !j {
		t.Errorf("got pos=%v ver=%q json=%v", pos, ver, j)
	}

	_, ver, _, err = splitPHPArgs([]string{"memory_limit=1G", "--version", "8.3"}, true)
	if err != nil || ver != "8.3" {
		t.Errorf("space form: ver=%q err=%v", ver, err)
	}
}

func TestSplitPHPArgs_NegativeValueStaysPositional(t *testing.T) {
	pos, _, _, err := splitPHPArgs([]string{"all", "memory_limit", "-1"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pos, []string{"all", "memory_limit", "-1"}) {
		t.Errorf("pos = %v", pos)
	}
}

func TestSplitPHPArgs_RejectsVersionWhereScopeIsPositional(t *testing.T) {
	if _, _, _, err := splitPHPArgs([]string{"all", "k", "v", "--version=8.4"}, false); err == nil {
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
	pos, ver, j, err := splitPHPArgs([]string{"--version=8.4", "all", "--", "error_reporting", "--json"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pos, []string{"all", "error_reporting", "--json"}) || ver != "8.4" || j {
		t.Errorf("got pos=%v ver=%q json=%v", pos, ver, j)
	}
}

func TestSplitPHPArgs_VersionWithoutValue(t *testing.T) {
	_, _, _, err := splitPHPArgs([]string{"memory_limit=1G", "--version"}, true)
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
