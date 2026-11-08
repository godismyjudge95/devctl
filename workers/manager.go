package workers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

// Status is the live state of a worker.
type Status string

const (
	StatusRunning    Status = "running"
	StatusStopped    Status = "stopped"
	StatusRestarting Status = "restarting" // exited; the supervisor restarts it after a backoff
	StatusFailed     Status = "failed"     // could not start; Error says why
)

// crashLoopAt is the number of fast crashes in a row that the UI reports
// (and notifies about) as a crash loop.
const crashLoopAt = 3

// ErrNotFound is returned for an unknown worker or site id.
var ErrNotFound = errors.New("not found")

// Worker is a stored worker plus the site values it runs with.
type Worker struct {
	ID      int64  `json:"id"`
	SiteID  string `json:"site_id"`
	Domain  string `json:"domain"`
	Type    Type   `json:"type"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`

	root       string
	phpVersion string
}

// State is a worker and what it is doing now.
type State struct {
	Worker
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
	// Crashes counts crashes in a row that each came soon after a start.
	Crashes   int    `json:"crashes"`
	CrashLoop bool   `json:"crash_loop"`
	LogID     string `json:"log_id"`
	// CommandLine is what the worker runs, as a person would type it.
	CommandLine string `json:"command_line"`
}

// Manager stores workers and runs them under the supervisor.
type Manager struct {
	q          *dbq.Queries
	sup        *services.Supervisor
	serverRoot string

	mu sync.Mutex
	// startErr is the last start error of each enabled worker that failed.
	startErr map[int64]string
}

// NewManager creates a Manager. The daemon runs as the site user, so workers
// do too.
func NewManager(db *sql.DB, sup *services.Supervisor, serverRoot string) *Manager {
	return &Manager{q: dbq.New(db), sup: sup, serverRoot: serverRoot, startErr: map[int64]string{}}
}

func supervisorID(id int64) string { return "worker-" + strconv.FormatInt(id, 10) }

// logID names the worker log file in the Logs page: <domain>-<type>-<id>.
func logID(w Worker) string {
	return fmt.Sprintf("%s-%s-%d", w.Domain, w.Type, w.ID)
}

func (m *Manager) all(ctx context.Context) ([]Worker, error) {
	rows, err := m.q.ListWorkers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Worker, 0, len(rows))
	for _, r := range rows {
		out = append(out, Worker{
			ID: r.ID, SiteID: r.SiteID, Domain: r.Domain, Type: Type(r.Type),
			Command: r.Command, Enabled: r.Enabled != 0,
			root: r.RootPath, phpVersion: r.PhpVersion,
		})
	}
	return out, nil
}

func (m *Manager) get(ctx context.Context, id int64) (Worker, error) {
	all, err := m.all(ctx)
	if err != nil {
		return Worker{}, err
	}
	for _, w := range all {
		if w.ID == id {
			return w, nil
		}
	}
	return Worker{}, fmt.Errorf("worker %d: %w", id, ErrNotFound)
}

func status(enabled bool, startErr string, running bool) Status {
	switch {
	case !enabled:
		return StatusStopped
	case startErr != "":
		return StatusFailed
	case running:
		return StatusRunning
	default:
		return StatusRestarting
	}
}

func (m *Manager) state(w Worker) State {
	m.mu.Lock()
	errMsg := m.startErr[w.ID]
	m.mu.Unlock()
	id := supervisorID(w.ID)
	st := State{
		Worker:      w,
		Status:      status(w.Enabled, errMsg, m.sup.IsRunning(id)),
		Error:       errMsg,
		LogID:       logID(w),
		CommandLine: display(w.Type, w.phpVersion, w.Command),
	}
	if w.Enabled {
		st.Crashes = m.sup.Failures(id)
		st.CrashLoop = st.Crashes >= crashLoopAt
	}
	return st
}

// List returns the workers of one site, or of every site when siteID is "".
func (m *Manager) List(ctx context.Context, siteID string) ([]State, error) {
	all, err := m.all(ctx)
	if err != nil {
		return nil, err
	}
	out := []State{}
	for _, w := range all {
		if siteID == "" || w.SiteID == siteID {
			out = append(out, m.state(w))
		}
	}
	return out, nil
}

// Get returns one worker.
func (m *Manager) Get(ctx context.Context, id int64) (State, error) {
	w, err := m.get(ctx, id)
	if err != nil {
		return State{}, err
	}
	return m.state(w), nil
}

// Add stores a worker for the site and starts it.
func (m *Manager) Add(ctx context.Context, siteID string, t Type, command string) (State, error) {
	command = strings.TrimSpace(command)
	if err := validate(t, command); err != nil {
		return State{}, err
	}
	if _, err := m.q.GetSite(ctx, siteID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return State{}, fmt.Errorf("site %q: %w", siteID, ErrNotFound)
		}
		return State{}, err
	}
	if !kinds[t].multiple {
		existing, err := m.List(ctx, siteID)
		if err != nil {
			return State{}, err
		}
		for _, w := range existing {
			if w.Type == t {
				return State{}, fmt.Errorf("%w: %s already has a %s worker (id %d)", ErrInvalid, w.Domain, t, w.ID)
			}
		}
	}
	id, err := m.q.CreateWorker(ctx, dbq.CreateWorkerParams{SiteID: siteID, Type: string(t), Command: command})
	if err != nil {
		return State{}, err
	}
	return m.Start(ctx, id)
}

// Start enables the worker and starts it. A start error is kept in the
// returned State (status failed), not returned, because the worker stays
// enabled and starts again with the daemon.
func (m *Manager) Start(ctx context.Context, id int64) (State, error) {
	w, err := m.setEnabled(ctx, id, true)
	if err != nil {
		return State{}, err
	}
	m.run(w, m.sup.Start)
	return m.state(w), nil
}

// Restart enables the worker and restarts it with fresh site values (PHP
// version, Node version, command).
func (m *Manager) Restart(ctx context.Context, id int64) (State, error) {
	w, err := m.setEnabled(ctx, id, true)
	if err != nil {
		return State{}, err
	}
	m.run(w, m.sup.Restart)
	return m.state(w), nil
}

// Stop disables the worker and stops it.
func (m *Manager) Stop(ctx context.Context, id int64) (State, error) {
	w, err := m.setEnabled(ctx, id, false)
	if err != nil {
		return State{}, err
	}
	m.halt(w.ID)
	return m.state(w), nil
}

// Remove stops the worker and deletes it with its log files.
func (m *Manager) Remove(ctx context.Context, id int64) error {
	w, err := m.get(ctx, id)
	if err != nil {
		return err
	}
	m.halt(w.ID)
	if err := m.q.DeleteWorker(ctx, id); err != nil {
		return err
	}
	removeLogs(paths.LogPath(m.serverRoot, logID(w)))
	return nil
}

// RemoveSite stops the workers of a site that is being deleted. The rows go
// with the site (ON DELETE CASCADE).
func (m *Manager) RemoveSite(ctx context.Context, siteID string) {
	ws, err := m.List(ctx, siteID)
	if err != nil {
		log.Printf("workers: list for site %s: %v", siteID, err)
		return
	}
	for _, w := range ws {
		m.halt(w.ID)
		removeLogs(paths.LogPath(m.serverRoot, w.LogID))
	}
}

// StartEnabled starts every enabled worker. The daemon calls it at startup.
func (m *Manager) StartEnabled(ctx context.Context) {
	all, err := m.all(ctx)
	if err != nil {
		log.Printf("workers: list: %v", err)
		return
	}
	for _, w := range all {
		if w.Enabled {
			m.run(w, m.sup.Start)
		}
	}
}

func (m *Manager) setEnabled(ctx context.Context, id int64, on bool) (Worker, error) {
	w, err := m.get(ctx, id)
	if err != nil {
		return Worker{}, err
	}
	if w.Enabled != on {
		v := int64(0)
		if on {
			v = 1
		}
		if err := m.q.SetWorkerEnabled(ctx, dbq.SetWorkerEnabledParams{Enabled: v, ID: id}); err != nil {
			return Worker{}, err
		}
		w.Enabled = on
	}
	return w, nil
}

// run builds the worker definition and hands it to start (Start or Restart),
// recording any error for the worker status.
func (m *Manager) run(w Worker, start func(services.Definition) error) {
	def, err := m.definitionFor(w)
	if err == nil {
		err = start(def)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		log.Printf("workers: start %s %s: %v", w.Domain, w.Type, err)
		m.startErr[w.ID] = err.Error()
		return
	}
	delete(m.startErr, w.ID)
}

func (m *Manager) halt(id int64) {
	_ = m.sup.Stop(supervisorID(id))
	m.mu.Lock()
	delete(m.startErr, id)
	m.mu.Unlock()
}

func (m *Manager) definitionFor(w Worker) (services.Definition, error) {
	args, err := argv(m.serverRoot, w.root, w.phpVersion, w.Type, w.Command)
	if err != nil {
		return services.Definition{}, err
	}
	return services.Definition{
		ID:               supervisorID(w.ID),
		Label:            w.Domain + " " + kinds[w.Type].label,
		Managed:          true,
		ManagedCmd:       args[0],
		ManagedExtraArgs: args[1:],
		ManagedDir:       w.root,
		// php8.4, composer, node, and npm resolve for custom commands.
		ManagedExtraEnv: []string{"PATH=" + paths.BinDir(m.serverRoot) + string(os.PathListSeparator) + os.Getenv("PATH")},
		Log:             paths.LogPath(m.serverRoot, logID(w)),
		ProcessGroup:    true,
	}, nil
}

func removeLogs(path string) {
	for _, p := range []string{path, path + ".1", path + ".2", path + ".3"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Printf("workers: remove log %s: %v", p, err)
		}
	}
}
