package install

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFromTar_PrefersLargestMatch(t *testing.T) {
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "test.tgz")
	dest := filepath.Join(dir, "out")

	// Archive with a small "clickhouse" script first, then a larger binary.
	if err := writeTestTGZ(tarPath, []tarMember{
		{name: "pkg/share/bash-completion/completions/clickhouse", body: []byte("#!/bin/bash\n# completion\n")},
		{name: "pkg/usr/bin/clickhouse", body: []byte("ELF-FAKE-BINARY-CONTENT-XXXX")},
	}); err != nil {
		t.Fatal(err)
	}

	if err := extractFromTar(tarPath, "clickhouse", dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ELF-FAKE-BINARY-CONTENT-XXXX" {
		t.Fatalf("extracted wrong member: %q", got)
	}
}

func TestFindBestTarMember_PrefersBinOnSizeTie(t *testing.T) {
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "test.tgz")
	body := []byte("same-size-content")
	if err := writeTestTGZ(tarPath, []tarMember{
		{name: "pkg/other/clickhouse", body: body},
		{name: "pkg/usr/bin/clickhouse", body: body},
	}); err != nil {
		t.Fatal(err)
	}
	name, err := findBestTarMember(tarPath, "clickhouse")
	if err != nil {
		t.Fatal(err)
	}
	if name != "pkg/usr/bin/clickhouse" {
		t.Fatalf("want bin path, got %q", name)
	}
}

type tarMember struct {
	name string
	body []byte
}

func writeTestTGZ(path string, members []tarMember) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	for _, m := range members {
		hdr := &tar.Header{
			Name: m.name,
			Mode: 0755,
			Size: int64(len(m.body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(m.body); err != nil {
			return err
		}
	}
	return nil
}
