package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStart_CallsPreStartBeforeExec(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "prestart")
	script := filepath.Join(dir, "child.sh")
	body := "#!/bin/sh\n" +
		"test -f \"" + marker + "\" || exit 42\n" +
		"while true; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	called := false
	sup := NewSupervisor(dir)
	def := Definition{
		ID:          "child",
		Managed:     true,
		ManagedCmd:  "/bin/sh",
		ManagedArgs: script,
		ManagedDir:  dir,
		PreStart: func() error {
			called = true
			return os.WriteFile(marker, []byte("ok"), 0644)
		},
	}
	if err := sup.Start(def); err != nil {
		t.Fatal(err)
	}
	defer sup.Stop("child") //nolint:errcheck

	if !called {
		t.Fatal("PreStart was not called")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if sup.IsRunning("child") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("child exited; PreStart likely ran after exec or not at all")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStart_PreStartErrorDoesNotLaunch(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "child.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile true; do sleep 1; done\n"), 0755); err != nil {
		t.Fatal(err)
	}

	sup := NewSupervisor(dir)
	def := Definition{
		ID:          "child",
		Managed:     true,
		ManagedCmd:  "/bin/sh",
		ManagedArgs: script,
		ManagedDir:  dir,
		PreStart: func() error {
			return errors.New("leftover still running")
		},
	}
	err := sup.Start(def)
	if err == nil {
		sup.Stop("child") //nolint:errcheck
		t.Fatal("Start succeeded; want PreStart error")
	}
	if sup.IsRunning("child") {
		sup.Stop("child") //nolint:errcheck
		t.Fatal("child started after PreStart error")
	}
}
