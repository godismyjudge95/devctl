package php

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielgormly/devctl/paths"
)

func testCACertPEM(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestWriteCABundle_CombinesSystemAndExtra(t *testing.T) {
	serverRoot := t.TempDir()
	sysPEM := testCACertPEM(t, "system-ca")
	caddyPEM := testCACertPEM(t, "caddy-local")

	sysFile := filepath.Join(t.TempDir(), "system.crt")
	if err := os.WriteFile(sysFile, sysPEM, 0644); err != nil {
		t.Fatal(err)
	}
	prev := systemCABundleCandidates
	prevExtra := extraLocalCAFiles
	systemCABundleCandidates = []string{sysFile}
	extraLocalCAFiles = nil
	t.Cleanup(func() {
		systemCABundleCandidates = prev
		extraLocalCAFiles = prevExtra
	})

	got, err := WriteCABundle(serverRoot, caddyPEM)
	if err != nil {
		t.Fatalf("WriteCABundle: %v", err)
	}
	if got != paths.CABundlePath(serverRoot) {
		t.Fatalf("path = %q, want %q", got, paths.CABundlePath(serverRoot))
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "BEGIN CERTIFICATE") {
		t.Fatal("bundle missing PEM")
	}
	certs := mergePEMCerts(data)
	sysCount := strings.Count(string(mergePEMCerts(sysPEM, caddyPEM)), "BEGIN CERTIFICATE")
	gotCount := strings.Count(string(certs), "BEGIN CERTIFICATE")
	if gotCount != sysCount {
		t.Fatalf("cert count = %d, want %d", gotCount, sysCount)
	}
}

func TestWriteCABundle_DedupsRepeatedCert(t *testing.T) {
	serverRoot := t.TempDir()
	cert := testCACertPEM(t, "same-ca")
	sysFile := filepath.Join(t.TempDir(), "system.crt")
	if err := os.WriteFile(sysFile, cert, 0644); err != nil {
		t.Fatal(err)
	}
	prev := systemCABundleCandidates
	prevExtra := extraLocalCAFiles
	systemCABundleCandidates = []string{sysFile}
	extraLocalCAFiles = nil
	t.Cleanup(func() {
		systemCABundleCandidates = prev
		extraLocalCAFiles = prevExtra
	})

	path, err := WriteCABundle(serverRoot, cert)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "BEGIN CERTIFICATE"); n != 1 {
		t.Fatalf("expected 1 cert after dedup, got %d", n)
	}
}

func TestWriteCABundle_UsesOnDiskCaddyRoot(t *testing.T) {
	serverRoot := t.TempDir()
	caddyPEM := testCACertPEM(t, "caddy-disk")
	rootPath := filepath.Join(serverRoot, "caddy", ".local", "share", "caddy", "pki", "authorities", "local", "root.crt")
	if err := os.MkdirAll(filepath.Dir(rootPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootPath, caddyPEM, 0644); err != nil {
		t.Fatal(err)
	}

	prev := systemCABundleCandidates
	prevExtra := extraLocalCAFiles
	systemCABundleCandidates = nil
	extraLocalCAFiles = nil
	t.Cleanup(func() {
		systemCABundleCandidates = prev
		extraLocalCAFiles = prevExtra
	})

	path, err := WriteCABundle(serverRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "BEGIN CERTIFICATE"); n != 1 {
		t.Fatalf("want disk caddy root in bundle, got %d:\n%s", n, data)
	}
	interPEM := testCACertPEM(t, "caddy-intermediate")
	if err := os.WriteFile(filepath.Join(filepath.Dir(rootPath), "intermediate.crt"), interPEM, 0644); err != nil {
		t.Fatal(err)
	}
	path, err = WriteCABundle(serverRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "BEGIN CERTIFICATE"); n != 2 {
		t.Fatalf("want root+intermediate in bundle, got %d:\n%s", n, data)
	}
}

func TestMigrateCAFile_AppendsMissingKeys(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "php.ini")
	if err := os.WriteFile(ini, []byte(";openssl.cafile=\nmemory_limit = 256M\n"), 0644); err != nil {
		t.Fatal(err)
	}
	bundle := "/tmp/ca-bundle.crt"
	if err := migrateCAFile(ini, bundle); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(ini)
	s := string(data)
	if !strings.Contains(s, "openssl.cafile = "+bundle) {
		t.Fatalf("missing openssl.cafile:\n%s", s)
	}
	if !strings.Contains(s, "curl.cainfo = "+bundle) {
		t.Fatalf("missing curl.cainfo:\n%s", s)
	}
	if !strings.Contains(s, ";openssl.cafile=") {
		t.Fatal("commented template line was rewritten")
	}
}

func TestMigrateCAFile_UpdatesExistingValue(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "php.ini")
	if err := os.WriteFile(ini, []byte("openssl.cafile = /old.pem\ncurl.cainfo = /old.pem\n"), 0644); err != nil {
		t.Fatal(err)
	}
	bundle := "/new/ca-bundle.crt"
	if err := migrateCAFile(ini, bundle); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(ini)
	s := string(data)
	if strings.Contains(s, "/old.pem") {
		t.Fatalf("old path remains:\n%s", s)
	}
	if !strings.Contains(s, "openssl.cafile = "+bundle) || !strings.Contains(s, "curl.cainfo = "+bundle) {
		t.Fatalf("new path missing:\n%s", s)
	}
}

func TestMigrateCAFile_NoRewriteWhenAlreadySet(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "php.ini")
	bundle := "/stable/ca-bundle.crt"
	orig := "openssl.cafile = " + bundle + "\ncurl.cainfo = " + bundle + "\n"
	if err := os.WriteFile(ini, []byte(orig), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ini)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := migrateCAFile(ini, bundle); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(ini)
	if err != nil {
		t.Fatal(err)
	}
	if !info2.ModTime().Equal(info.ModTime()) {
		t.Fatal("php.ini rewritten when openssl.cafile was already correct")
	}
}

func TestApplyCABundle_PatchesInstalledPHPIni(t *testing.T) {
	serverRoot := setupFakeServerRoot(t, "8.4")
	ini := PHPIniPath("8.4", serverRoot)
	if err := os.WriteFile(ini, []byte("memory_limit = 64M\n"), 0644); err != nil {
		t.Fatal(err)
	}
	extra := testCACertPEM(t, "live-caddy")
	prev := systemCABundleCandidates
	prevExtra := extraLocalCAFiles
	systemCABundleCandidates = nil
	extraLocalCAFiles = nil
	t.Cleanup(func() {
		systemCABundleCandidates = prev
		extraLocalCAFiles = prevExtra
	})
	if err := ApplyCABundle(serverRoot, extra); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ini)
	if err != nil {
		t.Fatal(err)
	}
	bundle := paths.CABundlePath(serverRoot)
	if !strings.Contains(string(data), "openssl.cafile = "+bundle) {
		t.Fatalf("ApplyCABundle did not patch php.ini:\n%s", data)
	}
	bundleData, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(bundleData), "BEGIN CERTIFICATE") < 1 {
		t.Fatal("bundle empty")
	}
}

func TestCertEnv(t *testing.T) {
	env := CertEnv("/srv")
	want := paths.CABundlePath("/srv")
	if len(env) != 3 {
		t.Fatalf("len=%d %v", len(env), env)
	}
	if env[0] != "SSL_CERT_FILE="+want || env[1] != "CURL_CA_BUNDLE="+want || env[2] != "AWS_CA_BUNDLE="+want {
		t.Fatalf("env=%v", env)
	}
}

func TestSetIniKey_AppendsAndIdempotent(t *testing.T) {
	out, changed := setIniKey("memory_limit = 128M\n", "openssl.cafile", "/ca.pem")
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(out, "openssl.cafile = /ca.pem\n") {
		t.Fatalf("got %q", out)
	}
	out2, changed2 := setIniKey(out, "openssl.cafile", "/ca.pem")
	if changed2 {
		t.Fatal("second set should be a no-op")
	}
	if out2 != out {
		t.Fatalf("content changed on no-op:\n%s\nvs\n%s", out2, out)
	}
}
