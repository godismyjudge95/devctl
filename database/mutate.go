package database

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (e *sqlEngine) RenameDatabase(oldName, newName string) error {
	if err := validateIdents(oldName, newName); err != nil {
		return err
	}
	if isSystemDatabase(e.kind, oldName) {
		return fmt.Errorf("refusing to rename system database %q", oldName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch e.kind {
	case "sqlite":
		return e.sqliteRenameDatabase(oldName, newName)
	case "postgres":
		_, err := e.db.ExecContext(ctx, "ALTER DATABASE "+quotePG(oldName)+" RENAME TO "+quotePG(newName))
		return err
	default:
		if err := e.DuplicateDatabase(oldName, newName); err != nil {
			return err
		}
		return e.DropDatabase(oldName)
	}
}

func (e *sqlEngine) sqliteRenameDatabase(oldName, newName string) error {
	idx := -1
	for i, f := range e.files {
		if f.Name == oldName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("sqlite database %q not found", oldName)
	}
	src := e.files[idx].Path
	dst := filepath.Join(filepath.Dir(src), newName+".sqlite")
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	e.files[idx] = SQLiteFile{Name: newName, Path: dst}
	return nil
}

func (e *sqlEngine) DuplicateDatabase(src, dst string) error {
	if err := validateIdents(src, dst); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if e.kind == "sqlite" {
		return e.sqliteDuplicateDatabase(src, dst)
	}
	if e.kind == "postgres" {
		_, err := e.db.ExecContext(ctx, "CREATE DATABASE "+quotePG(dst)+" WITH TEMPLATE "+quotePG(src))
		return err
	}
	if err := e.CreateDatabase(dst); err != nil {
		return err
	}
	tables, err := e.ListTables(src)
	if err != nil {
		return err
	}
	for _, t := range tables {
		if t.Type == "view" {
			continue
		}
		if err := e.copyTableSQL(ctx, src, dst, t); err != nil {
			return fmt.Errorf("copy %s: %w", t.Name, err)
		}
	}
	return nil
}

func (e *sqlEngine) sqliteDuplicateDatabase(src, dst string) error {
	var srcPath string
	for _, f := range e.files {
		if f.Name == src {
			srcPath = f.Path
			break
		}
	}
	if srcPath == "" {
		return fmt.Errorf("sqlite database %q not found", src)
	}
	dstPath := filepath.Join(filepath.Dir(srcPath), dst+".sqlite")
	if err := copyFile(srcPath, dstPath); err != nil {
		return err
	}
	e.files = append(e.files, SQLiteFile{Name: dst, Path: dstPath})
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func (e *sqlEngine) copyTableSQL(ctx context.Context, srcDB, dstDB string, t TableInfo) error {
	srcConn, err := e.conn(ctx, srcDB)
	if err != nil {
		return err
	}
	dstConn, err := e.conn(ctx, dstDB)
	if err != nil {
		return err
	}
	srcRef := e.tableRef(srcDB, t.Schema, t.Name)
	dstRef := e.tableRef(dstDB, t.Schema, t.Name)
	switch e.kind {
	case "mysql":
		if _, err := dstConn.ExecContext(ctx, "CREATE TABLE "+qualify("mysql", dstDB, t.Name)+" LIKE "+qualify("mysql", srcDB, t.Name)); err != nil {
			return err
		}
		_, err = dstConn.ExecContext(ctx, "INSERT INTO "+qualify("mysql", dstDB, t.Name)+" SELECT * FROM "+qualify("mysql", srcDB, t.Name))
		return err
	case "postgres":
		schema := t.Schema
		if schema == "" {
			schema = "public"
		}
		if _, err := dstConn.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+quotePG(schema)); err != nil {
			return err
		}
		if _, err := dstConn.ExecContext(ctx, "CREATE TABLE "+qualify("postgres", schema, t.Name)+" (LIKE "+qualify("postgres", schema, t.Name)+" INCLUDING ALL)"); err != nil {
			// LIKE refers to local table — for template fallback we copy DDL via AS.
			if _, err2 := dstConn.ExecContext(ctx, "CREATE TABLE "+qualify("postgres", schema, t.Name)+" AS TABLE "+srcRef+" WITH NO DATA"); err2 != nil {
				return err
			}
		}
		_, err = dstConn.ExecContext(ctx, "INSERT INTO "+dstRef+" SELECT * FROM "+srcRef)
		return err
	default:
		_, err = srcConn.ExecContext(ctx, "CREATE TABLE "+dstRef+" AS SELECT * FROM "+srcRef)
		return err
	}
}

func (e *sqlEngine) RenameTable(database, schema, oldName, newName string) error {
	if err := validateIdents(database, schema, oldName, newName); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	src := e.tableRef(database, schema, oldName)
	switch e.kind {
	case "mysql":
		_, err = conn.ExecContext(ctx, "RENAME TABLE "+src+" TO "+qualify("mysql", database, newName))
	case "postgres":
		sch := schema
		if sch == "" {
			sch = "public"
		}
		_, err = conn.ExecContext(ctx, "ALTER TABLE "+src+" RENAME TO "+quotePG(newName))
	default:
		_, err = conn.ExecContext(ctx, "ALTER TABLE "+src+" RENAME TO "+quotePG(newName))
	}
	return err
}

func (e *sqlEngine) DuplicateTable(database, schema, src, dst string) error {
	if err := validateIdents(database, schema, src, dst); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	srcRef := e.tableRef(database, schema, src)
	dstRef := e.tableRef(database, schema, dst)
	switch e.kind {
	case "mysql":
		if _, err := conn.ExecContext(ctx, "CREATE TABLE "+dstRef+" LIKE "+srcRef); err != nil {
			return err
		}
		_, err = conn.ExecContext(ctx, "INSERT INTO "+dstRef+" SELECT * FROM "+srcRef)
		return err
	case "postgres":
		if _, err := conn.ExecContext(ctx, "CREATE TABLE "+dstRef+" (LIKE "+srcRef+" INCLUDING ALL)"); err != nil {
			return err
		}
		_, err = conn.ExecContext(ctx, "INSERT INTO "+dstRef+" SELECT * FROM "+srcRef)
		return err
	default:
		_, err = conn.ExecContext(ctx, "CREATE TABLE "+dstRef+" AS SELECT * FROM "+srcRef)
		return err
	}
}

func columnDDL(kind string, col ColumnDef) string {
	typ := strings.TrimSpace(col.Type)
	if typ == "" {
		typ = "TEXT"
	}
	var b strings.Builder
	b.WriteString(quoteIdent(kind, col.Name))
	b.WriteString(" ")
	b.WriteString(typ)
	if col.AutoIncrement && kind == "mysql" {
		b.WriteString(" AUTO_INCREMENT")
	}
	if !col.Nullable {
		b.WriteString(" NOT NULL")
	}
	if col.Unique && !col.PrimaryKey {
		b.WriteString(" UNIQUE")
	}
	if col.Default != nil && *col.Default != "" && !col.AutoIncrement {
		fmt.Fprintf(&b, " DEFAULT %s", *col.Default)
	}
	return b.String()
}

func (e *sqlEngine) AddColumn(database, schema, table string, col ColumnDef) error {
	if err := validateIdents(database, schema, table, col.Name); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	q := "ALTER TABLE " + e.tableRef(database, schema, table) + " ADD COLUMN " + columnDDL(e.kind, col)
	_, err = conn.ExecContext(ctx, q)
	return err
}

func (e *sqlEngine) DropColumn(database, schema, table, column string) error {
	if err := validateIdents(database, schema, table, column); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	q := "ALTER TABLE " + e.tableRef(database, schema, table) + " DROP COLUMN " + quoteIdent(e.kind, column)
	_, err = conn.ExecContext(ctx, q)
	return err
}

func (e *sqlEngine) RenameColumn(database, schema, table, oldName, newName string) error {
	if err := validateIdents(database, schema, table, oldName, newName); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	ref := e.tableRef(database, schema, table)
	var q string
	switch e.kind {
	case "mysql":
		st, err := e.Structure(database, schema, table)
		if err != nil {
			return err
		}
		var col Column
		for _, c := range st.Columns {
			if c.Name == oldName {
				col = c
				break
			}
		}
		if col.Name == "" {
			return fmt.Errorf("column %q not found", oldName)
		}
		null := "NULL"
		if !col.Nullable {
			null = "NOT NULL"
		}
		q = fmt.Sprintf("ALTER TABLE %s CHANGE %s %s %s %s", ref, quoteMySQL(oldName), quoteMySQL(newName), col.Type, null)
	default:
		q = "ALTER TABLE " + ref + " RENAME COLUMN " + quoteIdent(e.kind, oldName) + " TO " + quoteIdent(e.kind, newName)
	}
	_, err = conn.ExecContext(ctx, q)
	return err
}

func (e *sqlEngine) AlterColumn(database, schema, table, column string, col ColumnDef) error {
	if err := validateIdents(database, schema, table, column); err != nil {
		return err
	}
	if col.Name == "" {
		col.Name = column
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := e.conn(ctx, database)
	if err != nil {
		return err
	}
	ref := e.tableRef(database, schema, table)
	if col.Name != column {
		if err := e.RenameColumn(database, schema, table, column, col.Name); err != nil {
			return err
		}
		column = col.Name
		ref = e.tableRef(database, schema, table)
	}
	switch e.kind {
	case "mysql":
		_, err = conn.ExecContext(ctx, "ALTER TABLE "+ref+" MODIFY COLUMN "+columnDDL("mysql", col))
		return err
	case "postgres":
		typ := strings.TrimSpace(col.Type)
		if typ == "" {
			typ = "TEXT"
		}
		if _, err = conn.ExecContext(ctx, "ALTER TABLE "+ref+" ALTER COLUMN "+quotePG(column)+" TYPE "+typ+" USING "+quotePG(column)+"::"+typ); err != nil {
			return err
		}
		if col.Nullable {
			_, err = conn.ExecContext(ctx, "ALTER TABLE "+ref+" ALTER COLUMN "+quotePG(column)+" DROP NOT NULL")
		} else {
			_, err = conn.ExecContext(ctx, "ALTER TABLE "+ref+" ALTER COLUMN "+quotePG(column)+" SET NOT NULL")
		}
		return err
	default:
		return fmt.Errorf("SQLite cannot change column types in place — recreate the table or run DDL in the query editor")
	}
}
