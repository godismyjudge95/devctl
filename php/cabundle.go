package php

import (
	"bytes"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/paths"
)

// systemCABundleCandidates is the OS CA bundle search list. Tests may replace it.
var systemCABundleCandidates = []string{
	"/etc/ssl/certs/ca-certificates.crt",                // Debian/Ubuntu
	"/etc/pki/tls/certs/ca-bundle.crt",                  // RHEL/Fedora
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // RHEL extract
	"/etc/ssl/ca-bundle.pem",                            // OpenSUSE
	"/etc/ssl/cert.pem",                                 // macOS / generic OpenSSL
}

// extraLocalCAFiles are copies of Caddy's local CA installed by
// `sudo devctl elevate trust`. Included so PHP trusts *.test even when the
// live Caddy Admin API is down, as long as elevate trust already ran.
var extraLocalCAFiles = []string{
	"/usr/local/share/ca-certificates/devctl-local-ca.crt",
	"/etc/pki/ca-trust/source/anchors/devctl-local-ca.crt",
	"/Library/Application Support/devctl/devctl-local-ca.crt",
}

// CertEnv returns SSL_CERT_FILE and CURL_CA_BUNDLE for PHP-FPM so OpenSSL
// and libcurl trust Caddy's local CA even when php.ini is not consulted.
func CertEnv(serverRoot string) []string {
	p := paths.CABundlePath(serverRoot)
	return []string{
		"SSL_CERT_FILE=" + p,
		"CURL_CA_BUNDLE=" + p,
		"AWS_CA_BUNDLE=" + p,
	}
}

// ApplyCABundle writes the combined CA bundle and points every installed
// php.ini at it. extraPEM is typically Caddy's live root certificate.
func ApplyCABundle(serverRoot string, extraPEM []byte) error {
	bundlePath, err := WriteCABundle(serverRoot, extraPEM)
	if err != nil {
		return err
	}
	versions, err := InstalledVersions(serverRoot)
	if err != nil {
		return fmt.Errorf("list php versions: %w", err)
	}
	for _, v := range versions {
		if err := migrateCAFile(fpmIniPath(v.Version, serverRoot), bundlePath); err != nil {
			return fmt.Errorf("php.ini %s: %w", v.Version, err)
		}
	}
	return nil
}

// WriteCABundle writes {serverRoot}/php/ca-bundle.crt: the OS trust bundle
// plus extraPEM plus Caddy's on-disk local CA (if present). Duplicate
// certificates are skipped.
func WriteCABundle(serverRoot string, extraPEM []byte) (string, error) {
	bundlePath := paths.CABundlePath(serverRoot)
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0755); err != nil {
		return "", fmt.Errorf("create php dir: %w", err)
	}

	var chunks [][]byte
	if sys := readFirstFile(systemCABundleCandidates); len(sys) > 0 {
		chunks = append(chunks, sys)
	}
	if len(bytes.TrimSpace(extraPEM)) > 0 {
		chunks = append(chunks, extraPEM)
	}
	for _, p := range caddyCACertPaths(serverRoot) {
		if data, err := os.ReadFile(p); err == nil && len(bytes.TrimSpace(data)) > 0 {
			chunks = append(chunks, data)
		}
	}
	if local := readFirstFile(extraLocalCAFiles); len(local) > 0 {
		chunks = append(chunks, local)
	}

	combined := mergePEMCerts(chunks...)
	if len(bytes.TrimSpace(combined)) == 0 {
		return "", fmt.Errorf("no CA certificates found")
	}
	if !bytes.HasSuffix(combined, []byte("\n")) {
		combined = append(combined, '\n')
	}
	if err := os.WriteFile(bundlePath, combined, 0644); err != nil {
		return "", fmt.Errorf("write ca bundle: %w", err)
	}
	return bundlePath, nil
}

func caddyCACertPaths(serverRoot string) []string {
	caddyDir := paths.ServiceDir(serverRoot, "caddy")
	dirs := []string{
		filepath.Join(caddyDir, ".local", "share", "caddy", "pki", "authorities", "local"),
		filepath.Join(caddyDir, "Library", "Application Support", "Caddy", "pki", "authorities", "local"),
		filepath.Join(caddyDir, "pki", "authorities", "local"),
	}
	var out []string
	for _, dir := range dirs {
		out = append(out, filepath.Join(dir, "root.crt"), filepath.Join(dir, "intermediate.crt"))
	}
	return out
}

func readFirstFile(candidates []string) []byte {
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil && len(bytes.TrimSpace(data)) > 0 {
			return data
		}
	}
	return nil
}

func mergePEMCerts(chunks ...[]byte) []byte {
	seen := map[string]struct{}{}
	var out bytes.Buffer
	for _, chunk := range chunks {
		rest := chunk
		for {
			var block *pem.Block
			block, rest = pem.Decode(rest)
			if block == nil {
				break
			}
			if block.Type != "CERTIFICATE" {
				continue
			}
			key := string(block.Bytes)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if err := pem.Encode(&out, block); err != nil {
				continue
			}
		}
	}
	return out.Bytes()
}

// migrateCAFile sets openssl.cafile and curl.cainfo on an existing php.ini.
// No-op when the file does not exist.
func migrateCAFile(iniPath, bundlePath string) error {
	if bundlePath == "" {
		return nil
	}
	data, err := os.ReadFile(iniPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(data)
	updated, changed := setIniKey(content, "openssl.cafile", bundlePath)
	updated2, changed2 := setIniKey(updated, "curl.cainfo", bundlePath)
	if !changed && !changed2 {
		return nil
	}
	return os.WriteFile(iniPath, []byte(updated2), 0644)
}

// setIniKey updates every uncommented assignment of key, or appends the key
// when it is absent. Commented template lines are left as-is.
func setIniKey(content, key, value string) (string, bool) {
	line := key + " = " + value
	lines := strings.Split(content, "\n")
	found := false
	changed := false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) != key {
			continue
		}
		found = true
		if strings.TrimSpace(parts[1]) == value {
			continue
		}
		lines[i] = line
		changed = true
	}
	if found {
		return strings.Join(lines, "\n"), changed
	}
	out := content
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out + line + "\n", true
}
