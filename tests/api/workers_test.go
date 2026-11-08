//go:build integration

package apitest

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type workerResp struct {
	ID          int64  `json:"id"`
	SiteID      string `json:"site_id"`
	Domain      string `json:"domain"`
	Type        string `json:"type"`
	Command     string `json:"command"`
	Enabled     bool   `json:"enabled"`
	Status      string `json:"status"`
	Error       string `json:"error"`
	Crashes     int    `json:"crashes"`
	CrashLoop   bool   `json:"crash_loop"`
	LogID       string `json:"log_id"`
	CommandLine string `json:"command_line"`
}

// The test VM is arm64 and devctl PHP is x86_64-only, so these tests use
// custom workers (plain shell) and leave PHP and Node types to unit tests.
const tickLoop = `echo $$ >> pids; while true; do echo tick; sleep 1; done`

func addWorker(t *testing.T, siteID, typ, command string) workerResp {
	t.Helper()
	body, status := httpPost(t, "/api/workers", map[string]string{"site_id": siteID, "type": typ, "command": command})
	if status != http.StatusCreated {
		t.Fatalf("POST /api/workers: status %d: %s", status, body)
	}
	w := decodeJSON[workerResp](t, body)
	t.Cleanup(func() { httpDelete(t, fmt.Sprintf("/api/workers/%d", w.ID)) })
	return w
}

func workerAction(t *testing.T, id int64, action string) workerResp {
	t.Helper()
	body, status := httpPost(t, fmt.Sprintf("/api/workers/%d/%s", id, action), nil)
	if status != http.StatusOK {
		t.Fatalf("%s worker %d: status %d: %s", action, id, status, body)
	}
	return decodeJSON[workerResp](t, body)
}

func getWorker(t *testing.T, id int64) workerResp {
	t.Helper()
	return decodeJSON[workerResp](t, httpGet(t, fmt.Sprintf("/api/workers/%d", id)))
}

func pollWorker(t *testing.T, id int64, what string, timeout time.Duration, ok func(workerResp) bool) workerResp {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		w := getWorker(t, id)
		if ok(w) {
			return w
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker %d: timed out waiting for %s; last %+v", id, what, w)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func workerLog(t *testing.T, logID string) string {
	t.Helper()
	body, status := httpGetRaw(t, "/api/logs/"+logID+"/tail")
	if status != http.StatusOK {
		return ""
	}
	return string(body)
}

func pollLog(t *testing.T, logID, what string, timeout time.Duration, ok func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok(workerLog(t, logID)) {
		if time.Now().After(deadline) {
			t.Fatalf("log %s: timed out waiting for %s; log:\n%s", logID, what, workerLog(t, logID))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func pidsIn(dir string) []string {
	b, _ := os.ReadFile(filepath.Join(dir, "pids"))
	return strings.Fields(string(b))
}

func processAlive(pid string) bool {
	n, err := strconv.Atoi(pid)
	if err != nil {
		return false
	}
	return syscall.Kill(n, 0) == nil
}

func TestWorkers_CustomWorkerLifecycle(t *testing.T) {
	dir := nodeSiteDir(t, nil)
	siteID := createNodeSite(t, "workers-life-go.test", dir)

	w := addWorker(t, siteID, "custom", tickLoop)
	if w.Status != "running" || !w.Enabled || w.CommandLine != tickLoop {
		t.Fatalf("after add: %+v, want running and enabled", w)
	}
	pollLog(t, w.LogID, "tick output", 10*time.Second, func(s string) bool { return strings.Contains(s, "tick") })

	listed := decodeJSON[[]workerResp](t, httpGet(t, "/api/workers?site_id="+siteID))
	if len(listed) != 1 || listed[0].ID != w.ID {
		t.Fatalf("GET /api/workers?site_id: %+v, want the one worker", listed)
	}
	if !strings.Contains(string(httpGet(t, "/api/logs")), `"`+w.LogID+`"`) {
		t.Errorf("Logs page list does not include %s", w.LogID)
	}

	w = workerAction(t, w.ID, "stop")
	if w.Status != "stopped" || w.Enabled {
		t.Fatalf("after stop: %+v", w)
	}
	first := pidsIn(dir)
	if len(first) != 1 {
		t.Fatalf("pids after first run = %v, want one", first)
	}
	waitGone(t, first[0])

	w = workerAction(t, w.ID, "start")
	if w.Status != "running" || !w.Enabled {
		t.Fatalf("after start: %+v", w)
	}
	pollUntil(t, "second run", 10*time.Second, func() bool { return len(pidsIn(dir)) == 2 })
	w = workerAction(t, w.ID, "restart")
	if w.Status != "running" {
		t.Fatalf("after restart: %+v", w)
	}
	pollUntil(t, "three runs", 10*time.Second, func() bool { return len(pidsIn(dir)) == 3 })

	if body, status := httpDelete(t, fmt.Sprintf("/api/workers/%d", w.ID)); status != http.StatusNoContent {
		t.Fatalf("DELETE worker: status %d: %s", status, body)
	}
	httpGetStatus(t, fmt.Sprintf("/api/workers/%d", w.ID), http.StatusNotFound)
	waitGone(t, pidsIn(dir)[2])
}

func TestWorkers_RejectsBadInput(t *testing.T) {
	siteID := createNodeSite(t, "workers-bad-go.test", nodeSiteDir(t, nil))
	cases := []struct {
		body map[string]string
		want int
	}{
		{map[string]string{"site_id": siteID, "type": "cron"}, http.StatusBadRequest},
		{map[string]string{"site_id": siteID, "type": "custom"}, http.StatusBadRequest},
		{map[string]string{"site_id": siteID, "type": "queue", "command": "--queue=high"}, http.StatusBadRequest},
		{map[string]string{"site_id": "no-such-site", "type": "custom", "command": "sleep 1"}, http.StatusNotFound},
	}
	for _, c := range cases {
		if body, status := httpPost(t, "/api/workers", c.body); status != c.want {
			t.Errorf("POST %v: status %d, want %d: %s", c.body, status, c.want, body)
		}
	}
	httpGetStatus(t, "/api/workers/999999", http.StatusNotFound)
}

func TestWorkers_RestartAfterCrashAndReportCrashLoop(t *testing.T) {
	siteID := createNodeSite(t, "workers-crash-go.test", nodeSiteDir(t, nil))
	w := addWorker(t, siteID, "custom", "echo started; sleep 1; exit 3")

	pollLog(t, w.LogID, "a restart after the crash", 20*time.Second, func(s string) bool { return strings.Count(s, "started") >= 2 })
	w = pollWorker(t, w.ID, "crash_loop", 60*time.Second, func(w workerResp) bool { return w.CrashLoop })
	if w.Crashes < 3 || !w.Enabled {
		t.Errorf("crash loop worker = %+v, want crashes >= 3 and still enabled", w)
	}

	w = workerAction(t, w.ID, "stop")
	if w.Status != "stopped" || w.CrashLoop {
		t.Errorf("after stop: %+v, want stopped without crash_loop", w)
	}
}

func TestWorkers_SurviveDaemonRestart(t *testing.T) {
	onDir := nodeSiteDir(t, nil)
	on := addWorker(t, createNodeSite(t, "workers-boot-go.test", onDir), "custom", tickLoop)
	offDir := nodeSiteDir(t, nil)
	off := addWorker(t, createNodeSite(t, "workers-boot-off-go.test", offDir), "custom", tickLoop)
	pollUntil(t, "first run of the worker to stop", 10*time.Second, func() bool { return len(pidsIn(offDir)) == 1 })
	workerAction(t, off.ID, "stop")
	pollUntil(t, "first run", 10*time.Second, func() bool { return len(pidsIn(onDir)) == 1 })

	restart := exec.Command("systemctl", "restart", "devctl")
	if runtime.GOOS == "darwin" {
		restart = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/ai.devctl", os.Getuid()))
	}
	if out, err := restart.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v: %s", restart, err, out)
	}
	pollUntil(t, "devctl to come back", 30*time.Second, func() bool {
		resp, err := http.Get(baseURL() + "/api/workers") //nolint:noctx
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	pollWorker(t, on.ID, "running after restart", 15*time.Second, func(w workerResp) bool { return w.Status == "running" })
	pollUntil(t, "second run", 10*time.Second, func() bool { return len(pidsIn(onDir)) == 2 })
	if pids := pidsIn(onDir); processAlive(pids[0]) {
		t.Errorf("worker from before the restart (pid %s) is still alive", pids[0])
	}
	if w := getWorker(t, off.ID); w.Status != "stopped" || len(pidsIn(offDir)) != 1 {
		t.Errorf("stopped worker after restart: %+v pids %v, want stopped and not started again", w, pidsIn(offDir))
	}
}

func TestWorkers_StoppedWhenTheSiteIsDeleted(t *testing.T) {
	dir := nodeSiteDir(t, nil)
	siteID := createNodeSite(t, "workers-del-go.test", dir)
	w := addWorker(t, siteID, "custom", tickLoop)
	pollUntil(t, "first run", 10*time.Second, func() bool { return len(pidsIn(dir)) == 1 })

	if body, status := httpDelete(t, "/api/sites/"+siteID); status != http.StatusNoContent {
		t.Fatalf("DELETE site: status %d: %s", status, body)
	}
	httpGetStatus(t, fmt.Sprintf("/api/workers/%d", w.ID), http.StatusNotFound)
	waitGone(t, pidsIn(dir)[0])
}

func pollUntil(t *testing.T, what string, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func waitGone(t *testing.T, pid string) {
	t.Helper()
	pollUntil(t, "pid "+pid+" to exit", 15*time.Second, func() bool { return !processAlive(pid) })
}
