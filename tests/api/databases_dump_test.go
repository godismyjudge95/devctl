//go:build integration

package apitest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type importResp struct {
	Status  string `json:"status"`
	Bytes   int64  `json:"bytes"`
	Created bool   `json:"created"`
	Error   string `json:"error"`
}

func importDump(t *testing.T, engine, name string, create bool, body io.Reader) (importResp, int) {
	t.Helper()
	u := fmt.Sprintf("%s/api/databases/%s/databases/%s/import", baseURL(), engine, url.PathEscape(name))
	if create {
		u += "?create=1"
	}
	req, err := http.NewRequest(http.MethodPost, u, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/sql")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", u, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out importResp
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("POST %s: decode %q: %v", u, raw, err)
	}
	return out, resp.StatusCode
}

func exportDump(t *testing.T, engine, name string) ([]byte, int, http.Header) {
	t.Helper()
	u := fmt.Sprintf("%s/api/databases/%s/databases/%s/export", baseURL(), engine, url.PathEscape(name))
	resp, err := http.Get(u) //nolint:noctx
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read: %v", u, err)
	}
	return raw, resp.StatusCode, resp.Header
}

func countRows(t *testing.T, engine, db, table string) string {
	t.Helper()
	body, status := httpPost(t, "/api/databases/"+engine+"/query", map[string]any{
		"database": db,
		"sql":      "SELECT COUNT(*) FROM " + table,
	})
	if status != http.StatusOK {
		t.Fatalf("count %s.%s: %d %s", db, table, status, body)
	}
	var res struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(body, &res); err != nil || len(res.Rows) != 1 {
		t.Fatalf("count %s.%s: %s", db, table, body)
	}
	return fmt.Sprint(res.Rows[0][0])
}

func gzipBytes(t *testing.T, b []byte) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func dropDatabaseCleanup(t *testing.T, engine, name string) {
	t.Cleanup(func() {
		httpDelete(t, "/api/databases/"+engine+"/databases/"+url.PathEscape(name))
	})
}

const dumpFixture = `CREATE TABLE people (id INT PRIMARY KEY, name VARCHAR(64) NOT NULL);
INSERT INTO people (id, name) VALUES (1, 'Ada'), (2, 'Grace'), (3, 'Linus');
`

func testDumpRoundTrip(t *testing.T, engine string) {
	ensureInstalledRunning(t, engine, 20*time.Minute)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	src, dst := "devctl_import_"+suffix, "devctl_reimport_"+suffix
	dropDatabaseCleanup(t, engine, src)
	dropDatabaseCleanup(t, engine, dst)

	if res, status := importDump(t, engine, src, false, strings.NewReader(dumpFixture)); status != http.StatusNotFound {
		t.Fatalf("import into a missing database without create: want 404, got %d %+v", status, res)
	}

	res, status := importDump(t, engine, src, true, gzipBytes(t, []byte(dumpFixture)))
	if status != http.StatusOK || !res.Created || res.Bytes != int64(len(dumpFixture)) {
		t.Fatalf("gzip import with create: %d %+v", status, res)
	}
	if n := countRows(t, engine, src, "people"); n != "3" {
		t.Fatalf("rows after import = %s, want 3", n)
	}

	dump, status, hdr := exportDump(t, engine, src)
	if status != http.StatusOK {
		t.Fatalf("export: %d %s", status, dump)
	}
	if !strings.HasPrefix(hdr.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export Content-Disposition = %q", hdr.Get("Content-Disposition"))
	}
	if !bytes.Contains(dump, []byte("people")) || !bytes.Contains(dump, []byte("Grace")) {
		t.Fatalf("export is missing the table or its rows:\n%s", dump)
	}

	res, status = importDump(t, engine, dst, true, bytes.NewReader(dump))
	if status != http.StatusOK {
		t.Fatalf("re-import of the export into another database: %d %+v", status, res)
	}
	if n := countRows(t, engine, dst, "people"); n != "3" {
		t.Fatalf("rows after re-import = %s, want 3", n)
	}

	res, status = importDump(t, engine, src, false, strings.NewReader("SELEC broken;\n"))
	if status != http.StatusBadRequest || !strings.Contains(strings.ToLower(res.Error), "syntax") {
		t.Fatalf("invalid SQL: want 400 with the engine syntax error, got %d %+v", status, res)
	}

	body, status, _ := exportDump(t, engine, "devctl_missing_"+suffix)
	if status != http.StatusBadRequest || !bytes.Contains(body, []byte(`"error"`)) {
		t.Fatalf("export of a missing database: want 400 with the engine error, got %d %s", status, body)
	}
}

func TestDatabaseDump_MySQLRoundTrip(t *testing.T) {
	testDumpRoundTrip(t, "mysql")
}

func TestDatabaseDump_PostgresRoundTrip(t *testing.T) {
	testDumpRoundTrip(t, "postgres")
}

func TestDatabaseDump_UnsupportedEngine(t *testing.T) {
	res, status := importDump(t, "sqlite", "anything", true, strings.NewReader("SELECT 1;"))
	if status != http.StatusBadRequest || !strings.Contains(res.Error, "not supported") {
		t.Fatalf("sqlite import: want 400 not supported, got %d %+v", status, res)
	}
}

func TestDatabaseDump_EnginesAdvertiseImportExport(t *testing.T) {
	var res struct {
		Engines []struct {
			ID           string `json:"id"`
			Capabilities struct {
				ImportExport bool `json:"import_export"`
			} `json:"capabilities"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(httpGet(t, "/api/databases"), &res); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"mysql": true, "postgres": true, "clickhouse": false, "sqlite": false}
	for _, e := range res.Engines {
		if w, ok := want[e.ID]; ok && e.Capabilities.ImportExport != w {
			t.Errorf("%s import_export = %v, want %v", e.ID, e.Capabilities.ImportExport, w)
		}
	}
}

// largeDump yields size bytes of SQL: one table, then ~1 MiB multi-row INSERTs.
type largeDump struct {
	left    int64
	pending []byte
}

func newLargeDump(size int64) *largeDump {
	return &largeDump{left: size, pending: []byte("CREATE TABLE payload (id INT AUTO_INCREMENT PRIMARY KEY, body TEXT);\n")}
}

func (d *largeDump) Read(p []byte) (int, error) {
	if len(d.pending) == 0 {
		if d.left <= 0 {
			return 0, io.EOF
		}
		row := "('" + strings.Repeat("x", 1000) + "')"
		var b strings.Builder
		b.WriteString("INSERT INTO payload (body) VALUES ")
		for i := 0; i < 1000; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(row)
		}
		b.WriteString(";\n")
		d.pending = []byte(b.String())
	}
	n := copy(p, d.pending)
	d.pending = d.pending[n:]
	d.left -= int64(n)
	return n, nil
}

func daemonRSS(pid string) (int64, error) {
	status, err := os.ReadFile("/proc/" + pid + "/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(status), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "VmRSS:" {
			kb, err := strconv.ParseInt(f[1], 10, 64)
			return kb << 10, err
		}
	}
	return 0, fmt.Errorf("no VmRSS in /proc/%s/status", pid)
}

// TestDatabaseDump_LargeImportKeepsDaemonMemoryFlat streams a 512 MiB dump and
// samples the daemon's resident memory during the upload.
func TestDatabaseDump_LargeImportKeepsDaemonMemoryFlat(t *testing.T) {
	const size = 512 << 20
	out, err := exec.Command("systemctl", "show", "-p", "MainPID", "--value", "devctl").Output()
	pid := strings.TrimSpace(string(out))
	if err != nil || pid == "" || pid == "0" {
		t.Skipf("devctl MainPID unavailable (%v): run inside the test machine", err)
	}
	ensureInstalledRunning(t, "mysql", 20*time.Minute)
	name := "devctl_large_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	dropDatabaseCleanup(t, "mysql", name)

	baseline, err := daemonRSS(pid)
	if err != nil {
		t.Fatalf("devctl RSS: %v", err)
	}
	var peak atomic.Int64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if rss, err := daemonRSS(pid); err == nil && rss > peak.Load() {
				peak.Store(rss)
			}
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
	res, status := importDump(t, "mysql", name, true, newLargeDump(size))
	close(stop)
	<-done

	if status != http.StatusOK || res.Bytes < size {
		t.Fatalf("large import: %d %+v", status, res)
	}
	growth := peak.Load() - baseline
	t.Logf("devctl RSS baseline %d MiB, peak %d MiB during a %d MiB import", baseline>>20, peak.Load()>>20, size>>20)
	if growth > 64<<20 {
		t.Fatalf("devctl RSS grew by %d MiB during a %d MiB import", growth>>20, size>>20)
	}
}
