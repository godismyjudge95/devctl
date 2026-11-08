package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkersCommands_AreRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "workers:list", wantArgs: true})
	assertCmd(t, cmdCheck{name: "workers:add", wantArgs: true, wantFlag: "command"})
	assertCmd(t, cmdCheck{name: "workers:remove", wantArgs: true})
	assertCmd(t, cmdCheck{name: "workers:start", wantArgs: true})
	assertCmd(t, cmdCheck{name: "workers:stop", wantArgs: true})
	assertCmd(t, cmdCheck{name: "workers:restart", wantArgs: true})
	assertCmd(t, cmdCheck{name: "workers:logs", wantArgs: true, wantFlag: "lines"})
}

func TestLastLines(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"a\nb\nc\n", 2, "b\nc"},
		{"a\nb\n", 5, "a\nb"},
		{"", 3, ""},
		{"a\n", 0, ""},
	}
	for _, c := range cases {
		if got := lastLines(c.in, c.n); got != c.want {
			t.Errorf("lastLines(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestFindWorker_ByTypeOrID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sites":
			_ = json.NewEncoder(w).Encode([]Site{{ID: "s1", Domain: "app.test"}})
		case "/api/workers":
			if r.URL.Query().Get("site_id") != "s1" {
				t.Errorf("site_id = %q, want s1", r.URL.Query().Get("site_id"))
			}
			_ = json.NewEncoder(w).Encode([]Worker{
				{ID: 1, Domain: "app.test", Type: "queue"},
				{ID: 2, Domain: "app.test", Type: "custom"},
				{ID: 3, Domain: "app.test", Type: "custom"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{base: srv.URL, http: srv.Client()}

	if w, err := findWorker(c, "app.test", "queue"); err != nil || w.ID != 1 {
		t.Errorf("by type: %+v, %v; want worker 1", w, err)
	}
	if w, err := findWorker(c, "app.test", "3"); err != nil || w.ID != 3 {
		t.Errorf("by id: %+v, %v; want worker 3", w, err)
	}
	if _, err := findWorker(c, "app.test", "custom"); err == nil || !strings.Contains(err.Error(), "use the id (2, 3)") {
		t.Errorf("two custom workers: err = %v, want a hint to use the id", err)
	}
	if _, err := findWorker(c, "app.test", "vite"); err == nil || !strings.Contains(err.Error(), "no vite worker") {
		t.Errorf("missing type: err = %v", err)
	}
}
