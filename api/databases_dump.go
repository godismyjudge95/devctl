package api

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/danielgormly/devctl/database"
	"github.com/danielgormly/devctl/paths"
)

// dumpTool is how one engine streams full-database SQL through the client
// wrappers in {serverRoot}/bin.
type dumpTool struct {
	importCmd dumpCmd
	exportCmd dumpCmd
	env       func(cfg database.Config) []string
}

type dumpCmd struct {
	bin  string
	args func(cfg database.Config, db string) []string
}

var dumpTools = map[string]dumpTool{
	"mysql": {
		importCmd: dumpCmd{"mysql", func(cfg database.Config, db string) []string {
			return append(mysqlConnArgs(cfg), "--default-character-set=utf8mb4", "--database="+db)
		}},
		exportCmd: dumpCmd{"mysqldump", func(cfg database.Config, db string) []string {
			return append(mysqlConnArgs(cfg),
				"--default-character-set=utf8mb4", "--single-transaction",
				"--routines", "--triggers", "--", db)
		}},
		env: func(cfg database.Config) []string {
			if cfg.Password == "" {
				return nil
			}
			return []string{"MYSQL_PWD=" + cfg.Password}
		},
	},
	"postgres": {
		importCmd: dumpCmd{"psql", func(cfg database.Config, db string) []string {
			return []string{"--no-psqlrc", "--quiet", "--set=ON_ERROR_STOP=1", "--dbname=" + pgConnInfo(cfg, db)}
		}},
		exportCmd: dumpCmd{"pg_dump", func(cfg database.Config, db string) []string {
			return []string{"--no-owner", "--no-privileges", "--dbname=" + pgConnInfo(cfg, db)}
		}},
		// The psql and pg_dump wrappers export PGPASSWORD themselves.
		env: func(database.Config) []string {
			return []string{"PGOPTIONS=-c client_min_messages=warning"}
		},
	},
}

func mysqlConnArgs(cfg database.Config) []string {
	args := []string{"--user=" + cfg.User}
	if cfg.Socket != "" {
		return append(args, "--socket="+cfg.Socket)
	}
	return append(args, "--protocol=TCP", "--host="+cfg.Host, "--port="+cfg.Port)
}

// pgConnInfo builds a libpq key/value string so database names with spaces,
// quotes, or "=" are never parsed as a connection URI.
func pgConnInfo(cfg database.Config, db string) string {
	quote := func(v string) string {
		v = strings.ReplaceAll(v, `\`, `\\`)
		return "'" + strings.ReplaceAll(v, `'`, `\'`) + "'"
	}
	return fmt.Sprintf("host=%s port=%s user=%s dbname=%s",
		quote(cfg.Host), quote(cfg.Port), quote(cfg.User), quote(db))
}

// dumpTarget resolves the engine (404 / 503 when it is missing or stopped)
// and its dump tool.
func (s *Server) dumpTarget(r *http.Request) (database.Engine, dumpTool, error) {
	kind := r.PathValue("engine")
	eng, err := s.openEngine(kind)
	if err != nil {
		return nil, dumpTool{}, err
	}
	tool, ok := dumpTools[kind]
	if !ok {
		return nil, dumpTool{}, errEngine("import and export are not supported for "+kind, http.StatusBadRequest)
	}
	return eng, tool, nil
}

func (s *Server) dumpCommand(ctx context.Context, kind string, c dumpCmd, db string) *exec.Cmd {
	cfg := s.engineConfig(kind)
	cmd := exec.CommandContext(ctx, filepath.Join(paths.BinDir(s.serverRoot), c.bin), c.args(cfg, db)...)
	cmd.Env = append(os.Environ(), dumpTools[kind].env(cfg)...)
	return cmd
}

type importResult struct {
	Status  string `json:"status"`
	Bytes   int64  `json:"bytes"`
	Created bool   `json:"created"`
}

// handleImportDatabase streams the request body (plain or gzip SQL) into the
// engine CLI. The body is never buffered, so dump size does not change memory.
//
//	POST /api/databases/{engine}/databases/{name}/import?create=1
func (s *Server) handleImportDatabase(w http.ResponseWriter, r *http.Request) {
	res, err := s.importDatabase(r)
	// Browsers drop the response when the server stops reading an upload
	// early, so read the rest before reporting an error.
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) importDatabase(r *http.Request) (importResult, error) {
	eng, tool, err := s.dumpTarget(r)
	if err != nil {
		return importResult{Status: "ok"}, err
	}
	kind, name := r.PathValue("engine"), r.PathValue("name")
	create, _ := strconv.ParseBool(r.URL.Query().Get("create"))
	return importInto(eng, name, create, func() (int64, error) {
		return runImport(s.dumpCommand(r.Context(), kind, tool.importCmd, name), r.Body, kind == "postgres")
	})
}

// importInto runs the import into name, creating it first when create is set.
// A database this call created is dropped again when the import fails, so a
// failed import never leaves a half-loaded new database behind.
func importInto(eng database.Engine, name string, create bool, run func() (int64, error)) (importResult, error) {
	res := importResult{Status: "ok"}
	exists, err := databaseExists(eng, name)
	if err != nil {
		return res, err
	}
	if !exists {
		if !create {
			return res, errEngine(fmt.Sprintf("database %q does not exist", name), http.StatusNotFound)
		}
		if err := eng.CreateDatabase(name); err != nil {
			return res, err
		}
		res.Created = true
	}
	res.Bytes, err = run()
	if err == nil || !res.Created {
		return res, err
	}
	if dropErr := eng.DropDatabase(name); dropErr != nil {
		return res, fmt.Errorf("%w (database %q was created for this import and could not be dropped: %v)", err, name, dropErr)
	}
	return res, fmt.Errorf("%w (database %q was created for this import and has been dropped)", err, name)
}

func databaseExists(eng database.Engine, name string) (bool, error) {
	dbs, err := eng.ListDatabases()
	if err != nil {
		return false, err
	}
	for _, d := range dbs {
		if d.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// runImport pipes body into cmd's stdin, un-gzipping it when it starts with
// the gzip magic bytes. It returns the number of SQL bytes sent. On failure
// the error carries the engine's stderr.
func runImport(cmd *exec.Cmd, body io.Reader, postgres bool) (int64, error) {
	src, err := sqlReader(body)
	if err != nil {
		return 0, err
	}
	in := &countingReader{r: src}
	stderr := &tailBuffer{max: 16 << 10}
	cmd.Stdin = in
	if postgres {
		cmd.Stdin = newPgIdempotentServers(in)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	err = cmd.Run()
	if in.err != nil && !errors.Is(in.err, io.EOF) {
		// The engine saw EOF and exited cleanly; the real failure is the upload.
		return in.n, fmt.Errorf("read dump: %w", in.err)
	}
	return in.n, engineFailure(err, stderr)
}

func sqlReader(r io.Reader) (io.Reader, error) {
	br := bufio.NewReader(r)
	magic, err := br.Peek(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		return gzip.NewReader(br)
	}
	return br, nil
}

// exportCommitSize is how much dump output is held back before the 200 is
// sent. A dump that fails inside it still gets a JSON error response.
const exportCommitSize = 64 << 10

// handleExportDatabase streams a full SQL dump of one database.
//
//	GET /api/databases/{engine}/databases/{name}/export
func (s *Server) handleExportDatabase(w http.ResponseWriter, r *http.Request) {
	_, tool, err := s.dumpTarget(r)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	kind, name := r.PathValue("engine"), r.PathValue("name")
	filename := name + "-" + time.Now().Format("20060102-150405") + ".sql"
	if err := streamExport(w, s.dumpCommand(r.Context(), kind, tool.exportCmd, name), filename); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
	}
}

// streamExport runs cmd and copies its stdout to w. It returns an error only
// while nothing has been written yet. A failure after the 200 aborts the
// connection, so clients see a broken transfer, never a silently short dump.
func streamExport(w http.ResponseWriter, cmd *exec.Cmd, filename string) error {
	stderr := &tailBuffer{max: 16 << 10}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	out := bufio.NewReaderSize(stdout, exportCommitSize)
	head, peekErr := out.Peek(exportCommitSize)
	complete := peekErr != nil // stdout closed before the buffer filled
	if complete {
		if err := cmd.Wait(); err != nil {
			return engineFailure(err, stderr)
		}
	}
	w.Header().Set("Content-Type", "application/sql")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	if complete {
		w.Write(head) //nolint:errcheck
		return nil
	}
	_, copyErr := io.Copy(w, out)
	if waitErr := cmd.Wait(); copyErr != nil || waitErr != nil {
		log.Printf("database export %s: copy: %v, engine: %v", filename, copyErr, engineFailure(waitErr, stderr))
		panic(http.ErrAbortHandler)
	}
	return nil
}

func engineFailure(err error, stderr *tailBuffer) error {
	if err == nil {
		return nil
	}
	if msg := strings.TrimSpace(string(stderr.buf)); msg != "" {
		return errors.New(msg)
	}
	return err
}

type countingReader struct {
	r   io.Reader
	n   int64
	err error
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if err != nil {
		c.err = err
	}
	return n, err
}

// tailBuffer keeps the last max bytes written. Engine errors come last, after
// any notices, and a long run must not grow memory.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}
