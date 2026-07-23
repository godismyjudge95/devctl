package php

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCurlDownload_ReplacesRunningExecutable(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "busybin")

	// A tiny executable that stays alive while we overwrite its path.
	if err := os.WriteFile(dest, []byte("#!/bin/sh\nexec sleep 60\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dest)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start busy binary: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	time.Sleep(50 * time.Millisecond)

	payload := []byte("replacement-payload-ok")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	// Direct curl -o dest fails with exit 23 / ETXTBSY while the binary runs.
	direct := exec.Command("curl", "-fsSL", "-o", dest, ts.URL)
	if err := direct.Run(); err == nil {
		t.Fatal("expected direct curl -o into a running binary to fail")
	}

	if err := curlDownload(context.Background(), ts.URL, dest); err != nil {
		t.Fatalf("curlDownload: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("dest content = %q, want %q", got, payload)
	}
}
