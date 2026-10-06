package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStop_SendsSIGTERM(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	marker := filepath.Join(dir, "got-term")
	script := filepath.Join(dir, "child.sh")
	body := "#!/bin/sh\n" +
		"trap 'echo term > \"" + marker + "\"; exit 0' TERM\n" +
		"echo ready > \"" + ready + "\"\n" +
		"while true; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	sup := NewSupervisor(dir)
	def := Definition{
		ID:          "child",
		Managed:     true,
		ManagedCmd:  "/bin/sh",
		ManagedArgs: script,
		ManagedDir:  dir,
	}
	if err := sup.Start(def); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never wrote ready file")
		}
		time.Sleep(20 * time.Millisecond)
	}

	done := make(chan error, 1)
	go func() { done <- sup.Stop("child") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not return")
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal("child did not handle SIGTERM (marker missing). SIGKILL would skip the trap")
	}
	if strings.TrimSpace(string(got)) != "term" {
		t.Fatalf("marker = %q", got)
	}
	if sup.IsRunning("child") {
		t.Fatal("child still running after Stop")
	}
}

