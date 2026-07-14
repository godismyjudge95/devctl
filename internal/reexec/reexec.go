// Package reexec restarts the current process in-place without systemctl.
// Used for self-update and API restart so non-root daemons need no sudo.
package reexec

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Schedule re-execs the current binary after delay. It runs in a new goroutine.
// After delay it calls AfterStop (if non-nil) then syscall.Exec.
//
// AfterStop should stop child processes / flush state. The process image is
// replaced; deferred closes in the caller will not run.
func Schedule(delay time.Duration, afterStop func()) {
	go func() {
		time.Sleep(delay)
		if afterStop != nil {
			afterStop()
		}
		if err := Now(); err != nil {
			log.Printf("reexec: %v", err)
		}
	}()
}

// Now replaces the current process with a fresh exec of the same binary.
func Now() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// Use original argv so "devctl daemon" stays "devctl daemon".
	argv := os.Args
	env := os.Environ()
	log.Printf("reexec: exec %s %v", exe, argv)
	return syscall.Exec(exe, argv, env)
}
