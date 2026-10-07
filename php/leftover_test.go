package php

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestStopLeftover_KillsProcessMatchingConfPath covers the live failure:
// a leftover php-fpm master (PPID 1) still holds the pool socket, so the
// supervisor's new start fails with "Another FPM instance seems to already
// listen" and the dashboard reports stopped.
func TestStopLeftover_KillsProcessMatchingConfPath(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)
	confPath := FPMConfigPath(ver, serverRoot)
	socketPath := FPMSocket(ver, serverRoot)

	if err := os.WriteFile(confPath, []byte("; test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socketPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(t.TempDir(), "fake-fpm.sh")
	body := "#!/bin/sh\n" +
		"echo ready\n" +
		"while true; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(script, "--fpm-config", confPath)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if processAlive(cmd.Process.Pid) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("leftover process never started")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := StopLeftover(ver, serverRoot); err != nil {
		t.Fatalf("StopLeftover: %v", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
		cmd.Process = nil
	case <-time.After(3 * time.Second):
		t.Fatal("leftover process still running after StopLeftover")
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("stale socket still present: %v", err)
	}
}

// TestStopLeftover_KillsPidFileProcess covers a leftover master whose
// process title no longer contains the conf path, but which still wrote a pid file.
func TestStopLeftover_KillsPidFileProcess(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)

	script := filepath.Join(t.TempDir(), "php-fpm")
	body := "#!/bin/sh\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	pidPath := FPMPidPath(ver, serverRoot)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := StopLeftover(ver, serverRoot); err != nil {
		t.Fatalf("StopLeftover: %v", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
		cmd.Process = nil
	case <-time.After(3 * time.Second):
		t.Fatal("pid-file process still running after StopLeftover")
	}
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
