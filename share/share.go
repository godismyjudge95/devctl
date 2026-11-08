// Package share runs public tunnels for local sites. A share is runtime
// state: it lives as long as its tunnel process and the daemon.
package share

import (
	"bytes"
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status is the lifecycle state of one share.
type Status string

const (
	// Starting means the tunnel process runs but has not printed its URL yet.
	Starting Status = "starting"
	// Active means the public URL is known.
	Active Status = "active"
	// Failed means the tunnel process exited. Error holds the reason.
	Failed Status = "failed"
)

// Share is the public view of one site tunnel.
type Share struct {
	SiteID    string    `json:"site_id"`
	Domain    string    `json:"domain"`
	Status    Status    `json:"status"`
	URL       string    `json:"url,omitempty"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// Process is one running tunnel.
type Process interface {
	// Wait blocks until the process exits.
	Wait() error
	// Stop ends the process and returns after it exits.
	Stop()
}

// Launcher starts a tunnel for domain. The process writes its output to out.
type Launcher func(domain string, out *LineWriter) (Process, error)

// Manager owns every share, keyed by site ID.
type Manager struct {
	launch Launcher

	mu     sync.Mutex
	shares map[string]*tunnel
}

type tunnel struct {
	share   Share
	proc    Process
	stopped bool
	lastErr string
	// settled is closed when the status leaves Starting.
	settled    chan struct{}
	settleOnce sync.Once
}

func (t *tunnel) settle() { t.settleOnce.Do(func() { close(t.settled) }) }

// NewManager returns a Manager that starts tunnels with launch.
func NewManager(launch Launcher) *Manager {
	return &Manager{launch: launch, shares: map[string]*tunnel{}}
}

// Start shares a site. It returns once the public URL is known, the tunnel
// fails, or ctx ends (status is then still Starting). A site that already has
// a live share keeps it. A failed share is replaced.
func (m *Manager) Start(ctx context.Context, siteID, domain string) (Share, error) {
	m.mu.Lock()
	if t, ok := m.shares[siteID]; ok && t.share.Status != Failed {
		m.mu.Unlock()
		return m.wait(ctx, t), nil
	}
	t := &tunnel{
		share:   Share{SiteID: siteID, Domain: domain, Status: Starting, StartedAt: time.Now().UTC()},
		settled: make(chan struct{}),
	}
	m.shares[siteID] = t
	m.mu.Unlock()

	proc, err := m.launch(domain, &LineWriter{fn: func(line string) { m.observe(t, line) }})
	if err != nil {
		m.mu.Lock()
		if m.shares[siteID] == t {
			delete(m.shares, siteID)
		}
		m.mu.Unlock()
		return Share{}, err
	}

	go func() {
		err := proc.Wait()
		m.exited(t, err)
	}()

	m.mu.Lock()
	t.proc = proc
	stopped := t.stopped
	m.mu.Unlock()
	if stopped {
		proc.Stop()
	}
	return m.wait(ctx, t), nil
}

func (m *Manager) wait(ctx context.Context, t *tunnel) Share {
	select {
	case <-t.settled:
	case <-ctx.Done():
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return t.share
}

func (m *Manager) observe(t *tunnel, line string) {
	log.Printf("[share %s] %s", t.share.Domain, line)
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg := errorMessage(line); msg != "" {
		t.lastErr = msg
	}
	if t.share.Status != Starting {
		return
	}
	if url := ParseURL(line); url != "" {
		t.share.Status = Active
		t.share.URL = url
		t.settle()
	}
}

func (m *Manager) exited(t *tunnel, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.share.Status = Failed
	t.share.URL = ""
	switch {
	case t.lastErr != "":
		t.share.Error = t.lastErr
	case err != nil:
		t.share.Error = "tunnel exited: " + err.Error()
	default:
		t.share.Error = "tunnel exited"
	}
	t.settle()
}

// Stop ends the share for siteID. It reports whether a share existed.
func (m *Manager) Stop(siteID string) bool {
	m.mu.Lock()
	t, ok := m.shares[siteID]
	if !ok {
		m.mu.Unlock()
		return false
	}
	delete(m.shares, siteID)
	t.stopped = true
	proc := t.proc
	m.mu.Unlock()
	if proc != nil {
		proc.Stop()
	}
	return true
}

// StopAll ends every share. Called on daemon shutdown and before re-exec.
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.shares))
	for id := range m.shares {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// List returns every share sorted by domain.
func (m *Manager) List() []Share {
	m.mu.Lock()
	out := make([]Share, 0, len(m.shares))
	for _, t := range m.shares {
		out = append(out, t.share)
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out
}

// LineWriter splits process output into lines. exec.Cmd serialises writes
// when Stdout and Stderr are the same writer, so it needs no lock.
type LineWriter struct {
	fn  func(string)
	buf []byte
}

func (w *LineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		if line := strings.TrimRight(string(w.buf[:i]), "\r"); line != "" {
			w.fn(line)
		}
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}
