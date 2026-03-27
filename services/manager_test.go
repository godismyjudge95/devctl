package services

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetStatus_HealthCheck_RetrySucceeds verifies that when the health check
// fails on the first N calls but succeeds on the (N+1)-th call, GetStatus
// returns StatusRunning (not StatusWarning), provided HealthCheckRetries >= N.
func TestGetStatus_HealthCheck_RetrySucceeds(t *testing.T) {
	// Use the goroutine-based IsRunning path: a non-nil done channel that never
	// closes causes IsRunning to return true.
	sup := &Supervisor{procs: make(map[string]*managedProc)}
	sup.procs["mysql"] = &managedProc{done: make(chan struct{})}

	mgr := NewManager(nil, sup)

	// Health check fails the first 2 times, succeeds on the 3rd.
	var callCount atomic.Int32
	orig := runHealthCheck
	t.Cleanup(func() { runHealthCheck = orig })
	runHealthCheck = func(cmd string) error {
		n := int(callCount.Add(1))
		if n < 3 {
			return errors.New("socket not ready yet")
		}
		return nil
	}

	def := Definition{
		ID:                    "mysql",
		Managed:               true,
		HealthCheck:           "mysqladmin ping",
		HealthCheckRetries:    3,
		HealthCheckRetryDelay: 1 * time.Millisecond, // fast in tests
	}

	status := mgr.GetStatus(def)

	if status != StatusRunning {
		t.Errorf("expected StatusRunning after retries, got %q", status)
	}
	if callCount.Load() != 3 {
		t.Errorf("expected health check called 3 times, got %d", callCount.Load())
	}
}

// TestGetStatus_HealthCheck_AllRetriesFail verifies that when all health check
// attempts fail (including retries), GetStatus returns StatusWarning.
func TestGetStatus_HealthCheck_AllRetriesFail(t *testing.T) {
	sup := &Supervisor{procs: make(map[string]*managedProc)}
	sup.procs["mysql"] = &managedProc{done: make(chan struct{})}

	mgr := NewManager(nil, sup)

	var callCount atomic.Int32
	orig := runHealthCheck
	t.Cleanup(func() { runHealthCheck = orig })
	runHealthCheck = func(cmd string) error {
		callCount.Add(1)
		return errors.New("always fails")
	}

	def := Definition{
		ID:                    "mysql",
		Managed:               true,
		HealthCheck:           "mysqladmin ping",
		HealthCheckRetries:    2,
		HealthCheckRetryDelay: 1 * time.Millisecond,
	}

	status := mgr.GetStatus(def)

	if status != StatusWarning {
		t.Errorf("expected StatusWarning when all retries fail, got %q", status)
	}
	// 1 initial attempt + 2 retries = 3 total calls
	if callCount.Load() != 3 {
		t.Errorf("expected 3 health check calls (1 + 2 retries), got %d", callCount.Load())
	}
}

// TestGetStatus_HealthCheck_NoRetries verifies that with HealthCheckRetries==0
// (the default), a single failure immediately produces StatusWarning — no
// change to existing behaviour.
func TestGetStatus_HealthCheck_NoRetries(t *testing.T) {
	sup := &Supervisor{procs: make(map[string]*managedProc)}
	sup.procs["mysql"] = &managedProc{done: make(chan struct{})}

	mgr := NewManager(nil, sup)

	var callCount atomic.Int32
	orig := runHealthCheck
	t.Cleanup(func() { runHealthCheck = orig })
	runHealthCheck = func(cmd string) error {
		callCount.Add(1)
		return errors.New("fails")
	}

	def := Definition{
		ID:          "mysql",
		Managed:     true,
		HealthCheck: "mysqladmin ping",
		// HealthCheckRetries defaults to 0
	}

	status := mgr.GetStatus(def)

	if status != StatusWarning {
		t.Errorf("expected StatusWarning with no retries, got %q", status)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 health check call, got %d", callCount.Load())
	}
}
