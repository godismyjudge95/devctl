package install

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFromZipStrip_Symlink(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pg.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	hdr := &zip.FileHeader{Name: "pgsql/lib/libicudata.77.dylib"}
	hdr.SetMode(os.ModeSymlink | 0755)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "libicudata.77.1.dylib"); err != nil {
		t.Fatal(err)
	}
	reg, err := zw.Create("pgsql/lib/libicudata.77.1.dylib")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(reg, "mach-o-placeholder"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zf.Close()

	dest := filepath.Join(dir, "out")
	if err := extractFromZipStrip(zipPath, dest, "pgsql"); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dest, "lib", "libicudata.77.dylib")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("want symlink, mode=%v", info.Mode())
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if target != "libicudata.77.1.dylib" {
		t.Fatalf("target = %q", target)
	}
}
