package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/newsite"
)

func TestSitesNew_IsRegistered(t *testing.T) {
	for _, flag := range []string{"template", "php", "starter-kit", "testing", "database", "admin-user", "admin-password", "admin-email"} {
		assertCmd(t, cmdCheck{name: "sites:new", wantArgs: true, wantFlag: flag})
	}
}

func TestSitesNew_DatabaseFlagNamesEveryTemplateDefault(t *testing.T) {
	for _, f := range Find("sites:new").Flags {
		if f.Name != "database" {
			continue
		}
		for _, want := range []string{"Laravel: sqlite, mysql, postgres (default sqlite)", "WordPress: mysql (default mysql)"} {
			if !strings.Contains(f.Description, want) {
				t.Errorf("database flag %q does not contain %q", f.Description, want)
			}
		}
		return
	}
	t.Fatal("database flag missing")
}

func TestParseNewSiteArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want newsite.Request
	}{
		{"equals form", []string{"shop", "--template=laravel", "--starter-kit=react", "--database=mysql"},
			newsite.Request{Name: "shop", Template: "laravel", Options: map[string]string{"starter_kit": "react", "database": "mysql"}}},
		{"space form, flags first", []string{"--template", "wordpress", "--php", "8.4", "--admin-user", "daniel", "blog"},
			newsite.Request{Name: "blog", Template: "wordpress", PHPVersion: "8.4", Options: map[string]string{"admin_user": "daniel"}}},
		{"no options", []string{"hello", "--template=php"},
			newsite.Request{Name: "hello", Template: "php", Options: map[string]string{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseNewSiteArgs(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseNewSiteArgs_Errors(t *testing.T) {
	for _, args := range [][]string{
		{"shop"},
		{"--template=php"},
		{"a", "b", "--template=php"},
		{"shop", "--template"},
	} {
		if _, err := parseNewSiteArgs(args); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestNewSiteSSE_SendsRequestAndReturnsSite(t *testing.T) {
	var got newsite.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sites/new" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: step\ndata: {\"index\":1,\"total\":3,\"label\":\"Register hello.test\"}\n\n" +
			"event: output\ndata: \"==> Register hello.test\\n\"\n\n" +
			"event: done\ndata: {\"site\":{\"id\":\"hello-test\",\"domain\":\"hello.test\",\"root_path\":\"/sites/hello\"},\"url\":\"https://hello.test\"}\n\n"))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	var lines []string
	res, err := c.NewSiteSSE(newsite.Request{Name: "hello", Template: "php"}, func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "hello" || got.Template != "php" {
		t.Errorf("server got %+v", got)
	}
	if res.URL != "https://hello.test" || res.Site.Domain != "hello.test" || res.Site.RootPath != "/sites/hello" {
		t.Errorf("result = %+v", res)
	}
	if !reflect.DeepEqual(lines, []string{"==> Register hello.test\n"}) {
		t.Errorf("output lines = %q", lines)
	}
}

func TestNewSiteSSE_ReturnsValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"site hello.test already exists"}`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	_, err := c.NewSiteSSE(newsite.Request{Name: "hello", Template: "php"}, nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
}
