package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

type tarEntry struct {
	name, body, link string
	dir              bool
}

func writeTarGz(t *testing.T, entries []tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0644}
		switch {
		case e.dir:
			hdr.Typeflag, hdr.Mode = tar.TypeDir, 0755
		case e.link != "":
			hdr.Typeflag, hdr.Linkname = tar.TypeSymlink, e.link
		default:
			hdr.Typeflag, hdr.Size = tar.TypeReg, int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractPgExtensionsBundle_PlacesFilesAndReplacesByRename(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "lib", "postgresql")
	extDir := filepath.Join(root, "share", "postgresql", "extension")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(libDir, "vector.so")
	if err := os.WriteFile(old, []byte("vendor"), 0755); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(old)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	archive := writeTarGz(t, []tarEntry{
		{name: "lib/", dir: true},
		{name: "lib/vector.so", body: "portable"},
		{name: "lib/bitcode/vector/src/hnsw.bc", body: "bc"},
		{name: "lib/timescaledb-link.so", link: "timescaledb.so"},
		{name: "extension/vector.control", body: "default_version = '0.8.2'\n"},
		{name: "README", body: "ignored"},
	})
	if err := extractPgExtensionsBundle(archive, libDir, extDir); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]string{
		filepath.Join(libDir, "vector.so"):                  "portable",
		filepath.Join(libDir, "bitcode/vector/src/hnsw.bc"): "bc",
		filepath.Join(extDir, "vector.control"):             "default_version = '0.8.2'\n",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
	if link, err := os.Readlink(filepath.Join(libDir, "timescaledb-link.so")); err != nil || link != "timescaledb.so" {
		t.Fatalf("symlink = %q, %v", link, err)
	}
	if _, err := os.Stat(filepath.Join(root, "README")); !os.IsNotExist(err) {
		t.Fatal("entries outside lib/ and extension/ must be skipped")
	}
	// A running postgres keeps the library it mapped: the old inode survives.
	stale := make([]byte, 16)
	n, _ := held.ReadAt(stale, 0)
	if string(stale[:n]) != "vendor" {
		t.Fatalf("open handle sees %q; the file was overwritten in place", stale[:n])
	}
}

func TestExtractPgExtensionsBundle_RejectsEscapingPaths(t *testing.T) {
	root := t.TempDir()
	archive := writeTarGz(t, []tarEntry{{name: "lib/../../evil.so", body: "x"}})
	if err := extractPgExtensionsBundle(archive, filepath.Join(root, "lib"), filepath.Join(root, "ext")); err == nil {
		t.Fatal("expected an error for a path that leaves lib/")
	}
}

func TestPgExtensionsAssetName_NamesPostgresVersionAndPlatform(t *testing.T) {
	want := "pg-extensions-pg" + postgresVersion + "-" + pgExtensionsPlatform() + ".tar.gz"
	if got := pgExtensionsAssetName(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := pgExtensionsURL(); got != "https://github.com/godismyjudge95/devctl/releases/download/"+pgExtensionsReleaseTag+"/"+want {
		t.Fatalf("url %q", got)
	}
}

// The workflow builds the versions devctl expects. Drift would ship a bundle
// whose file names isTimescaleInstalled does not find.
func TestPgExtensionsWorkflowVersionsMatch(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/build-pg-extensions.yml")
	if err != nil {
		t.Skipf("workflow not present: %v", err)
	}
	for key, want := range map[string]string{
		"PG_VERSION":            postgresVersion,
		"TIMESCALEDB_VERSION":   timescaledbVersion,
		"PGVECTOR_VERSION":      pgvectorVersion,
		"PG_CLICKHOUSE_VERSION": pgClickhouseVersion,
	} {
		m := regexp.MustCompile(`(?m)^\s+` + key + `:\s*"([^"]+)"`).FindSubmatch(data)
		if m == nil {
			t.Fatalf("%s missing from workflow env", key)
		}
		if string(m[1]) != want {
			t.Fatalf("workflow %s = %s, Go constant = %s", key, m[1], want)
		}
	}
}
