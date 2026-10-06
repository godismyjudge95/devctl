package install

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/dist"
)

func TestValkeyHerdZipURL(t *testing.T) {
	a, err := dist.Lookup("darwin", "arm64", "valkey")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://download.herdphp.com/services/valkey/" + a.File
	if want != "https://download.herdphp.com/services/valkey/8.1.10-universal.zip" {
		t.Errorf("herd zip URL = %q", want)
	}
}

func TestValkeyDownloadURL_CurrentOS(t *testing.T) {
	got, err := valkeyDownloadURL(context.Background(), "8.1.10")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		if got != "https://download.herdphp.com/services/valkey/8.1.10-universal.zip" {
			t.Errorf("darwin URL = %q", got)
		}
		return
	}
	if !strings.Contains(got, "download.valkey.io/releases/valkey-8.1.10-") {
		t.Errorf("linux URL = %q", got)
	}
	if !strings.HasSuffix(got, ".tar.gz") {
		t.Errorf("linux URL should be tarball: %q", got)
	}
}

func TestExtractValkeyZip_FlattensBinAndLibs(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "valkey.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for name, body := range map[string]string{
		"bin/valkey-server": "server-bin",
		"bin/valkey-cli":    "cli-bin",
		"libssl.3.dylib":    "ssl",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zf.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "out")
	if err := extractValkeyZip(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"valkey-server", "valkey-cli", "libssl.3.dylib"} {
		p := filepath.Join(dest, name)
		if !fileExists(p) {
			t.Errorf("missing %s", p)
		}
	}
}
