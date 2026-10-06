package php

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgormly/devctl/dist"
)

func TestLatestStaticPHPNames(t *testing.T) {
	html := `
<a href="php-8.4.8-cli-macos-aarch64.tar.gz">php-8.4.8-cli-macos-aarch64.tar.gz</a>
<a href="php-8.4.8-fpm-macos-aarch64.tar.gz">php-8.4.8-fpm-macos-aarch64.tar.gz</a>
<a href="php-8.4.14-cli-macos-aarch64.tar.gz">php-8.4.14-cli-macos-aarch64.tar.gz</a>
<a href="php-8.4.14-fpm-macos-aarch64.tar.gz">php-8.4.14-fpm-macos-aarch64.tar.gz</a>
<a href="php-8.3.1-cli-macos-aarch64.tar.gz">php-8.3.1-cli-macos-aarch64.tar.gz</a>
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(html))
	}))
	defer srv.Close()

	orig := staticPHPIndexURL
	staticPHPIndexURL = srv.URL + "/"
	defer func() { staticPHPIndexURL = orig }()

	cli, fpm, ver, err := latestStaticPHPNames(context.Background(), "8.4", "macos-aarch64")
	if err != nil {
		t.Fatal(err)
	}
	if ver != "8.4.14" {
		t.Fatalf("ver = %q, want 8.4.14", ver)
	}
	if cli != "php-8.4.14-cli-macos-aarch64.tar.gz" {
		t.Fatalf("cli = %q", cli)
	}
	if fpm != "php-8.4.14-fpm-macos-aarch64.tar.gz" {
		t.Fatalf("fpm = %q", fpm)
	}
}

func TestDarwinPHPToken(t *testing.T) {
	a, err := dist.Lookup("darwin", "arm64", "php")
	if err != nil {
		t.Fatal(err)
	}
	if a.Token != "macos-aarch64" {
		t.Fatalf("token = %q", a.Token)
	}
}
