package services

import (
	"bufio"
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// managedProc holds state for a single supervised child process.
type managedProc struct {
	def    Definition
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// Supervisor manages devctl-supervised child processes (e.g. Laravel Reverb).
// It forks each managed service as a child process, auto-restarts on crash,
// and stops all children cleanly on shutdown.
type Supervisor struct {
	mu       sync.Mutex
	procs    map[string]*managedProc
	siteHome string // home directory of the site owner (e.g. "/home/alice")
}

// NewSupervisor creates an idle Supervisor.
// siteHome is the home directory of the non-root user who owns ~/sites
// (e.g. "/home/alice") — used to resolve the working directory of managed
// child processes.
func NewSupervisor(siteHome string) *Supervisor {
	return &Supervisor{
		procs:    make(map[string]*managedProc),
		siteHome: siteHome,
	}
}

// Start forks a managed service as a child process.
// It is a no-op if the service is already running.
// ManagedDir is always $HOME/sites/<id> by convention.
func (s *Supervisor) Start(def Definition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.procs[def.ID]; ok && p.cmd.ProcessState == nil {
		// Already running (ProcessState is nil until the process exits).
		return nil
	}

	return s.startLocked(def)
}

// startLocked launches the process. Must be called with s.mu held.
func (s *Supervisor) startLocked(def Definition) error {
	managedDir := def.ManagedDir
	if managedDir == "" {
		managedDir = s.siteHome + "/sites/" + def.ID
	}

	args := strings.Fields(def.ManagedArgs)
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, def.ManagedCmd, args...)
	cmd.Dir = managedDir

	// If ManagedEnvFile is set, load key=value pairs as extra environment
	// variables for the child process. This allows secrets (e.g.
	// MEILI_MASTER_KEY) to be injected from a file written at install time.
	if def.ManagedEnvFile != "" {
		extra := loadEnvFile(def.ManagedEnvFile)
		cmd.Env = append(os.Environ(), extra...)
	}

	// Pipe stdout and stderr to the log with a service-ID prefix.
	pr, pw, err := os.Pipe()
	if err != nil {
		cancel()
		return err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		cancel()
		pr.Close()
		pw.Close()
		return err
	}
	pw.Close() // parent side no longer needs the write end

	s.procs[def.ID] = &managedProc{def: def, cmd: cmd, cancel: cancel}

	// Log output lines prefixed with the service ID.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				log.Printf("[%s] %s", def.ID, strings.TrimRight(string(buf[:n]), "\n"))
			}
			if err != nil {
				break
			}
		}
		pr.Close()
	}()

	log.Printf("supervisor: started %s (pid %d)", def.ID, cmd.Process.Pid)
	return nil
}

// Stop sends SIGTERM to the managed process and waits up to 10 s before
// sending SIGKILL.
func (s *Supervisor) Stop(id string) error {
	s.mu.Lock()
	p, ok := s.procs[id]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	cancel := p.cancel
	cmd := p.cmd
	delete(s.procs, id)
	s.mu.Unlock()

	cancel() // sends SIGTERM via context (exec.CommandContext)

	done := make(chan struct{})
	go func() {
		cmd.Wait() //nolint:errcheck
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		if cmd.Process != nil {
			cmd.Process.Kill() //nolint:errcheck
		}
		<-done
	}

	log.Printf("supervisor: stopped %s", id)
	return nil
}

// Restart stops then starts a managed service.
func (s *Supervisor) Restart(def Definition) error {
	if err := s.Stop(def.ID); err != nil {
		return err
	}
	return s.Start(def)
}

// IsRunning returns true when the process exists and has not yet exited.
func (s *Supervisor) IsRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.procs[id]
	if !ok {
		return false
	}
	return p.cmd.ProcessState == nil
}

// Run watches for unexpected process exits and auto-restarts them.
// It also stops all processes when ctx is cancelled.
// Call as a goroutine: go supervisor.Run(ctx).
func (s *Supervisor) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.stopAll()
			return
		case <-ticker.C:
			s.restartCrashed()
		}
	}
}

// restartCrashed finds processes that have exited unexpectedly and restarts them.
func (s *Supervisor) restartCrashed() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, p := range s.procs {
		if p.cmd.ProcessState != nil {
			// Process has exited; restart it.
			log.Printf("supervisor: %s exited unexpectedly, restarting", id)
			def := p.def
			delete(s.procs, id)
			if err := s.startLocked(def); err != nil {
				log.Printf("supervisor: restart %s: %v", id, err)
			}
		}
	}
}

// stopAll stops every managed process. Called on shutdown.
func (s *Supervisor) stopAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.procs))
	for id := range s.procs {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	for _, id := range ids {
		s.Stop(id) //nolint:errcheck
	}
}

// loadEnvFile reads a key=value file and returns a slice of "KEY=VALUE" strings
// suitable for appending to exec.Cmd.Env. Lines that are blank or start with #
// are skipped.
func loadEnvFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var pairs []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "=") {
			pairs = append(pairs, line)
		}
	}
	return pairs
}
