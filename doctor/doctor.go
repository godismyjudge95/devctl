// Package doctor runs a registry of environment health checks. The daemon
// owns the checks (so the CLI and the dashboard show the same report); the
// CLI adds the one check only a client can make: whether the daemon answers.
package doctor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Severity is the outcome of one check.
type Severity string

const (
	OK   Severity = "ok"
	Warn Severity = "warn"
	Fail Severity = "fail"
)

// Result is one check outcome as the API and CLI report it.
type Result struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Details  []string `json:"details,omitempty"`
	// Remedy is what the user can do. Root-only remedies name the
	// `sudo devctl elevate …` command.
	Remedy string `json:"remedy,omitempty"`
	// Fixable is true when `doctor:fix` can repair this without root.
	Fixable bool `json:"fixable"`
}

// Report is the full doctor output, in registry order.
type Report struct {
	Checks []Result `json:"checks"`
}

// Failed counts checks with severity fail.
func (r Report) Failed() int {
	n := 0
	for _, c := range r.Checks {
		if c.Severity == Fail {
			n++
		}
	}
	return n
}

// FixResult is the outcome of one attempted fix.
type FixResult struct {
	ID    string `json:"id"`
	Error string `json:"error,omitempty"`
}

// FixReport is the response of a fix run: what was attempted, then a fresh report.
type FixReport struct {
	Fixes []FixResult `json:"fixes"`
	Report
}

// Finding is what a check observed. Fix is set only when a safe,
// unprivileged repair exists for exactly this observed state.
type Finding struct {
	Severity Severity
	Message  string
	Details  []string
	Remedy   string
	Fix      func(context.Context) error
}

// Check is one registry row.
type Check struct {
	ID    string
	Title string
	Run   func(context.Context) Finding
}

// Outcome pairs a reported Result with the fix the check offered.
type Outcome struct {
	Result
	fix func(context.Context) error
}

// checkTimeout bounds each check so one hung probe cannot stall the report.
const checkTimeout = 8 * time.Second

// Run evaluates every check concurrently and returns outcomes in registry order.
func Run(ctx context.Context, checks []Check) []Outcome {
	out := make([]Outcome, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = evaluate(ctx, c)
		}()
	}
	wg.Wait()
	return out
}

func evaluate(ctx context.Context, c Check) (o Outcome) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	// Checks run on goroutines; a panic here would take down the daemon.
	defer func() {
		if r := recover(); r != nil {
			o = Outcome{Result: Result{ID: c.ID, Title: c.Title, Severity: Fail, Message: fmt.Sprintf("check crashed: %v", r)}}
		}
	}()
	f := c.Run(ctx)
	return Outcome{
		Result: Result{
			ID:       c.ID,
			Title:    c.Title,
			Severity: f.Severity,
			Message:  f.Message,
			Details:  f.Details,
			Remedy:   f.Remedy,
			Fixable:  f.Fix != nil && f.Severity != OK,
		},
		fix: f.Fix,
	}
}

// Results strips the fixes from outcomes.
func Results(outcomes []Outcome) Report {
	r := Report{Checks: make([]Result, len(outcomes))}
	for i, o := range outcomes {
		r.Checks[i] = o.Result
	}
	return r
}

// ApplyFixes runs the offered fixes in registry order, so a fix that starts
// Caddy runs before checks that depend on it. When only is non-empty, fixes
// for other IDs are skipped.
func ApplyFixes(ctx context.Context, outcomes []Outcome, only map[string]bool) []FixResult {
	fixes := []FixResult{}
	for _, o := range outcomes {
		if !o.Fixable || (len(only) > 0 && !only[o.ID]) {
			continue
		}
		fr := FixResult{ID: o.ID}
		if err := o.fix(ctx); err != nil {
			fr.Error = err.Error()
		}
		fixes = append(fixes, fr)
	}
	return fixes
}

// Okf, Warnf, and Failf build findings without a fix.
func Okf(format string, a ...any) Finding {
	return Finding{Severity: OK, Message: fmt.Sprintf(format, a...)}
}

func Warnf(remedy, format string, a ...any) Finding {
	return Finding{Severity: Warn, Message: fmt.Sprintf(format, a...), Remedy: remedy}
}

func Failf(remedy, format string, a ...any) Finding {
	return Finding{Severity: Fail, Message: fmt.Sprintf(format, a...), Remedy: remedy}
}
