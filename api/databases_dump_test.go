package api

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgormly/devctl/database"
)

func shell(script string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", script)
}

// sqlStream yields size bytes of INSERT statements without holding them in memory.
type sqlStream struct {
	left int64
	line []byte
	pos  int
}

func newSQLStream(size int64) *sqlStream {
	return &sqlStream{left: size, line: []byte("INSERT INTO t VALUES (1, 'lorem ipsum dolor sit amet');\n")}
}

func (s *sqlStream) Read(p []byte) (int, error) {
	if s.left <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > s.left {
		p = p[:s.left]
	}
	n := 0
	for n < len(p) {
		c := copy(p[n:], s.line[s.pos:])
		n += c
		s.pos = (s.pos + c) % len(s.line)
	}
	s.left -= int64(n)
	return n, nil
}

func gzipPipe(src io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		zw, _ := gzip.NewWriterLevel(pw, gzip.BestSpeed)
		_, err := io.Copy(zw, src)
		if err == nil {
			err = zw.Close()
		}
		pw.CloseWithError(err)
	}()
	return pr
}

func TestRunImport_LargeGzipDumpKeepsHeapFlat(t *testing.T) {
	const size = 512 << 20
	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var ms runtime.MemStats
		for {
			runtime.ReadMemStats(&ms)
			if ms.HeapInuse > peak.Load() {
				peak.Store(ms.HeapInuse)
			}
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()

	n, err := runImport(shell("cat >/dev/null"), gzipPipe(newSQLStream(size)), false)
	close(stop)
	<-done
	if err != nil {
		t.Fatalf("runImport: %v", err)
	}
	if n != size {
		t.Fatalf("imported %d bytes, want %d", n, size)
	}
	if p := peak.Load(); p > 64<<20 {
		t.Fatalf("heap in use peaked at %d MiB while importing a %d MiB dump", p>>20, size>>20)
	}
}

func TestRunImport_EngineReceivesSQLText(t *testing.T) {
	const sql = "CREATE TABLE t (id int);\n"
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte(sql)) //nolint:errcheck
	zw.Close()

	for name, body := range map[string]io.Reader{
		"plain": strings.NewReader(sql),
		"gzip":  &gz,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := shell(`grep -q "CREATE TABLE t" || { echo "engine did not get SQL text" >&2; exit 1; }`)
			n, err := runImport(cmd, body, false)
			if err != nil {
				t.Fatalf("runImport: %v", err)
			}
			if n != int64(len(sql)) {
				t.Fatalf("n = %d, want %d", n, len(sql))
			}
		})
	}
}

func TestRunImport_ReturnsEngineStderr(t *testing.T) {
	cmd := shell(`cat >/dev/null; echo "ERROR 1064 (42000) at line 1: You have an error in your SQL syntax" >&2; exit 1`)
	_, err := runImport(cmd, strings.NewReader("SELEC 1;\n"), false)
	if err == nil || !strings.Contains(err.Error(), "ERROR 1064") {
		t.Fatalf("want engine stderr in error, got %v", err)
	}
}

func TestRunImport_EngineExitsBeforeBodyEnds(t *testing.T) {
	cmd := shell(`head -c 10 >/dev/null; echo "psql:<stdin>:1: ERROR:  syntax error" >&2; exit 3`)
	errc := make(chan error, 1)
	go func() {
		_, err := runImport(cmd, newSQLStream(32<<20), false)
		errc <- err
	}()
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "syntax error") {
			t.Fatalf("want engine stderr in error, got %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runImport hung after the engine exited")
	}
}

func TestRunImport_CorruptGzipFails(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(bytes.Repeat([]byte("INSERT INTO t VALUES (1);\n"), 4096)) //nolint:errcheck
	zw.Close()
	truncated := gz.Bytes()[:gz.Len()/2]

	_, err := runImport(shell("cat >/dev/null"), bytes.NewReader(truncated), false)
	if err == nil || !strings.Contains(err.Error(), "read dump") {
		t.Fatalf("truncated gzip: want a read dump error, got %v", err)
	}
}

func TestStreamExport_SmallDump(t *testing.T) {
	rec := httptest.NewRecorder()
	err := streamExport(rec, shell(`printf 'CREATE TABLE t (id int);\n'`), "app-1.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); got != "CREATE TABLE t (id int);\n" {
		t.Fatalf("body = %q", got)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "app-1.sql") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
}

func TestStreamExport_LargeDump(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := streamExport(rec, shell(`head -c 1000000 /dev/zero`), "app.sql"); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != 1000000 {
		t.Fatalf("body length = %d", rec.Body.Len())
	}
}

func TestStreamExport_EarlyFailureReturnsEngineError(t *testing.T) {
	rec := httptest.NewRecorder()
	err := streamExport(rec, shell(`echo "-- MySQL dump"; echo "mysqldump: Got error: 1049: Unknown database 'nope'" >&2; exit 2`), "nope.sql")
	if err == nil || !strings.Contains(err.Error(), "Unknown database") {
		t.Fatalf("want engine error, got %v", err)
	}
	if rec.Body.Len() != 0 || rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("partial download started before the error: %q %v", rec.Body.String(), rec.Header())
	}
}

func TestStreamExport_LateFailureAbortsConnection(t *testing.T) {
	rec := httptest.NewRecorder()
	defer func() {
		if r := recover(); r != http.ErrAbortHandler {
			t.Fatalf("want panic(http.ErrAbortHandler), got %v", r)
		}
	}()
	streamExport(rec, shell(`head -c 200000 /dev/zero; echo "pg_dump: error: connection lost" >&2; exit 1`), "app.sql") //nolint:errcheck
}

func TestPGConnInfoQuotesDatabaseName(t *testing.T) {
	cfg := database.Config{Host: "127.0.0.1", Port: "5432", User: "root"}
	got := pgConnInfo(cfg, `it's a=b\c`)
	want := `host='127.0.0.1' port='5432' user='root' dbname='it\'s a=b\\c'`
	if got != want {
		t.Fatalf("pgConnInfo = %s\nwant          %s", got, want)
	}
}

func TestTailBufferKeepsLastBytes(t *testing.T) {
	tb := &tailBuffer{max: 8}
	io.WriteString(tb, "NOTICE: one\n") //nolint:errcheck
	io.WriteString(tb, "ERROR: x")      //nolint:errcheck
	if string(tb.buf) != "ERROR: x" {
		t.Fatalf("tail = %q", tb.buf)
	}
}

type fakeDumpEngine struct {
	database.Engine
	dbs     []string
	dropErr error
	dropped []string
}

func (f *fakeDumpEngine) ListDatabases() ([]database.DatabaseInfo, error) {
	out := make([]database.DatabaseInfo, len(f.dbs))
	for i, n := range f.dbs {
		out[i] = database.DatabaseInfo{Name: n}
	}
	return out, nil
}

func (f *fakeDumpEngine) CreateDatabase(name string) error {
	f.dbs = append(f.dbs, name)
	return nil
}

func (f *fakeDumpEngine) DropDatabase(name string) error {
	if f.dropErr != nil {
		return f.dropErr
	}
	f.dropped = append(f.dropped, name)
	return nil
}

func failingImport() (int64, error) { return 10, errors.New("ERROR 1064 at line 1") }

func TestImportInto_CreatedDatabaseDroppedOnFailure(t *testing.T) {
	eng := &fakeDumpEngine{}
	res, err := importInto(eng, "shop", true, failingImport)
	if !res.Created {
		t.Fatal("want Created for a new database")
	}
	if len(eng.dropped) != 1 || eng.dropped[0] != "shop" {
		t.Fatalf("want shop dropped after a failed import, dropped %v", eng.dropped)
	}
	if err == nil || !strings.Contains(err.Error(), "ERROR 1064") || !strings.Contains(err.Error(), "has been dropped") {
		t.Fatalf("want engine error and drop note, got %v", err)
	}
}

func TestImportInto_DropFailureIsReported(t *testing.T) {
	eng := &fakeDumpEngine{dropErr: errors.New("database is in use")}
	_, err := importInto(eng, "shop", true, failingImport)
	if err == nil || !strings.Contains(err.Error(), "could not be dropped") || !strings.Contains(err.Error(), "database is in use") {
		t.Fatalf("want drop failure in error, got %v", err)
	}
}

func TestImportInto_ExistingDatabaseKeptOnFailure(t *testing.T) {
	eng := &fakeDumpEngine{dbs: []string{"shop"}}
	res, err := importInto(eng, "shop", true, failingImport)
	if res.Created || len(eng.dropped) != 0 {
		t.Fatalf("existing database must not be dropped: created=%v dropped=%v", res.Created, eng.dropped)
	}
	if err == nil || strings.Contains(err.Error(), "dropped") {
		t.Fatalf("want plain engine error, got %v", err)
	}
}

func TestImportInto_MissingDatabaseWithoutCreate(t *testing.T) {
	ran := false
	_, err := importInto(&fakeDumpEngine{}, "shop", false, func() (int64, error) { ran = true; return 0, nil })
	if ran || err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want not-found without running the import, ran=%v err=%v", ran, err)
	}
}

func TestImportInto_CreatedDatabaseKeptOnSuccess(t *testing.T) {
	eng := &fakeDumpEngine{}
	res, err := importInto(eng, "shop", true, func() (int64, error) { return 42, nil })
	if err != nil || !res.Created || res.Bytes != 42 || len(eng.dropped) != 0 {
		t.Fatalf("got res=%+v err=%v dropped=%v", res, err, eng.dropped)
	}
}
