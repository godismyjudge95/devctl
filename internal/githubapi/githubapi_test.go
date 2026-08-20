package githubapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestTag_ParsesTagName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/repos/acme/tool/releases/latest") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v9.8.7"}`)
	}))
	defer srv.Close()

	orig := http.DefaultTransport
	http.DefaultTransport = rewire(srv.URL)
	defer func() { http.DefaultTransport = orig }()

	got, err := LatestTag(context.Background(), "acme/tool")
	if err != nil {
		t.Fatalf("LatestTag: %v", err)
	}
	if got != "v9.8.7" {
		t.Fatalf("LatestTag = %q, want v9.8.7", got)
	}
}

func TestLatestTag_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	orig := http.DefaultTransport
	http.DefaultTransport = rewire(srv.URL)
	defer func() { http.DefaultTransport = orig }()

	_, err := LatestTag(context.Background(), "acme/tool")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("error = %v, want HTTP 403", err)
	}
}

func TestLatestTag_EmptyTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":""}`)
	}))
	defer srv.Close()

	orig := http.DefaultTransport
	http.DefaultTransport = rewire(srv.URL)
	defer func() { http.DefaultTransport = orig }()

	_, err := LatestTag(context.Background(), "acme/tool")
	if err == nil {
		t.Fatal("expected error")
	}
}

type rewriter struct {
	base  string
	inner http.RoundTripper
}

func rewire(baseURL string) http.RoundTripper {
	return &rewriter{base: baseURL, inner: http.DefaultTransport}
}

func (h *rewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	host := strings.TrimPrefix(h.base, "http://")
	host = strings.TrimPrefix(host, "https://")
	req2.URL.Host = host
	return h.inner.RoundTrip(req2)
}
