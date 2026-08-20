package database

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type clickhouseEngine struct {
	base     string
	user     string
	password string
	client   *http.Client
}

func openClickHouse(cfg Config) (Engine, error) {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Port
	if port == "" {
		port = "8123"
	}
	e := &clickhouseEngine{
		base:     "http://" + host + ":" + port,
		user:     cfg.User,
		password: cfg.Password,
		client:   &http.Client{Timeout: defaultTimeout(cfg.Timeout)},
	}
	if e.user == "" {
		e.user = "default"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Ping(ctx); err != nil {
		return nil, fmt.Errorf("clickhouse: %w", err)
	}
	return e, nil
}

func (e *clickhouseEngine) Kind() string          { return "clickhouse" }
func (e *clickhouseEngine) Caps() Capabilities    { return clickhouseCaps() }
func (e *clickhouseEngine) Close() error          { return nil }

func (e *clickhouseEngine) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.base+"/ping", nil)
	if err != nil {
		return err
	}
	e.auth(req)
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("ping: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func (e *clickhouseEngine) auth(req *http.Request) {
	if e.user != "" {
		req.SetBasicAuth(e.user, e.password)
	}
}

type chJSON struct {
	Meta []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"meta"`
	Data         []map[string]any `json:"data"`
	Rows         int              `json:"rows"`
	RowsBeforeLimit int           `json:"rows_before_limit_at_least"`
	Statistics   struct {
		Elapsed float64 `json:"elapsed"`
	} `json:"statistics"`
}

func (e *clickhouseEngine) queryJSON(ctx context.Context, database, query string) (*chJSON, error) {
	u, err := url.Parse(e.base + "/")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if database != "" {
		q.Set("database", database)
	}
	q.Set("default_format", "JSON")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	e.auth(req)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	var out chJSON
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func (e *clickhouseEngine) exec(ctx context.Context, database, query string) error {
	_, err := e.queryJSON(ctx, database, query)
	if err != nil {
		// DDL/DML may return empty body; retry without JSON decode on empty.
		u, _ := url.Parse(e.base + "/")
		q := u.Query()
		if database != "" {
			q.Set("database", database)
		}
		u.RawQuery = q.Encode()
		req, err2 := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(query))
		if err2 != nil {
			return err
		}
		e.auth(req)
		resp, err2 := e.client.Do(req)
		if err2 != nil {
			return err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode != 200 {
			return fmt.Errorf("%s", strings.TrimSpace(string(body)))
		}
		return nil
	}
	return nil
}

func (e *clickhouseEngine) ListDatabases() ([]DatabaseInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := e.queryJSON(ctx, "", "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	var out []DatabaseInfo
	for _, row := range res.Data {
		name := chString(row, "name")
		if name == "" {
			for _, v := range row {
				name = fmt.Sprint(v)
				break
			}
		}
		out = append(out, DatabaseInfo{Name: name, System: isSystemDatabase("clickhouse", name)})
	}
	return out, nil
}

func (e *clickhouseEngine) CreateDatabase(name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, "", "CREATE DATABASE "+quoteCH(name))
}

func (e *clickhouseEngine) DropDatabase(name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	if isSystemDatabase("clickhouse", name) {
		return fmt.Errorf("refusing to drop system database %q", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, "", "DROP DATABASE "+quoteCH(name))
}

func (e *clickhouseEngine) ListTables(database string) ([]TableInfo, error) {
	if err := validateIdent(database); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := e.queryJSON(ctx, database, fmt.Sprintf(
		`SELECT name, engine, total_rows FROM system.tables WHERE database = %s ORDER BY name`, chQuoteLit(database)))
	if err != nil {
		return nil, err
	}
	var out []TableInfo
	for _, row := range res.Data {
		name := chString(row, "name")
		info := TableInfo{
			Name:     name,
			Type:     "table",
			Engine:   chString(row, "engine"),
			Internal: isInternalTable("", name),
		}
		if n, ok := chInt(row, "total_rows"); ok {
			info.Rows = &n
		}
		out = append(out, info)
	}
	sortTables(out)
	return out, nil
}

func (e *clickhouseEngine) Structure(database, schema, table string) (*TableStructure, error) {
	if err := validateIdents(database, table); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := e.queryJSON(ctx, database, fmt.Sprintf(
		`SELECT name, type, default_kind, default_expression, comment, is_in_primary_key
		 FROM system.columns WHERE database = %s AND table = %s ORDER BY position`,
		chQuoteLit(database), chQuoteLit(table)))
	if err != nil {
		return nil, err
	}
	st := &TableStructure{Columns: []Column{}, Indexes: []Index{}}
	for _, row := range res.Data {
		col := Column{
			Name:       chString(row, "name"),
			Type:       chString(row, "type"),
			Nullable:   strings.Contains(strings.ToLower(chString(row, "type")), "nullable"),
			Comment:    chString(row, "comment"),
			PrimaryKey: chString(row, "is_in_primary_key") == "1" || chString(row, "is_in_primary_key") == "true",
		}
		if d := chString(row, "default_expression"); d != "" {
			col.Default = &d
			col.Extra = chString(row, "default_kind")
		}
		if col.PrimaryKey {
			st.PrimaryKey = append(st.PrimaryKey, col.Name)
		}
		st.Columns = append(st.Columns, col)
	}
	create, _ := e.queryJSON(ctx, database, "SHOW CREATE TABLE "+qualify("clickhouse", database, table))
	if create != nil && len(create.Data) > 0 {
		for _, v := range create.Data[0] {
			st.CreateSQL = fmt.Sprint(v)
			break
		}
	}
	return st, nil
}

func (e *clickhouseEngine) Rows(q RowsQuery) (*RowsResult, error) {
	if err := validateIdents(q.Database, q.Table); err != nil {
		return nil, err
	}
	if q.Where != "" && strings.Contains(q.Where, ";") {
		return nil, fmt.Errorf("WHERE clause must not contain semicolons")
	}
	if q.Sort != "" {
		if err := validateIdent(q.Sort); err != nil {
			return nil, err
		}
	}
	limit := clampLimit(q.Limit, 100, 1000)
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := e.Structure(q.Database, q.Schema, q.Table)
	if err != nil {
		return nil, err
	}

	ref := qualify("clickhouse", q.Database, q.Table)
	where := ""
	if strings.TrimSpace(q.Where) != "" {
		where = " WHERE " + q.Where
	}
	order := ""
	if q.Sort != "" {
		order = " ORDER BY " + quoteCH(q.Sort) + " " + sanitizeDir(q.Dir)
	}

	countRes, err := e.queryJSON(ctx, q.Database, "SELECT count() AS c FROM "+ref+where)
	var total int64
	if err == nil && len(countRes.Data) > 0 {
		if n, ok := chInt(countRes.Data[0], "c"); ok {
			total = n
		}
	}

	start := time.Now()
	sql := fmt.Sprintf("SELECT * FROM %s%s%s LIMIT %d OFFSET %d", ref, where, order, limit, offset)
	res, err := e.queryJSON(ctx, q.Database, sql)
	if err != nil {
		return nil, err
	}
	cols, data := chToRows(res, st)
	return &RowsResult{
		Columns:    cols,
		Rows:       data,
		Total:      total,
		Limit:      limit,
		Offset:     offset,
		PrimaryKey: st.PrimaryKey,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

func (e *clickhouseEngine) InsertRow(database, schema, table string, values map[string]any) error {
	if err := validateIdents(database, table); err != nil {
		return err
	}
	if len(values) == 0 {
		return fmt.Errorf("no values")
	}
	cols := sortedKeys(values)
	quoted := make([]string, len(cols))
	vals := make([]string, len(cols))
	for i, c := range cols {
		if err := validateIdent(c); err != nil {
			return err
		}
		quoted[i] = quoteCH(c)
		vals[i] = chLiteral(values[c])
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		qualify("clickhouse", database, table),
		strings.Join(quoted, ", "),
		strings.Join(vals, ", "),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, database, q)
}

func (e *clickhouseEngine) UpdateRow(database, schema, table string, key, values map[string]any) error {
	return fmt.Errorf("ClickHouse does not support in-place row edits")
}

func (e *clickhouseEngine) DeleteRows(database, schema, table string, keys []map[string]any) error {
	return fmt.Errorf("ClickHouse does not support deleting individual rows from the grid")
}

func (e *clickhouseEngine) CreateTable(database, schema, table string, cols []ColumnDef) error {
	return fmt.Errorf("create table from the grid is not supported for ClickHouse — run DDL in the query editor")
}

func (e *clickhouseEngine) DropTable(database, schema, table string) error {
	if err := validateIdents(database, table); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, database, "DROP TABLE "+qualify("clickhouse", database, table))
}

func (e *clickhouseEngine) TruncateTable(database, schema, table string) error {
	if err := validateIdents(database, table); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, database, "TRUNCATE TABLE "+qualify("clickhouse", database, table))
}

func (e *clickhouseEngine) RenameDatabase(oldName, newName string) error {
	if err := validateIdents(oldName, newName); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return e.exec(ctx, "", "RENAME DATABASE "+quoteCH(oldName)+" TO "+quoteCH(newName))
}

func (e *clickhouseEngine) DuplicateDatabase(src, dst string) error {
	if err := validateIdents(src, dst); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := e.CreateDatabase(dst); err != nil {
		return err
	}
	tables, err := e.ListTables(src)
	if err != nil {
		return err
	}
	for _, t := range tables {
		q := fmt.Sprintf("CREATE TABLE %s AS %s", qualify("clickhouse", dst, t.Name), qualify("clickhouse", src, t.Name))
		if err := e.exec(ctx, dst, q); err != nil {
			return err
		}
	}
	return nil
}

func (e *clickhouseEngine) RenameTable(database, schema, oldName, newName string) error {
	if err := validateIdents(database, oldName, newName); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, database, "RENAME TABLE "+qualify("clickhouse", database, oldName)+" TO "+qualify("clickhouse", database, newName))
}

func (e *clickhouseEngine) DuplicateTable(database, schema, src, dst string) error {
	if err := validateIdents(database, src, dst); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return e.exec(ctx, database, "CREATE TABLE "+qualify("clickhouse", database, dst)+" AS "+qualify("clickhouse", database, src))
}

func (e *clickhouseEngine) AddColumn(database, schema, table string, col ColumnDef) error {
	if err := validateIdents(database, table, col.Name); err != nil {
		return err
	}
	typ := strings.TrimSpace(col.Type)
	if typ == "" {
		typ = "String"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, database, "ALTER TABLE "+qualify("clickhouse", database, table)+" ADD COLUMN "+quoteCH(col.Name)+" "+typ)
}

func (e *clickhouseEngine) DropColumn(database, schema, table, column string) error {
	if err := validateIdents(database, table, column); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, database, "ALTER TABLE "+qualify("clickhouse", database, table)+" DROP COLUMN "+quoteCH(column))
}

func (e *clickhouseEngine) RenameColumn(database, schema, table, oldName, newName string) error {
	if err := validateIdents(database, table, oldName, newName); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, database, "ALTER TABLE "+qualify("clickhouse", database, table)+" RENAME COLUMN "+quoteCH(oldName)+" TO "+quoteCH(newName))
}

func (e *clickhouseEngine) AlterColumn(database, schema, table, column string, col ColumnDef) error {
	if col.Name != "" && col.Name != column {
		if err := e.RenameColumn(database, schema, table, column, col.Name); err != nil {
			return err
		}
		column = col.Name
	}
	typ := strings.TrimSpace(col.Type)
	if typ == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return e.exec(ctx, database, "ALTER TABLE "+qualify("clickhouse", database, table)+" MODIFY COLUMN "+quoteCH(column)+" "+typ)
}

func (e *clickhouseEngine) ExecQuery(database, query string, limit int) (*QueryResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty query")
	}
	limit = clampLimit(limit, 500, 5000)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	kw := firstSQLKeyword(query)
	start := time.Now()
	out := &QueryResult{Statement: kw, Columns: []Column{}, Rows: [][]any{}}
	if isResultKeyword(kw) {
		q := query
		if !strings.Contains(strings.ToUpper(query), " LIMIT ") && (kw == "SELECT" || kw == "WITH") {
			q = strings.TrimRight(query, ";") + fmt.Sprintf(" LIMIT %d", limit+1)
		}
		res, err := e.queryJSON(ctx, database, q)
		if err != nil {
			return nil, err
		}
		cols, data := chToRows(res, nil)
		if len(data) > limit {
			out.Limited = true
			data = data[:limit]
		}
		out.Columns = cols
		out.Rows = data
		out.DurationMS = time.Since(start).Milliseconds()
		return out, nil
	}
	if err := e.exec(ctx, database, query); err != nil {
		return nil, err
	}
	out.DurationMS = time.Since(start).Milliseconds()
	return out, nil
}

func chToRows(res *chJSON, st *TableStructure) ([]Column, [][]any) {
	cols := make([]Column, 0, len(res.Meta))
	for _, m := range res.Meta {
		cols = append(cols, Column{Name: m.Name, Type: m.Type, Nullable: strings.Contains(strings.ToLower(m.Type), "nullable")})
	}
	if st != nil && len(st.Columns) == len(cols) {
		cols = st.Columns
	}
	data := make([][]any, 0, len(res.Data))
	for _, row := range res.Data {
		vals := make([]any, len(res.Meta))
		for i, m := range res.Meta {
			vals[i] = normalizeValue(row[m.Name])
		}
		data = append(data, vals)
	}
	if data == nil {
		data = [][]any{}
	}
	return cols, data
}

func chString(row map[string]any, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func chInt(row map[string]any, key string) (int64, bool) {
	v, ok := row[key]
	if !ok || v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case json.Number:
		n, err := t.Int64()
		return n, err == nil
	case string:
		var n int64
		_, err := fmt.Sscan(t, &n)
		return n, err == nil
	default:
		var n int64
		_, err := fmt.Sscan(fmt.Sprint(t), &n)
		return n, err == nil
	}
}

func chQuoteLit(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
}

func chLiteral(v any) string {
	v = jsonToSQL(v)
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case int64, int, float64, bool:
		return fmt.Sprint(t)
	default:
		s := fmt.Sprint(t)
		return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
	}
}
