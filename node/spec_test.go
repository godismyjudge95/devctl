package node

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFind_Files(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		want    string
		wantSrc string
	}{
		{"nvmrc", map[string]string{".nvmrc": "20\n"}, "20", ".nvmrc"},
		{"nvmrc with v and spaces", map[string]string{".nvmrc": "  v18.17.1  \r\n"}, "v18.17.1", ".nvmrc"},
		{"nvmrc lts alias", map[string]string{".nvmrc": "lts/*\n"}, "lts/*", ".nvmrc"},
		{"nvmrc comment first", map[string]string{".nvmrc": "# pinned for vite\n\nlts/iron\n"}, "lts/iron", ".nvmrc"},
		{"node-version", map[string]string{".node-version": "22.11.0\n"}, "22.11.0", ".node-version"},
		{"engines range", map[string]string{"package.json": `{"name":"app","engines":{"node":">=20"}}`}, ">=20", "package.json"},
		{"engines caret", map[string]string{"package.json": `{"engines":{"node":"^18.17"}}`}, "^18.17", "package.json"},
		{"nvmrc wins over node-version and engines", map[string]string{
			".nvmrc": "20", ".node-version": "18", "package.json": `{"engines":{"node":"16"}}`,
		}, "20", ".nvmrc"},
		{"node-version wins over engines", map[string]string{
			".node-version": "18", "package.json": `{"engines":{"node":"16"}}`,
		}, "18", ".node-version"},
		{"empty nvmrc falls through", map[string]string{".nvmrc": "\n", "package.json": `{"engines":{"node":"22"}}`}, "22", "package.json"},
		{"package.json without engines", map[string]string{"package.json": `{"name":"app"}`}, "", ""},
		{"malformed package.json", map[string]string{"package.json": `{"engines":`}, "", ""},
		{"no files", nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				writeFile(t, filepath.Join(dir, name), content)
			}
			got := Find(dir)
			if got.Version != tt.want {
				t.Errorf("Version = %q, want %q", got.Version, tt.want)
			}
			wantFile := ""
			if tt.wantSrc != "" {
				wantFile = filepath.Join(dir, tt.wantSrc)
			}
			if got.File != wantFile {
				t.Errorf("File = %q, want %q", got.File, wantFile)
			}
		})
	}
}

func TestFind_WalksUpFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".nvmrc"), "20\n")
	writeFile(t, filepath.Join(root, "packages", "ui", "package.json"), `{"name":"ui"}`)

	got := Find(filepath.Join(root, "packages", "ui"))
	if got.Version != "20" || got.File != filepath.Join(root, ".nvmrc") {
		t.Fatalf("Find(subdir) = %+v, want 20 from the root .nvmrc", got)
	}
}

func TestFind_NearestDirectoryWins(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".nvmrc"), "18\n")
	writeFile(t, filepath.Join(root, "app", "package.json"), `{"engines":{"node":"22"}}`)

	if got := Find(filepath.Join(root, "app")); got.Version != "22" {
		t.Fatalf("Find(app) = %+v, want 22 from app/package.json", got)
	}
}

func TestWriteNvmrc(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"engines":{"node":"18"}}`)

	if err := WriteNvmrc(dir, " 22 "); err != nil {
		t.Fatalf("WriteNvmrc: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".nvmrc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "22\n" {
		t.Fatalf(".nvmrc = %q, want %q", b, "22\n")
	}
	if got := Find(dir); got.Version != "22" {
		t.Fatalf("Find after write = %+v, want 22", got)
	}
}

func TestWriteNvmrc_RejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"", "system", "20\n18", "abc"} {
		if err := WriteNvmrc(dir, v); err == nil {
			t.Errorf("WriteNvmrc(%q) succeeded, want error", v)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".nvmrc")); !os.IsNotExist(err) {
		t.Fatalf(".nvmrc exists after rejected writes: %v", err)
	}
}
