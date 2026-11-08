package services

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func shortRestartPolicy(t *testing.T, stable, base, maxDelay time.Duration) {
	t.Helper()
	oldStable, oldBase, oldMax := stableRun, backoffBase, backoffMax
	stableRun, backoffBase, backoffMax = stable, base, maxDelay
	t.Cleanup(func() { stableRun, backoffBase, backoffMax = oldStable, oldBase, oldMax })
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *Supervisor) exitedForTest(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.procs[id]
	return ok && p.exited()
}

func TestBackoff(t *testing.T) {
	cases := map[int]time.Duration{0: 0, 1: 0, 2: 4 * time.Second, 3: 8 * time.Second, 5: 32 * time.Second, 6: time.Minute, 100: time.Minute}
	for n, want := range cases {
		if got := backoff(n); got != want {
			t.Errorf("backoff(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestRestartCrashed_BacksOffOnFastCrashes(t *testing.T) {
	shortRestartPolicy(t, time.Hour, 100*time.Millisecond, time.Second)
	dir := t.TempDir()
	starts := filepath.Join(dir, "starts")
	sup := NewSupervisor(dir)
	t.Cleanup(sup.StopAll)
	def := Definition{ID: "crasher", ManagedCmd: "/bin/sh", ManagedExtraArgs: []string{"-c", "echo x >> starts; exit 1"}, ManagedDir: dir}
	if err := sup.Start(def); err != nil {
		t.Fatal(err)
	}
	countStarts := func() int { b, _ := os.ReadFile(starts); return strings.Count(string(b), "x") }

	waitUntil(t, "first exit", func() bool { return sup.exitedForTest("crasher") })
	sup.restartCrashed()
	if got := sup.Failures("crasher"); got != 1 {
		t.Fatalf("after first crash Failures = %d, want 1", got)
	}
	waitUntil(t, "restart after the first crash", func() bool { return countStarts() == 2 })

	waitUntil(t, "second exit", func() bool { return sup.exitedForTest("crasher") })
	sup.restartCrashed()
	if got := sup.Failures("crasher"); got != 2 {
		t.Fatalf("after second crash Failures = %d, want 2", got)
	}
	sup.restartCrashed()
	time.Sleep(50 * time.Millisecond)
	if got := countStarts(); got != 2 {
		t.Fatalf("restarted during the backoff: %d starts, want 2", got)
	}

	time.Sleep(200 * time.Millisecond)
	sup.restartCrashed()
	waitUntil(t, "restart after the backoff", func() bool { return countStarts() == 3 })
	if got := sup.Failures("crasher"); got != 2 {
		t.Errorf("Failures after restart = %d, want 2 (it carries over)", got)
	}
}

func TestFailures_ResetAfterAStableRun(t *testing.T) {
	shortRestartPolicy(t, 300*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond)
	dir := t.TempDir()
	sup := NewSupervisor(dir)
	t.Cleanup(sup.StopAll)
	// Crashes on the first run, then stays up.
	script := "if [ -e ran ]; then exec sleep 30; fi; touch ran; exit 1"
	if err := sup.Start(Definition{ID: "flaky", ManagedCmd: "/bin/sh", ManagedExtraArgs: []string{"-c", script}, ManagedDir: dir}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "first exit", func() bool { return sup.exitedForTest("flaky") })
	sup.restartCrashed()
	waitUntil(t, "restart", func() bool { return sup.IsRunning("flaky") })
	if got := sup.Failures("flaky"); got != 1 {
		t.Fatalf("Failures right after the restart = %d, want 1", got)
	}
	time.Sleep(400 * time.Millisecond)
	if got := sup.Failures("flaky"); got != 0 {
		t.Errorf("Failures after a stable run = %d, want 0", got)
	}
}

func TestStop_ProcessGroupStopsGrandchildren(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	sup := NewSupervisor(dir)
	def := Definition{
		ID:               "group",
		ManagedCmd:       "/bin/sh",
		ManagedExtraArgs: []string{"-c", "sleep 300 & echo $! > child.pid; wait"},
		ManagedDir:       dir,
		ProcessGroup:     true,
	}
	if err := sup.Start(def); err != nil {
		t.Fatal(err)
	}
	var pid int
	waitUntil(t, "grandchild pid", func() bool {
		b, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return pid > 0
	})
	if err := sup.Stop("group"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "grandchild to exit", func() bool {
		return syscall.Kill(pid, 0) == syscall.ESRCH
	})
}
