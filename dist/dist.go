// Package dist holds unprivileged download tokens and Caddy listen ports
// keyed by GOOS and GOARCH. Privileged OS mutations stay in elevate.
package dist

import (
	"errors"
	"fmt"
	"runtime"
)

// ErrUnsupported means this GOOS/GOARCH has no vendor binary for name.
var ErrUnsupported = errors.New("dist: unsupported on this platform")

// Asset is one vendor filename dialect for a downloadable binary.
type Asset struct {
	Token string
	File  string
}

type assetRow struct {
	GOOS   string
	GOARCH string
	Name   string
	Asset  Asset
}

// linux/amd64 tokens match the URL fragments already hardcoded in installers
// so Incus tests and the artifact cache keep working.
var assetTable = []assetRow{
	{GOOS: "linux", GOARCH: "amd64", Name: "caddy", Asset: Asset{Token: "linux_amd64", File: "caddy-linux-amd64.tar.gz"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "caddy", Asset: Asset{Token: "linux_arm64", File: "caddy-linux-arm64.tar.gz"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "caddy", Asset: Asset{Token: "mac_arm64", File: "caddy-mac-arm64.tar.gz"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "mailpit", Asset: Asset{Token: "linux-amd64", File: "mailpit-linux-amd64.tar.gz"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "mailpit", Asset: Asset{Token: "linux-arm64", File: "mailpit-linux-arm64.tar.gz"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "mailpit", Asset: Asset{Token: "darwin-arm64", File: "mailpit-darwin-arm64.tar.gz"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "meilisearch", Asset: Asset{Token: "linux-amd64", File: "meilisearch-linux-amd64"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "meilisearch", Asset: Asset{Token: "linux-aarch64", File: "meilisearch-linux-aarch64"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "meilisearch", Asset: Asset{Token: "macos-apple-silicon", File: "meilisearch-macos-apple-silicon"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "typesense", Asset: Asset{Token: "linux-amd64", File: "typesense-linux-amd64.tar.gz"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "typesense", Asset: Asset{Token: "linux-arm64", File: "typesense-linux-arm64.tar.gz"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "typesense", Asset: Asset{Token: "darwin-arm64", File: "typesense-darwin-arm64.tar.gz"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "maxio", Asset: Asset{Token: "linux-amd64", File: "maxio-linux-amd64.tar.gz"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "maxio", Asset: Asset{Token: "linux-arm64", File: "maxio-linux-arm64.tar.gz"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "maxio", Asset: Asset{Token: "macos-arm64", File: "maxio-macos-arm64.tar.gz"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "clickhouse", Asset: Asset{Token: "amd64", File: "clickhouse-common-static-amd64.tgz"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "clickhouse", Asset: Asset{Token: "arm64", File: "clickhouse-common-static-arm64.tgz"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "clickhouse", Asset: Asset{Token: "macos-aarch64", File: "clickhouse-macos-aarch64"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "php", Asset: Asset{Token: "linux-x86_64", File: "php-linux-x86_64"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "php", Asset: Asset{Token: "macos-aarch64", File: "php-macos-aarch64"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "yq", Asset: Asset{Token: "linux_amd64", File: "yq_linux_amd64"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "yq", Asset: Asset{Token: "linux_arm64", File: "yq_linux_arm64"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "yq", Asset: Asset{Token: "darwin_arm64", File: "yq_darwin_arm64"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "mago", Asset: Asset{Token: "x86_64-unknown-linux-gnu", File: "mago-linux-gnu.tar.gz"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "mago", Asset: Asset{Token: "aarch64-unknown-linux-gnu", File: "mago-linux-gnu.tar.gz"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "mago", Asset: Asset{Token: "aarch64-apple-darwin", File: "mago-apple-darwin.tar.gz"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "fnm", Asset: Asset{Token: "linux", File: "fnm-linux.zip"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "fnm", Asset: Asset{Token: "linux", File: "fnm-linux.zip"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "fnm", Asset: Asset{Token: "macos", File: "fnm-macos.zip"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "sqlite3", Asset: Asset{Token: "linux-x64", File: "sqlite-tools-linux-x64.zip"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "sqlite3", Asset: Asset{Token: "linux-x64", File: "sqlite-tools-linux-x64.zip"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "sqlite3", Asset: Asset{Token: "osx-arm64", File: "sqlite-tools-osx-arm64.zip"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "phpantom_lsp", Asset: Asset{Token: "x86_64-unknown-linux-gnu", File: "phpantom_lsp-linux-gnu.tar.gz"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "phpantom_lsp", Asset: Asset{Token: "aarch64-unknown-linux-gnu", File: "phpantom_lsp-linux-gnu.tar.gz"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "phpantom_lsp", Asset: Asset{Token: "aarch64-apple-darwin", File: "phpantom_lsp-apple-darwin.tar.gz"}},

	{GOOS: "linux", GOARCH: "amd64", Name: "valkey", Asset: Asset{Token: "jammy-x86_64", File: "valkey-linux-x86_64.tar.gz"}},
	{GOOS: "linux", GOARCH: "arm64", Name: "valkey", Asset: Asset{Token: "jammy-aarch64", File: "valkey-linux-aarch64.tar.gz"}},

	{GOOS: "darwin", GOARCH: "arm64", Name: "mysql", Asset: Asset{Token: "macos15-arm64", File: "mysql-macos15-arm64.tar.gz"}},
	{GOOS: "darwin", GOARCH: "arm64", Name: "postgres", Asset: Asset{Token: "osx-binaries", File: "postgresql-osx-binaries.zip"}},
}

// Lookup returns the asset for goos/goarch/name.
// Tests on any OS can assert Darwin rows without faking GOOS.
func Lookup(goos, goarch, name string) (Asset, error) {
	for _, row := range assetTable {
		if row.GOOS == goos && row.GOARCH == goarch && row.Name == name {
			return row.Asset, nil
		}
	}
	return Asset{}, fmt.Errorf("%w: %s/%s/%s", ErrUnsupported, goos, goarch, name)
}

// For returns the asset for the running GOOS/GOARCH.
func For(name string) (Asset, error) {
	return Lookup(runtime.GOOS, runtime.GOARCH, name)
}

// ListenHTTP is the Caddy HTTP server listen list for the running OS.
func ListenHTTP() []string {
	return ListenHTTPFor(runtime.GOOS)
}

// ListenHTTPFor is the Caddy HTTP server listen list for goos.
// Linux binds :80 and :443 (ambient cap). Darwin binds :8080 and :8443 (pf rdr).
func ListenHTTPFor(goos string) []string {
	if goos == "darwin" {
		return []string{":8080", ":8443"}
	}
	return []string{":80", ":443"}
}

// catalogName maps a service ID to a dist table name.
// IDs with no row on any OS are omitted so Allows stays true (apt or in-process).
func catalogName(id string) (string, bool) {
	switch id {
	case "redis":
		return "valkey", true
	case "caddy", "mailpit", "meilisearch", "typesense", "maxio", "clickhouse", "yq", "php":
		return id, true
	default:
		return "", false
	}
}

// Allows reports whether this GOOS/GOARCH has a vendor binary for the service.
// Services that are not in the download table (dns, reverb, mysql, postgres)
// stay allowed so Linux apt installers keep working.
func Allows(id string) bool {
	return AllowsFor(runtime.GOOS, runtime.GOARCH, id)
}

// AllowsFor is Allows for an explicit GOOS/GOARCH.
func AllowsFor(goos, goarch, id string) bool {
	name, ok := catalogName(id)
	if !ok {
		return true
	}
	_, err := Lookup(goos, goarch, name)
	return err == nil
}
