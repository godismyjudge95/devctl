package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/danielgormly/devctl/database"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

func (s *Server) engineStatus(id string) (installed, running bool) {
	inst, ok := s.installers[id]
	if ok {
		installed = inst.IsInstalled()
	}
	for _, st := range s.poller.CurrentStates() {
		if st.ID == id {
			running = st.Status == services.StatusRunning
			if st.Installed {
				installed = true
			}
			return installed, running
		}
	}
	return installed, running
}

func (s *Server) handleListDBEngines(w http.ResponseWriter, r *http.Request) {
	out := []database.EngineInfo{}

	add := func(id, label, kind, host, port string, caps database.Capabilities) {
		installed, running := s.engineStatus(id)
		info := database.EngineInfo{
			ID: id, Label: label, Kind: kind,
			Installed: installed, Running: running,
			Host: host, Port: port, Capabilities: caps,
		}
		if installed {
			cfg := s.engineConfig(kind)
			if cfg.Host != "" {
				info.Host = cfg.Host
			}
			if cfg.Port != "" {
				info.Port = cfg.Port
			}
		}
		out = append(out, info)
	}

	add("mysql", "MySQL", "mysql", "127.0.0.1", "3306", database.Capabilities{
		CreateDatabase: true, DropDatabase: true, CreateTable: true, DropTable: true,
		Truncate: true, RowEdit: true, RowInsert: true,
		RenameDatabase: true, DuplicateDatabase: true, RenameTable: true, DuplicateTable: true, AlterTable: true,
	})
	add("postgres", "PostgreSQL", "postgres", "127.0.0.1", "5432", database.Capabilities{
		CreateDatabase: true, DropDatabase: true, CreateTable: true, DropTable: true,
		Truncate: true, RowEdit: true, RowInsert: true, Schemas: true,
		RenameDatabase: true, DuplicateDatabase: true, RenameTable: true, DuplicateTable: true, AlterTable: true,
	})
	add("clickhouse", "ClickHouse", "clickhouse", "127.0.0.1", "8123", database.Capabilities{
		CreateDatabase: true, DropDatabase: true, DropTable: true, Truncate: true, RowInsert: true,
		RenameDatabase: true, DuplicateDatabase: true, RenameTable: true, DuplicateTable: true, AlterTable: true,
	})

	sqliteInfo := database.EngineInfo{
		ID: "sqlite", Label: "SQLite", Kind: "sqlite",
		Installed: true, Running: true,
		Capabilities: database.Capabilities{
			CreateDatabase: true, DropDatabase: true,
			CreateTable: true, DropTable: true, Truncate: true, RowEdit: true, RowInsert: true,
			RenameDatabase: true, DuplicateDatabase: true, RenameTable: true, DuplicateTable: true, AlterTable: true,
		},
	}
	out = append(out, sqliteInfo)

	writeJSON(w, map[string]any{"engines": out})
}

func (s *Server) engineConfig(kind string) database.Config {
	cfg := database.Config{Kind: kind}
	switch kind {
	case "mysql":
		env := readKVFile(filepath.Join(paths.ServiceDir(s.serverRoot, "mysql"), "config.env"))
		cfg.Host = coalesceKV(env["DB_HOST"], "127.0.0.1")
		cfg.Port = coalesceKV(env["DB_PORT"], "3306")
		cfg.User = coalesceKV(env["DB_USERNAME"], "root")
		cfg.Password = env["DB_PASSWORD"]
		sock := filepath.Join(paths.ServiceDir(s.serverRoot, "mysql"), "mysql.sock")
		if _, err := os.Stat(sock); err == nil {
			cfg.Socket = sock
		}
	case "postgres":
		env := readKVFile(filepath.Join(paths.ServiceDir(s.serverRoot, "postgres"), "config.env"))
		cfg.Host = coalesceKV(env["DB_HOST"], "127.0.0.1")
		cfg.Port = coalesceKV(env["DB_PORT"], "5432")
		cfg.User = coalesceKV(env["DB_USERNAME"], "root")
		cfg.Password = env["DB_PASSWORD"]
	case "clickhouse":
		env := readKVFile(filepath.Join(paths.ServiceDir(s.serverRoot, "clickhouse"), "config.env"))
		cfg.Host = coalesceKV(env["CLICKHOUSE_HOST"], "127.0.0.1")
		cfg.Port = coalesceKV(env["CLICKHOUSE_PORT"], "8123")
		cfg.User = coalesceKV(env["CLICKHOUSE_USER"], "default")
		cfg.Password = env["CLICKHOUSE_PASSWORD"]
	case "sqlite":
		cfg.Files = database.DiscoverSQLite(filepath.Dir(s.serverRoot))
		cfg.SQLiteDir = filepath.Join(s.serverRoot, "sqlite")
	}
	return cfg
}

func (s *Server) openEngine(kind string) (database.Engine, error) {
	switch kind {
	case "mysql", "postgres", "clickhouse":
		installed, running := s.engineStatus(kind)
		if !installed {
			return nil, errEngine("not installed", http.StatusNotFound)
		}
		if !running {
			return nil, errEngine(kind+" is not running", http.StatusServiceUnavailable)
		}
	case "sqlite":
		// Always available — files may be created from the UI.
	default:
		return nil, errEngine("unknown engine", http.StatusNotFound)
	}
	return database.Open(s.engineConfig(kind), s.dbPool)
}

type engineError struct {
	msg  string
	code int
}

func (e engineError) Error() string { return e.msg }

func errEngine(msg string, code int) error { return engineError{msg, code} }

func writeEngineErr(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if e, ok := err.(engineError); ok {
		writeError(w, e.msg, e.code)
		return
	}
	writeError(w, err.Error(), http.StatusBadRequest)
}

func (s *Server) withEngine(w http.ResponseWriter, r *http.Request) database.Engine {
	kind := r.PathValue("engine")
	eng, err := s.openEngine(kind)
	if err != nil {
		writeEngineErr(w, err)
		return nil
	}
	return eng
}

func (s *Server) handleListDBCatalogs(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	dbs, err := eng.ListDatabases()
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	if dbs == nil {
		dbs = []database.DatabaseInfo{}
	}
	writeJSON(w, map[string]any{"databases": dbs})
}

func (s *Server) handleCreateDBCatalog(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := eng.CreateDatabase(strings.TrimSpace(body.Name)); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDropDBCatalog(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	name := r.PathValue("name")
	if err := eng.DropDatabase(name); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func queryDB(r *http.Request) (database, schema, table string) {
	q := r.URL.Query()
	return q.Get("database"), q.Get("schema"), q.Get("table")
}

func (s *Server) handleListDBTables(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	dbName, _, _ := queryDB(r)
	if dbName == "" {
		writeError(w, "database is required", http.StatusBadRequest)
		return
	}
	tables, err := eng.ListTables(dbName)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	if tables == nil {
		tables = []database.TableInfo{}
	}
	writeJSON(w, map[string]any{"tables": tables})
}

func (s *Server) handleDBStructure(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	dbName, schema, table := queryDB(r)
	if dbName == "" || table == "" {
		writeError(w, "database and table are required", http.StatusBadRequest)
		return
	}
	st, err := eng.Structure(dbName, schema, table)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, st)
}

func (s *Server) handleDBRows(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	q := r.URL.Query()
	dbName, schema, table := queryDB(r)
	if dbName == "" || table == "" {
		writeError(w, "database and table are required", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	res, err := eng.Rows(database.RowsQuery{
		Database: dbName,
		Schema:   schema,
		Table:    table,
		Limit:    limit,
		Offset:   offset,
		Sort:     q.Get("sort"),
		Dir:      q.Get("dir"),
		Where:    q.Get("where"),
	})
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleInsertDBRow(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string         `json:"database"`
		Schema   string         `json:"schema"`
		Table    string         `json:"table"`
		Values   map[string]any `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.Database == "" || body.Table == "" {
		writeError(w, "database and table are required", http.StatusBadRequest)
		return
	}
	if err := eng.InsertRow(body.Database, body.Schema, body.Table, body.Values); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleUpdateDBRow(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string         `json:"database"`
		Schema   string         `json:"schema"`
		Table    string         `json:"table"`
		Key      map[string]any `json:"key"`
		Values   map[string]any `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := eng.UpdateRow(body.Database, body.Schema, body.Table, body.Key, body.Values); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteDBRows(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string           `json:"database"`
		Schema   string           `json:"schema"`
		Table    string           `json:"table"`
		Keys     []map[string]any `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := eng.DeleteRows(body.Database, body.Schema, body.Table, body.Keys); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleCreateDBTable(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string               `json:"database"`
		Schema   string               `json:"schema"`
		Name     string               `json:"name"`
		Columns  []database.ColumnDef `json:"columns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.Database == "" || body.Name == "" {
		writeError(w, "database and name are required", http.StatusBadRequest)
		return
	}
	if err := eng.CreateTable(body.Database, body.Schema, body.Name, body.Columns); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDropDBTable(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
		Table    string `json:"table"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := eng.DropTable(body.Database, body.Schema, body.Table); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleTruncateDBTable(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
		Table    string `json:"table"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := eng.TruncateTable(body.Database, body.Schema, body.Table); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDBQuery(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string `json:"database"`
		SQL      string `json:"sql"`
		Limit    int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	res, err := eng.ExecQuery(body.Database, body.SQL, body.Limit)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleRenameDBCatalog(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.From == "" || body.To == "" {
		writeError(w, "from and to are required", http.StatusBadRequest)
		return
	}
	if err := eng.RenameDatabase(body.From, body.To); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDuplicateDBCatalog(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.From == "" || body.To == "" {
		writeError(w, "from and to are required", http.StatusBadRequest)
		return
	}
	if err := eng.DuplicateDatabase(body.From, body.To); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleRenameDBTable(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
		From     string `json:"from"`
		To       string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Database == "" || body.From == "" || body.To == "" {
		writeError(w, "database, from and to are required", http.StatusBadRequest)
		return
	}
	if err := eng.RenameTable(body.Database, body.Schema, body.From, body.To); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDuplicateDBTable(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
		From     string `json:"from"`
		To       string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Database == "" || body.From == "" || body.To == "" {
		writeError(w, "database, from and to are required", http.StatusBadRequest)
		return
	}
	if err := eng.DuplicateTable(body.Database, body.Schema, body.From, body.To); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleAddDBColumn(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string              `json:"database"`
		Schema   string              `json:"schema"`
		Table    string              `json:"table"`
		Column   database.ColumnDef  `json:"column"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := eng.AddColumn(body.Database, body.Schema, body.Table, body.Column); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDropDBColumn(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
		Table    string `json:"table"`
		Column   string `json:"column"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := eng.DropColumn(body.Database, body.Schema, body.Table, body.Column); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleAlterDBColumn(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string             `json:"database"`
		Schema   string             `json:"schema"`
		Table    string             `json:"table"`
		Column   string             `json:"column"`
		Next     database.ColumnDef `json:"next"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := eng.AlterColumn(body.Database, body.Schema, body.Table, body.Column, body.Next); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleRenameDBColumn(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
		Table    string `json:"table"`
		From     string `json:"from"`
		To       string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := eng.RenameColumn(body.Database, body.Schema, body.Table, body.From, body.To); err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleExportDB(w http.ResponseWriter, r *http.Request) {
	eng := s.withEngine(w, r)
	if eng == nil {
		return
	}
	var body struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
		Table    string `json:"table"`
		Format   string `json:"format"`
		Where    string `json:"where"`
		Sort     string `json:"sort"`
		Dir      string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.Database == "" || body.Table == "" {
		writeError(w, "database and table are required", http.StatusBadRequest)
		return
	}
	res, err := eng.Export(database.RowsQuery{
		Database: body.Database,
		Schema:   body.Schema,
		Table:    body.Table,
		Where:    body.Where,
		Sort:     body.Sort,
		Dir:      body.Dir,
	}, body.Format)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	w.Header().Set("Content-Type", res.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+res.Filename+`"`)
	w.Write(res.Body) //nolint:errcheck
}

func readKVFile(path string) map[string]string {
	m := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		m[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+1:])
	}
	return m
}

func coalesceKV(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
