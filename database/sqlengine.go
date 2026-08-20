package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sqlEngine struct {
	kind   string
	db     *sql.DB
	openDB func(ctx context.Context, database string) (*sql.DB, error)
	files     []SQLiteFile // sqlite only
	sqliteDir string
	owned     bool // close db on Close()
}

func (e *sqlEngine) Kind() string { return e.kind }

func (e *sqlEngine) Caps() Capabilities {
	c := Capabilities{
		CreateTable: true, DropTable: true, Truncate: true,
		RowEdit: true, RowInsert: true,
		RenameTable: true, DuplicateTable: true, AlterTable: true,
	}
	switch e.kind {
	case "postgres":
		c.CreateDatabase = true
		c.DropDatabase = true
		c.Schemas = true
		c.RenameDatabase = true
		c.DuplicateDatabase = true
	case "sqlite":
		c.CreateDatabase = true
		c.DropDatabase = true
		c.RenameDatabase = true
		c.DuplicateDatabase = true
	default:
		c.CreateDatabase = true
		c.DropDatabase = true
		c.RenameDatabase = true
		c.DuplicateDatabase = true
	}
	return c
}

func (e *sqlEngine) Ping(ctx context.Context) error {
	return e.db.PingContext(ctx)
}

func (e *sqlEngine) Close() error {
	if e.owned && e.db != nil {
		return e.db.Close()
	}
	return nil
}

func (e *sqlEngine) dollar() bool { return e.kind == "postgres" }

func (e *sqlEngine) conn(ctx context.Context, database string) (*sql.DB, error) {
	if e.kind == "postgres" && e.openDB != nil && database != "" {
		return e.openDB(ctx, database)
	}
	if e.kind == "sqlite" {
		return e.sqliteConn(ctx, database)
	}
	return e.db, nil
}

func (e *sqlEngine) sqliteConn(ctx context.Context, database string) (*sql.DB, error) {
	if database == "" && len(e.files) == 1 {
		database = e.files[0].Name
	}
	for _, f := range e.files {
		if f.Name == database {
			if e.openDB != nil {
				return e.openDB(ctx, f.Path)
			}
		}
	}
	if e.db != nil {
		return e.db, nil
	}
	return nil, fmt.Errorf("sqlite database %q not found", database)
}

func (e *sqlEngine) tableRef(database, schema, table string) string {
	switch e.kind {
	case "postgres":
		if schema == "" {
			schema = "public"
		}
		return qualify(e.kind, schema, table)
	case "sqlite":
		return qualify(e.kind, table)
	default:
		if database != "" {
			return qualify(e.kind, database, table)
		}
		return qualify(e.kind, table)
	}
}

func (e *sqlEngine) ListDatabases() ([]DatabaseInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch e.kind {
	case "sqlite":
		out := make([]DatabaseInfo, 0, len(e.files))
		for _, f := range e.files {
			out = append(out, DatabaseInfo{Name: f.Name})
		}
		return out, nil
	case "postgres":
		rows, err := e.db.QueryContext(ctx, `SELECT datname FROM pg_database WHERE datallowconn ORDER BY datname`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []DatabaseInfo
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			out = append(out, DatabaseInfo{Name: name, System: isSystemDatabase("postgres", name)})
		}
		return out, rows.Err()
	default:
		rows, err := e.db.QueryContext(ctx, `SHOW DATABASES`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []DatabaseInfo
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			out = append(out, DatabaseInfo{Name: name, System: isSystemDatabase("mysql", name)})
		}
		return out, rows.Err()
	}
}

func (e *sqlEngine) CreateDatabase(name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	if isSystemDatabase(e.kind, name) {
		return fmt.Errorf("refusing to create system database %q", name)
	}
	if e.kind == "sqlite" {
		return e.sqliteCreateDatabase(name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := "CREATE DATABASE " + quoteIdent(e.kind, name)
	if e.kind == "mysql" {
		q += " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	}
	_, err := e.db.ExecContext(ctx, q)
	return err
}

func (e *sqlEngine) sqliteCreateDatabase(name string) error {
	dir := e.sqliteDir
	if dir == "" && len(e.files) > 0 {
		dir = filepath.Dir(e.files[0].Path)
	}
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, name+".sqlite")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return err
	}
	_ = f.Close()
	e.files = append(e.files, SQLiteFile{Name: name, Path: path})
	return nil
}

func (e *sqlEngine) DropDatabase(name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	if isSystemDatabase(e.kind, name) {
		return fmt.Errorf("refusing to drop system database %q", name)
	}
	if e.kind == "sqlite" {
		kept := e.files[:0]
		for _, f := range e.files {
			if f.Name == name {
				_ = os.Remove(f.Path)
				continue
			}
			kept = append(kept, f)
		}
		e.files = kept
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := e.db.ExecContext(ctx, "DROP DATABASE "+quoteIdent(e.kind, name))
	return err
}

func (e *sqlEngine) ListTables(database string) ([]TableInfo, error) {
	if err := validateIdents(database); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return nil, err
	}

	switch e.kind {
	case "sqlite":
		rows, err := conn.QueryContext(ctx, `SELECT name, type FROM sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []TableInfo
		for rows.Next() {
			var name, typ string
			if err := rows.Scan(&name, &typ); err != nil {
				return nil, err
			}
			out = append(out, TableInfo{Name: name, Type: typ, Internal: isInternalTable("", name)})
		}
		sortTables(out)
		return out, rows.Err()
	case "postgres":
		rows, err := conn.QueryContext(ctx, `
			SELECT n.nspname, c.relname,
				CASE c.relkind WHEN 'v' THEN 'view' WHEN 'm' THEN 'view' ELSE 'table' END,
				COALESCE(c.reltuples, 0)::bigint
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind IN ('r','p','v','m')
			  AND n.nspname NOT IN ('pg_catalog','information_schema','pg_toast')
			ORDER BY n.nspname, c.relname`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []TableInfo
		for rows.Next() {
			var schema, name, typ string
			var est int64
			if err := rows.Scan(&schema, &name, &typ, &est); err != nil {
				return nil, err
			}
			n := est
			info := TableInfo{Name: name, Schema: schema, Type: typ, Internal: isInternalTable(schema, name)}
			if est >= 0 {
				info.Rows = &n
			}
			out = append(out, info)
		}
		sortTables(out)
		return out, rows.Err()
	default:
		rows, err := conn.QueryContext(ctx, `
			SELECT TABLE_NAME, TABLE_TYPE, TABLE_ROWS, ENGINE, TABLE_COMMENT, TABLE_COLLATION
			FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = ?
			ORDER BY TABLE_NAME`, database)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []TableInfo
		for rows.Next() {
			var name, typ string
			var nrows sql.NullInt64
			var engine, comment, coll sql.NullString
			if err := rows.Scan(&name, &typ, &nrows, &engine, &comment, &coll); err != nil {
				return nil, err
			}
			info := TableInfo{
				Name:      name,
				Type:      "table",
				Engine:    engine.String,
				Comment:   comment.String,
				Collation: coll.String,
				Internal:  isInternalTable("", name),
			}
			if strings.Contains(strings.ToUpper(typ), "VIEW") {
				info.Type = "view"
			}
			if nrows.Valid {
				n := nrows.Int64
				info.Rows = &n
			}
			out = append(out, info)
		}
		sortTables(out)
		return out, rows.Err()
	}
}

func (e *sqlEngine) Structure(database, schema, table string) (*TableStructure, error) {
	if err := validateIdents(database, schema, table); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return nil, err
	}

	st := &TableStructure{Columns: []Column{}, Indexes: []Index{}}
	switch e.kind {
	case "sqlite":
		if err := e.sqliteStructure(ctx, conn, table, st); err != nil {
			return nil, err
		}
	case "postgres":
		if schema == "" {
			schema = "public"
		}
		if err := e.pgStructure(ctx, conn, schema, table, st); err != nil {
			return nil, err
		}
	default:
		if err := e.mysqlStructure(ctx, conn, database, table, st); err != nil {
			return nil, err
		}
	}
	return st, nil
}

func (e *sqlEngine) sqliteStructure(ctx context.Context, conn *sql.DB, table string, st *TableStructure) error {
	rows, err := conn.QueryContext(ctx, "PRAGMA table_info("+quotePG(table)+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			return err
		}
		col := Column{
			Name:       name,
			Type:       typ,
			Nullable:   notnull == 0,
			PrimaryKey: pk > 0,
		}
		if def.Valid {
			d := def.String
			col.Default = &d
		}
		if pk > 0 {
			st.PrimaryKey = append(st.PrimaryKey, name)
			col.Key = "PRI"
		}
		if strings.EqualFold(typ, "INTEGER") && pk > 0 {
			col.AutoIncrement = true
			col.Extra = "autoincrement"
		}
		st.Columns = append(st.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	idxRows, err := conn.QueryContext(ctx, "PRAGMA index_list("+quotePG(table)+")")
	if err != nil {
		return err
	}
	defer idxRows.Close()
	type idxRow struct{ name string; unique bool; origin string }
	var idxs []idxRow
	for idxRows.Next() {
		var seq int
		var name, origin string
		var unique int
		var partial any
		if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return err
		}
		idxs = append(idxs, idxRow{name, unique == 1, origin})
	}
	for _, ix := range idxs {
		cols, err := conn.QueryContext(ctx, "PRAGMA index_info("+quotePG(ix.name)+")")
		if err != nil {
			return err
		}
		var names []string
		for cols.Next() {
			var seqno, cid int
			var n string
			if err := cols.Scan(&seqno, &cid, &n); err != nil {
				cols.Close()
				return err
			}
			names = append(names, n)
		}
		cols.Close()
		st.Indexes = append(st.Indexes, Index{
			Name:    ix.name,
			Unique:  ix.unique,
			Primary: ix.origin == "pk",
			Columns: names,
		})
	}
	var create string
	_ = conn.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name = ? AND type IN ('table','view')`, table).Scan(&create)
	st.CreateSQL = create
	return nil
}

func (e *sqlEngine) mysqlStructure(ctx context.Context, conn *sql.DB, database, table string, st *TableStructure) error {
	rows, err := conn.QueryContext(ctx, `
		SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY, EXTRA, COLUMN_COMMENT
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`, database, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, typ, nullable, key, extra, comment string
		var def sql.NullString
		if err := rows.Scan(&name, &typ, &nullable, &def, &key, &extra, &comment); err != nil {
			return err
		}
		col := Column{
			Name:          name,
			Type:          typ,
			Nullable:      strings.EqualFold(nullable, "YES"),
			Key:           key,
			Extra:         extra,
			Comment:       comment,
			PrimaryKey:    key == "PRI",
			AutoIncrement: strings.Contains(strings.ToLower(extra), "auto_increment"),
		}
		if def.Valid {
			d := def.String
			col.Default = &d
		}
		if col.PrimaryKey {
			st.PrimaryKey = append(st.PrimaryKey, name)
		}
		st.Columns = append(st.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	idxRows, err := conn.QueryContext(ctx, `
		SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME, INDEX_TYPE
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX`, database, table)
	if err != nil {
		return err
	}
	defer idxRows.Close()
	byName := map[string]*Index{}
	var order []string
	for idxRows.Next() {
		var name, col, typ string
		var nonUnique int
		if err := idxRows.Scan(&name, &nonUnique, &col, &typ); err != nil {
			return err
		}
		ix, ok := byName[name]
		if !ok {
			ix = &Index{Name: name, Unique: nonUnique == 0, Primary: name == "PRIMARY", Type: typ}
			byName[name] = ix
			order = append(order, name)
		}
		ix.Columns = append(ix.Columns, col)
	}
	for _, n := range order {
		st.Indexes = append(st.Indexes, *byName[n])
	}

	var create, dummy string
	q := "SHOW CREATE TABLE " + qualify("mysql", database, table)
	if err := conn.QueryRowContext(ctx, q).Scan(&dummy, &create); err == nil {
		st.CreateSQL = create
	}
	return nil
}

func (e *sqlEngine) pgStructure(ctx context.Context, conn *sql.DB, schema, table string, st *TableStructure) error {
	rows, err := conn.QueryContext(ctx, `
		SELECT a.attname,
		       pg_catalog.format_type(a.atttypid, a.atttypmod),
		       NOT a.attnotnull,
		       pg_get_expr(ad.adbin, ad.adrelid),
		       COALESCE(i.indisprimary, false),
		       COALESCE(i.indisunique AND i.indisprimary = false, false),
		       col_description(a.attrelid, a.attnum)
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		LEFT JOIN pg_index i ON i.indrelid = a.attrelid AND a.attnum = ANY (i.indkey) AND i.indisprimary
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, schema, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, typ string
		var nullable, pk, uniq bool
		var def, comment sql.NullString
		if err := rows.Scan(&name, &typ, &nullable, &def, &pk, &uniq, &comment); err != nil {
			return err
		}
		col := Column{
			Name:       name,
			Type:       typ,
			Nullable:   nullable,
			PrimaryKey: pk,
			Comment:    comment.String,
		}
		if def.Valid {
			d := def.String
			col.Default = &d
			if strings.Contains(strings.ToLower(d), "nextval(") {
				col.AutoIncrement = true
				col.Extra = "serial"
			}
		}
		if pk {
			col.Key = "PRI"
			st.PrimaryKey = append(st.PrimaryKey, name)
		} else if uniq {
			col.Key = "UNI"
		}
		st.Columns = append(st.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	idxRows, err := conn.QueryContext(ctx, `
		SELECT i.relname, ix.indisunique, ix.indisprimary,
		       array_agg(a.attname ORDER BY x.ordinality) 
		FROM pg_index ix
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS x(attnum, ordinality) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = x.attnum
		WHERE n.nspname = $1 AND t.relname = $2
		GROUP BY i.relname, ix.indisunique, ix.indisprimary
		ORDER BY ix.indisprimary DESC, i.relname`, schema, table)
	if err != nil {
		return err
	}
	defer idxRows.Close()
	for idxRows.Next() {
		var name string
		var unique, primary bool
		var cols string
		if err := idxRows.Scan(&name, &unique, &primary, &cols); err != nil {
			return err
		}
		st.Indexes = append(st.Indexes, Index{
			Name:    name,
			Unique:  unique,
			Primary: primary,
			Columns: parsePGTextArray(cols),
		})
	}

	var create string
	_ = conn.QueryRowContext(ctx, `SELECT 'CREATE TABLE ' || quote_ident($1) || '.' || quote_ident($2)`, schema, table).Scan(&create)
	// Prefer pg_get_tabledef-like reconstruction is heavy; keep a compact DDL from columns.
	st.CreateSQL = e.reconstructCreate(schema, table, st)
	return nil
}

func parsePGTextArray(s string) []string {
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.Trim(parts[i], `"`)
	}
	return parts
}

func (e *sqlEngine) reconstructCreate(schema, table string, st *TableStructure) string {
	var b strings.Builder
	b.WriteString("CREATE TABLE ")
	b.WriteString(e.tableRef("", schema, table))
	b.WriteString(" (\n")
	for i, c := range st.Columns {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, "  %s %s", quoteIdent(e.kind, c.Name), c.Type)
		if !c.Nullable {
			b.WriteString(" NOT NULL")
		}
		if c.Default != nil {
			fmt.Fprintf(&b, " DEFAULT %s", *c.Default)
		}
	}
	if len(st.PrimaryKey) > 0 {
		b.WriteString(",\n  PRIMARY KEY (")
		for i, n := range st.PrimaryKey {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(quoteIdent(e.kind, n))
		}
		b.WriteString(")")
	}
	b.WriteString("\n)")
	return b.String()
}

func (e *sqlEngine) Rows(q RowsQuery) (*RowsResult, error) {
	if err := validateIdents(q.Database, q.Schema, q.Table); err != nil {
		return nil, err
	}
	if q.Sort != "" {
		if err := validateIdent(q.Sort); err != nil {
			return nil, err
		}
	}
	if q.Where != "" && strings.Contains(q.Where, ";") {
		return nil, fmt.Errorf("WHERE clause must not contain semicolons")
	}
	limit := clampLimit(q.Limit, 100, 1000)
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, q.Database)
	if err != nil {
		return nil, err
	}

	st, err := e.Structure(q.Database, q.Schema, q.Table)
	if err != nil {
		return nil, err
	}
	sortOK := false
	if q.Sort != "" {
		for _, c := range st.Columns {
			if c.Name == q.Sort {
				sortOK = true
				break
			}
		}
		if !sortOK {
			return nil, fmt.Errorf("unknown sort column %q", q.Sort)
		}
	}

	ref := e.tableRef(q.Database, q.Schema, q.Table)
	where := ""
	if strings.TrimSpace(q.Where) != "" {
		where = " WHERE " + q.Where
	}
	order := ""
	if q.Sort != "" {
		order = " ORDER BY " + quoteIdent(e.kind, q.Sort) + " " + sanitizeDir(q.Dir)
	}

	countSQL := "SELECT COUNT(*) FROM " + ref + where
	var total int64
	estimated := false
	if err := conn.QueryRowContext(ctx, countSQL).Scan(&total); err != nil {
		total = 0
	}

	selectSQL := "SELECT * FROM " + ref + where + order
	if e.kind == "postgres" {
		selectSQL += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	} else {
		selectSQL += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	}

	start := time.Now()
	rows, err := conn.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, data, err := scanRows(rows, limit)
	if err != nil {
		return nil, err
	}
	// Prefer information_schema types/nullability from Structure.
	if len(st.Columns) == len(cols) {
		cols = st.Columns
	} else {
		// Match by name.
		byName := map[string]Column{}
		for _, c := range st.Columns {
			byName[c.Name] = c
		}
		for i, c := range cols {
			if full, ok := byName[c.Name]; ok {
				cols[i] = full
			}
		}
	}
	return &RowsResult{
		Columns:    cols,
		Rows:       data,
		Total:      total,
		Estimated:  estimated,
		Limit:      limit,
		Offset:     offset,
		PrimaryKey: st.PrimaryKey,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

func (e *sqlEngine) InsertRow(database, schema, table string, values map[string]any) error {
	if err := validateIdents(database, schema, table); err != nil {
		return err
	}
	if len(values) == 0 {
		return fmt.Errorf("no values")
	}
	cols := sortedKeys(values)
	for _, c := range cols {
		if err := validateIdent(c); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = quoteIdent(e.kind, c)
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		e.tableRef(database, schema, table),
		strings.Join(quoted, ", "),
		placeholders(e.dollar(), len(cols), 1),
	)
	_, err = conn.ExecContext(ctx, q, argsFromMap(cols, values)...)
	return err
}

func (e *sqlEngine) UpdateRow(database, schema, table string, key, values map[string]any) error {
	if err := validateIdents(database, schema, table); err != nil {
		return err
	}
	if len(key) == 0 {
		return fmt.Errorf("update requires a primary key")
	}
	if len(values) == 0 {
		return fmt.Errorf("no values")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	setCols := sortedKeys(values)
	keyCols := sortedKeys(key)
	for _, c := range append(append([]string{}, setCols...), keyCols...) {
		if err := validateIdent(c); err != nil {
			return err
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "UPDATE %s SET ", e.tableRef(database, schema, table))
	args := make([]any, 0, len(setCols)+len(keyCols))
	n := 1
	for i, c := range setCols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(quoteIdent(e.kind, c))
		b.WriteString(" = ")
		if e.dollar() {
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteString("?")
		}
		args = append(args, jsonToSQL(values[c]))
		n++
	}
	b.WriteString(" WHERE ")
	for i, c := range keyCols {
		if i > 0 {
			b.WriteString(" AND ")
		}
		b.WriteString(quoteIdent(e.kind, c))
		b.WriteString(" = ")
		if e.dollar() {
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteString("?")
		}
		args = append(args, jsonToSQL(key[c]))
		n++
	}
	res, err := conn.ExecContext(ctx, b.String(), args...)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("no matching row")
	}
	return nil
}

func (e *sqlEngine) DeleteRows(database, schema, table string, keys []map[string]any) error {
	if err := validateIdents(database, schema, table); err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("no keys")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	ref := e.tableRef(database, schema, table)
	for _, key := range keys {
		if len(key) == 0 {
			return fmt.Errorf("delete requires a primary key")
		}
		keyCols := sortedKeys(key)
		var b strings.Builder
		fmt.Fprintf(&b, "DELETE FROM %s WHERE ", ref)
		args := make([]any, 0, len(keyCols))
		n := 1
		for i, c := range keyCols {
			if err := validateIdent(c); err != nil {
				return err
			}
			if i > 0 {
				b.WriteString(" AND ")
			}
			b.WriteString(quoteIdent(e.kind, c))
			b.WriteString(" = ")
			if e.dollar() {
				fmt.Fprintf(&b, "$%d", n)
			} else {
				b.WriteString("?")
			}
			args = append(args, jsonToSQL(key[c]))
			n++
		}
		if _, err := conn.ExecContext(ctx, b.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func (e *sqlEngine) CreateTable(database, schema, table string, cols []ColumnDef) error {
	if err := validateIdents(database, schema, table); err != nil {
		return err
	}
	if len(cols) == 0 {
		return fmt.Errorf("table needs at least one column")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", e.tableRef(database, schema, table))
	var pks []string
	for i, c := range cols {
		if err := validateIdent(c.Name); err != nil {
			return err
		}
		if i > 0 {
			b.WriteString(",\n")
		}
		typ := strings.TrimSpace(c.Type)
		if typ == "" {
			typ = "TEXT"
		}
		if e.kind == "sqlite" && c.AutoIncrement && c.PrimaryKey {
			typ = "INTEGER"
		}
		if e.kind == "mysql" && c.AutoIncrement && !strings.Contains(strings.ToLower(typ), "int") {
			typ = "BIGINT"
		}
		fmt.Fprintf(&b, "  %s %s", quoteIdent(e.kind, c.Name), typ)
		if c.AutoIncrement {
			switch e.kind {
			case "mysql":
				b.WriteString(" AUTO_INCREMENT")
			case "postgres":
				// Prefer generated identity when type is integer-like.
				if !strings.Contains(strings.ToLower(typ), "serial") {
					b.WriteString(" GENERATED BY DEFAULT AS IDENTITY")
				}
			}
		}
		if !c.Nullable && !c.PrimaryKey {
			b.WriteString(" NOT NULL")
		}
		if c.Unique && !c.PrimaryKey {
			b.WriteString(" UNIQUE")
		}
		if c.Default != nil && *c.Default != "" && !c.AutoIncrement {
			fmt.Fprintf(&b, " DEFAULT %s", *c.Default)
		}
		if c.PrimaryKey {
			pks = append(pks, c.Name)
		}
	}
	if len(pks) > 0 {
		b.WriteString(",\n  PRIMARY KEY (")
		for i, n := range pks {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(quoteIdent(e.kind, n))
		}
		b.WriteString(")")
	}
	b.WriteString("\n)")
	if e.kind == "mysql" {
		b.WriteString(" ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
	}
	_, err = conn.ExecContext(ctx, b.String())
	return err
}

func (e *sqlEngine) DropTable(database, schema, table string) error {
	if err := validateIdents(database, schema, table); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "DROP TABLE "+e.tableRef(database, schema, table))
	return err
}

func (e *sqlEngine) TruncateTable(database, schema, table string) error {
	if err := validateIdents(database, schema, table); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	if e.kind == "sqlite" {
		_, err = conn.ExecContext(ctx, "DELETE FROM "+e.tableRef(database, schema, table))
		return err
	}
	_, err = conn.ExecContext(ctx, "TRUNCATE TABLE "+e.tableRef(database, schema, table))
	return err
}

func (e *sqlEngine) ExecQuery(database, query string, limit int) (*QueryResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty query")
	}
	limit = clampLimit(limit, 500, 5000)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	kw := firstSQLKeyword(query)
	res := &QueryResult{Statement: kw, Columns: []Column{}, Rows: [][]any{}}

	if isResultKeyword(kw) {
		q := query
		if !strings.Contains(strings.ToUpper(query), " LIMIT ") && (kw == "SELECT" || kw == "WITH" || kw == "VALUES" || kw == "TABLE") {
			q = strings.TrimRight(query, ";") + fmt.Sprintf(" LIMIT %d", limit+1)
		}
		rows, err := conn.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		cols, data, err := scanRows(rows, limit+1)
		if err != nil {
			return nil, err
		}
		if len(data) > limit {
			res.Limited = true
			data = data[:limit]
		}
		res.Columns = cols
		res.Rows = data
		res.DurationMS = time.Since(start).Milliseconds()
		return res, nil
	}

	execRes, err := conn.ExecContext(ctx, query)
	if err != nil {
		return nil, err
	}
	n, _ := execRes.RowsAffected()
	res.RowsAffected = n
	res.DurationMS = time.Since(start).Milliseconds()
	return res, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
