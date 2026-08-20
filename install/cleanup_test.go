package install

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danielgormly/devctl/paths"
)

func TestCleanupRemovedWhoDB_RemovesFiles(t *testing.T) {
	root := t.TempDir()
	dir := paths.ServiceDir(root, "whodb")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whodb"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	binDir := paths.BinDir(root)
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "whodb"), filepath.Join(binDir, "whodb")); err != nil {
		t.Fatal(err)
	}
	logPath := paths.LogPath(root, "whodb")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("log"), 0644); err != nil {
		t.Fatal(err)
	}

	CleanupRemovedWhoDB(context.Background(), nil, root)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("whodb dir still present: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(binDir, "whodb")); !os.IsNotExist(err) {
		t.Errorf("bin/whodb still present: %v", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("whodb log still present: %v", err)
	}
}

func TestCleanupRemovedWhoDB_NoopWhenAbsent(t *testing.T) {
	CleanupRemovedWhoDB(context.Background(), nil, t.TempDir())
}
