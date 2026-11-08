package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSitesShare_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:share", wantArgs: true})
}
func TestSitesUnshare_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:unshare", wantArgs: true})
}
func TestSitesShares_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "sites:shares"}) }

// shareAPI fakes the daemon routes the share commands call and records the
// share requests it saw.
func shareAPI(t *testing.T, shareBody string) (*Client, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/sites":
			w.Write([]byte(`[{"id":"shop-test","domain":"shop.test","root_path":"/srv/shop"}]`))
		case "POST /api/sites/shop-test/share":
			calls = append(calls, "share")
			w.Write([]byte(shareBody))
		case "DELETE /api/sites/shop-test/share":
			calls = append(calls, "unshare")
			w.WriteHeader(http.StatusNoContent)
		case "GET /api/shares":
			w.Write([]byte("[" + shareBody + "]"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{base: srv.URL, http: srv.Client()}, &calls
}

func TestSitesShare_ResolvesDomainAndShares(t *testing.T) {
	c, calls := shareAPI(t, `{"site_id":"shop-test","domain":"shop.test","status":"active","url":"https://a-b.trycloudflare.com"}`)
	if err := Find("sites:share").Handler(c, []string{"shop.test"}, true); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, ",") != "share" {
		t.Fatalf("calls = %v", *calls)
	}
}

func TestSitesShare_FailedTunnelIsAnError(t *testing.T) {
	c, _ := shareAPI(t, `{"site_id":"shop-test","domain":"shop.test","status":"failed","error":"lookup api.trycloudflare.com: no such host"}`)
	err := Find("sites:share").Handler(c, []string{"shop.test"}, true)
	if err == nil || !strings.Contains(err.Error(), "no such host") {
		t.Fatalf("err = %v, want the tunnel error", err)
	}
}

func TestSitesShare_UnknownDomain(t *testing.T) {
	c, calls := shareAPI(t, `{}`)
	if err := Find("sites:share").Handler(c, []string{"nope.test"}, true); err == nil {
		t.Fatal("expected an error for an unknown domain")
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %v, want none", *calls)
	}
}

func TestSitesUnshare_SendsDelete(t *testing.T) {
	c, calls := shareAPI(t, `{}`)
	if err := Find("sites:unshare").Handler(c, []string{"shop-test"}, true); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, ",") != "unshare" {
		t.Fatalf("calls = %v", *calls)
	}
}

func TestListShares_Decodes(t *testing.T) {
	c, _ := shareAPI(t, `{"site_id":"shop-test","domain":"shop.test","status":"active","url":"https://a-b.trycloudflare.com"}`)
	shares, err := c.ListShares()
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 1 || shares[0].URL != "https://a-b.trycloudflare.com" || shares[0].Status != "active" {
		t.Fatalf("shares = %+v", shares)
	}
}
