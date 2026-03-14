package services

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// managedProc holds state for a single supervised child process or goroutine.
type managedProc struct {
	def     Definition
	cmd     *exec.Cmd // non-nil for exec-based services
	cancel  context.CancelFunc
	logFile *os.File      // non-nil when def.Log != ""; closed after the process exits
	done    chan struct{} // non-nil for goroutine-based services; closed when RunFunc returns
}

// Supervisor manages devctl-supervised services (child processes or embedded goroutines).
// It auto-restarts on crash and stops all children cleanly on shutdown.
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

// Start forks a managed service as a child process (or launches it as an
// embedded goroutine if def.RunFunc is non-nil).
// It is a no-op if the service is already running.
func (s *Supervisor) Start(def Definition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.procs[def.ID]; ok {
		// Check if still running.
		if p.done != nil {
			// Goroutine proc: still alive if done is not closed.
			select {
			case <-p.done:
				// exited — fall through to restart
			default:
				return nil // still running
			}
		} else if p.cmd != nil && p.cmd.ProcessState == nil {
			// Process proc: still alive.
			return nil
		}
	}

	return s.startLocked(def)
}

// startLocked launches the service. Must be called with s.mu held.
func (s *Supervisor) startLocked(def Definition) error {
	if def.RunFunc != nil {
		return s.startEmbedded(def)
	}
	return s.startProcess(def)
}

// startEmbedded launches def.RunFunc as a supervised goroutine.
// Must be called with s.mu held.
func (s *Supervisor) startEmbedded(def Definition) error {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	logW := s.newServiceLogWriter(def)

	proc := &managedProc{def: def, cancel: cancel, done: done}
	s.procs[def.ID] = proc

	go func() {
		defer close(done)
		if err := def.RunFunc(ctx, logW); err != nil && ctx.Err() == nil {
			log.Printf("[%s] exited with error: %v", def.ID, err)
		}
		if f, ok := logW.(*serviceLogWriter); ok && f.file != nil {
			f.file.Close()
		}
	}()

	log.Printf("supervisor: started embedded service %s", def.ID)
	return nil
}

// startProcess launches a child process. Must be called with s.mu held.
func (s *Supervisor) startProcess(def Definition) error {
	managedDir := def.ManagedDir
	if managedDir == "" {
		managedDir = s.siteHome + "/sites/" + def.ID
	}

	args := strings.Fields(def.ManagedArgs)
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, def.ManagedCmd, args...)
	cmd.Dir = managedDir

	// If ManagedUser is set, drop privileges to that user before exec.
	// This is required for services that refuse to run as root (e.g. PostgreSQL).
	if def.ManagedUser != "" {
		u, err := user.Lookup(def.ManagedUser)
		if err != nil {
			cancel()
			return fmt.Errorf("supervisor: lookup user %q: %w", def.ManagedUser, err)
		}
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			},
		}
	}

	// If ManagedEnvFile is set, load key=value pairs as extra environment
	// variables for the child process.
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

	// Open log file for tee if def.Log is set.
	var logFile *os.File
	if def.Log != "" {
		if err := os.MkdirAll(filepath.Dir(def.Log), 0755); err == nil {
			logFile, _ = os.OpenFile(def.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		}
		if logFile != nil {
			s.procs[def.ID].logFile = logFile
		}
	}

	// Log output lines prefixed with the service ID.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, errRead := pr.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				log.Printf("[%s] %s", def.ID, strings.TrimRight(string(chunk), "\n"))
				if logFile != nil {
					_, _ = logFile.Write(chunk)
				}
			}
			if errRead != nil {
				break
			}
		}
		pr.Close()
		if logFile != nil {
			logFile.Close()
		}
	}()

	log.Printf("supervisor: started %s (pid %d)", def.ID, cmd.Process.Pid)
	return nil
}

// newServiceLogWriter builds an io.Writer that writes to both journald (via
// log.Printf) and optionally to the service's log file.
func (s *Supervisor) newServiceLogWriter(def Definition) io.Writer {
	w := &serviceLogWriter{id: def.ID}
	if def.Log != "" {
		if err := os.MkdirAll(filepath.Dir(def.Log), 0755); err == nil {
			f, _ := os.OpenFile(def.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			w.file = f
		}
	}
	return w
}

// serviceLogWriter implements io.Writer. Each Write call is split on newlines
// and each line is sent to log.Printf prefixed with the service ID.
// Raw bytes are also tee'd to file when non-nil.
type serviceLogWriter struct {
	id   string
	file *os.File
}

func (w *serviceLogWriter) Write(p []byte) (int, error) {
	if w.file != nil {
		_, _ = w.file.Write(p)
	}
	msg := strings.TrimRight(string(p), "\n")
	if msg != "" {
		log.Printf("[%s] %s", w.id, msg)
	}
	return len(p), nil
}

// Stop sends SIGTERM (or cancels the context for goroutine procs) and waits
// up to 10 s before force-killing.
func (s *Supervisor) Stop(id string) error {
	s.mu.Lock()
	p, ok := s.procs[id]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	cancel := p.cancel
	cmd := p.cmd
	done := p.done
	delete(s.procs, id)
	s.mu.Unlock()

	cancel() // cancel context — triggers SIGTERM for exec procs, stops RunFunc for goroutines

	if done != nil {
		// Goroutine proc — wait for RunFunc to return.
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			log.Printf("supervisor: %s goroutine did not stop within 10s", id)
		}
	} else if cmd != nil {
		// Exec proc — wait with optional force-kill.
		waitDone := make(chan struct{})
		go func() {
			cmd.Wait() //nolint:errcheck
			close(waitDone)
		}()
		select {
		case <-waitDone:
		case <-time.After(10 * time.Second):
			if cmd.Process != nil {
				cmd.Process.Kill() //nolint:errcheck
			}
			<-waitDone
		}
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

// IsRunning returns true when the service exists and has not yet exited.
func (s *Supervisor) IsRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.procs[id]
	if !ok {
		return false
	}
	if p.done != nil {
		select {
		case <-p.done:
			return false
		default:
			return true
		}
	}
	return p.cmd != nil && p.cmd.ProcessState == nil
}

// Run watches for unexpected exits and auto-restarts them.
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

// restartCrashed finds services that have exited unexpectedly and restarts them.
func (s *Supervisor) restartCrashed() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, p := range s.procs {
		crashed := false
		if p.done != nil {
			select {
			case <-p.done:
				crashed = true
			default:
			}
		} else if p.cmd != nil && p.cmd.ProcessState != nil {
			crashed = true
		}

		if crashed {
			log.Printf("supervisor: %s exited unexpectedly, restarting", id)
			def := p.def
			delete(s.procs, id)
			if err := s.startLocked(def); err != nil {
				log.Printf("supervisor: restart %s: %v", id, err)
			}
		}
	}
}

// stopAll stops every managed service. Called on shutdown.
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
