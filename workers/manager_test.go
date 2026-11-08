package workers

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgormly/devctl/db"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

type fixture struct {
	db         *sql.DB
	sup        *services.Supervisor
	m          *Manager
	serverRoot string
	siteID     string
	siteDir    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	serverRoot := t.TempDir()
	siteDir := t.TempDir()
	sm := sites.NewManager(sqlDB, sites.NewCaddyClient("http://127.0.0.1:1"), serverRoot)
	site, err := sm.Create(context.Background(), sites.CreateSiteInput{Domain: "app.test", RootPath: siteDir, PHPVersion: "8.3"})
	if err != nil {
		t.Fatal(err)
	}
	sup := services.NewSupervisor(serverRoot)
	t.Cleanup(sup.StopAll)
	return &fixture{db: sqlDB, sup: sup, m: NewManager(sqlDB, sup, serverRoot), serverRoot: serverRoot, siteID: site.ID, siteDir: siteDir}
}

func waitFor(t *testing.T, what string, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func readLines(path string) []string {
	b, _ := os.ReadFile(path)
	return strings.Fields(string(b))
}

func TestStatus(t *testing.T) {
	cases := []struct {
		enabled  bool
		startErr string
		running  bool
		want     Status
	}{
		{false, "", false, StatusStopped},
		{false, "boom", true, StatusStopped},
		{true, "boom", false, StatusFailed},
		{true, "", true, StatusRunning},
		{true, "", false, StatusRestarting},
	}
	for _, c := range cases {
		if got := status(c.enabled, c.startErr, c.running); got != c.want {
			t.Errorf("status(%v, %q, %v) = %s, want %s", c.enabled, c.startErr, c.running, got, c.want)
		}
	}
}

func TestManager_CustomWorkerRunsInSiteDirAndLogs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	st, err := f.m.Add(ctx, f.siteID, "custom", "echo hello-from-$(basename \"$PWD\"); exec sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusRunning || !st.Enabled {
		t.Fatalf("after add: status %s enabled %v, want running and enabled (error %q)", st.Status, st.Enabled, st.Error)
	}
	if want := "app.test-custom-" + strconv.FormatInt(st.ID, 10); st.LogID != want {
		t.Errorf("log id = %q, want %q", st.LogID, want)
	}
	logPath := paths.LogPath(f.serverRoot, st.LogID)
	want := "hello-from-" + filepath.Base(f.siteDir)
	waitFor(t, "worker output in its log", 5*time.Second, func() bool {
		b, _ := os.ReadFile(logPath)
		return strings.Contains(string(b), want)
	})

	list, err := f.m.List(ctx, f.siteID)
	if err != nil || len(list) != 1 || list[0].ID != st.ID {
		t.Fatalf("List = %+v, %v; want the one worker", list, err)
	}
	if other, _ := f.m.List(ctx, "other-site"); len(other) != 0 {
		t.Errorf("List(other-site) = %+v, want none", other)
	}

	if err := f.m.Remove(ctx, st.ID); err != nil {
		t.Fatal(err)
	}
	if f.sup.IsRunning(supervisorID(st.ID)) {
		t.Error("worker still running after Remove")
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("log file still there after Remove: %v", err)
	}
	if _, err := f.m.Get(ctx, st.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Remove = %v, want ErrNotFound", err)
	}
}

func TestManager_StopStartRestart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pids := filepath.Join(f.siteDir, "pids")

	st, err := f.m.Add(ctx, f.siteID, "custom", "echo $$ >> pids; exec sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first pid", 5*time.Second, func() bool { return len(readLines(pids)) == 1 })

	st, err = f.m.Stop(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusStopped || st.Enabled || f.sup.IsRunning(supervisorID(st.ID)) {
		t.Fatalf("after stop: status %s enabled %v", st.Status, st.Enabled)
	}

	st, err = f.m.Start(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusRunning || !st.Enabled {
		t.Fatalf("after start: status %s enabled %v", st.Status, st.Enabled)
	}
	waitFor(t, "second pid", 5*time.Second, func() bool { return len(readLines(pids)) == 2 })

	st, err = f.m.Restart(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusRunning {
		t.Fatalf("after restart: status %s", st.Status)
	}
	waitFor(t, "third pid", 5*time.Second, func() bool { return len(readLines(pids)) == 3 })
	got := readLines(pids)
	if got[1] == got[2] {
		t.Errorf("restart kept pid %s; want a new process", got[1])
	}
}

func TestManager_AddRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.m.Add(ctx, f.siteID, "cron", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown type: err = %v, want ErrInvalid", err)
	}
	if _, err := f.m.Add(ctx, f.siteID, "queue", "--queue=high"); !errors.Is(err, ErrInvalid) {
		t.Errorf("queue with command: err = %v, want ErrInvalid", err)
	}
	if _, err := f.m.Add(ctx, "no-such-site", "custom", "sleep 1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown site: err = %v, want ErrNotFound", err)
	}

	sm := sites.NewManager(f.db, sites.NewCaddyClient("http://127.0.0.1:1"), f.serverRoot)
	proxy, err := sm.Create(ctx, sites.CreateSiteInput{Domain: "vite.test", Proxy: sites.ServiceProxy("127.0.0.1:5173")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Add(ctx, proxy.ID, "custom", "sleep 1"); !errors.Is(err, ErrInvalid) {
		t.Errorf("proxy site: err = %v, want ErrInvalid", err)
	}
}

func TestManager_FailedStartIsReportedAndBuiltinTypesAreUnique(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	st, err := f.m.Add(ctx, f.siteID, "queue", "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusFailed || !strings.Contains(st.Error, "PHP 8.3 is not installed") {
		t.Fatalf("queue without PHP: status %s error %q, want failed with a PHP hint", st.Status, st.Error)
	}
	if st.CommandLine != "php8.3 artisan queue:work" {
		t.Errorf("command line = %q", st.CommandLine)
	}
	if _, err := f.m.Add(ctx, f.siteID, "queue", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("second queue worker: err = %v, want ErrInvalid", err)
	}
	if _, err := f.m.Add(ctx, f.siteID, "custom", "sleep 30"); err != nil {
		t.Errorf("first custom worker: %v", err)
	}
	if _, err := f.m.Add(ctx, f.siteID, "custom", "sleep 30"); err != nil {
		t.Errorf("second custom worker: %v", err)
	}

	st, err = f.m.Stop(ctx, st.ID)
	if err != nil || st.Status != StatusStopped || st.Error != "" {
		t.Fatalf("stop failed worker: status %s error %q err %v, want stopped with no error", st.Status, st.Error, err)
	}
}

func TestManager_StartEnabledAfterDaemonRestart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	on, err := f.m.Add(ctx, f.siteID, "custom", "exec sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	off, err := f.m.Add(ctx, f.siteID, "custom", "exec sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Stop(ctx, off.ID); err != nil {
		t.Fatal(err)
	}
	f.sup.StopAll()

	sup := services.NewSupervisor(f.serverRoot)
	t.Cleanup(sup.StopAll)
	m := NewManager(f.db, sup, f.serverRoot)
	m.StartEnabled(ctx)

	if st, _ := m.Get(ctx, on.ID); st.Status != StatusRunning {
		t.Errorf("enabled worker after restart: status %s (error %q), want running", st.Status, st.Error)
	}
	if st, _ := m.Get(ctx, off.ID); st.Status != StatusStopped {
		t.Errorf("stopped worker after restart: status %s, want stopped", st.Status)
	}
}

func TestManager_DeletingTheSiteStopsAndRemovesWorkers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sm := sites.NewManager(f.db, sites.NewCaddyClient("http://127.0.0.1:1"), f.serverRoot)
	sm.SetBeforeDelete(f.m.RemoveSite)

	st, err := f.m.Add(ctx, f.siteID, "custom", "exec sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	if err := sm.Delete(ctx, f.siteID); err != nil {
		t.Fatal(err)
	}
	if f.sup.IsRunning(supervisorID(st.ID)) {
		t.Error("worker still running after its site was deleted")
	}
	if all, err := f.m.List(ctx, ""); err != nil || len(all) != 0 {
		t.Errorf("workers after site delete = %+v, %v; want none", all, err)
	}
}

func TestManager_CrashLoopIsReported(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.sup.Run(ctx)

	st, err := f.m.Add(ctx, f.siteID, "custom", "echo started; exit 3")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "crash loop", 30*time.Second, func() bool {
		st, _ = f.m.Get(ctx, st.ID)
		return st.CrashLoop
	})
	if st.Crashes < crashLoopAt {
		t.Errorf("crashes = %d, want >= %d", st.Crashes, crashLoopAt)
	}
	if b, _ := os.ReadFile(paths.LogPath(f.serverRoot, st.LogID)); strings.Count(string(b), "started") < crashLoopAt {
		t.Errorf("log shows %d starts, want >= %d (the supervisor restarts it)", strings.Count(string(b), "started"), crashLoopAt)
	}

	st, _ = f.m.Stop(ctx, st.ID)
	if st.CrashLoop || st.Crashes != 0 {
		t.Errorf("after stop: crashes %d crash_loop %v, want cleared", st.Crashes, st.CrashLoop)
	}
}
