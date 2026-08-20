package database

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxIdentLen = 64

// quoteMySQL wraps a MySQL identifier in backticks, doubling any internal backticks.
func quoteMySQL(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// quotePG wraps a PostgreSQL/SQLite identifier in double quotes, doubling internals.
func quotePG(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteCH wraps a ClickHouse identifier in backticks.
func quoteCH(name string) string {
	return quoteMySQL(name)
}

func quoteIdent(kind, name string) string {
	switch kind {
	case "postgres", "sqlite":
		return quotePG(name)
	default:
		return quoteMySQL(name)
	}
}

func qualify(kind string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, quoteIdent(kind, p))
	}
	return strings.Join(out, ".")
}

// validateIdent rejects empty, oversized, or non-utf8 identifiers.
// SQL injection is prevented by quoting, not by restricting charset — database
// names may legally contain almost anything except NUL.
func validateIdent(name string) error {
	if name == "" {
		return fmt.Errorf("identifier is empty")
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("identifier is not valid UTF-8")
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("identifier contains NUL")
	}
	if utf8.RuneCountInString(name) > 255 {
		return fmt.Errorf("identifier is too long")
	}
	return nil
}

func validateIdents(names ...string) error {
	for _, n := range names {
		if n == "" {
			continue
		}
		if err := validateIdent(n); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeDir(dir string) string {
	d := strings.ToUpper(strings.TrimSpace(dir))
	if d == "DESC" {
		return "DESC"
	}
	return "ASC"
}

func firstSQLKeyword(sql string) string {
	s := strings.TrimSpace(sql)
	if s == "" {
		return ""
	}
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			break
		}
		i += size
	}
	j := i
	for j < len(s) {
		r, size := utf8.DecodeRuneInString(s[j:])
		if unicode.IsSpace(r) || r == ';' || r == '(' {
			break
		}
		j += size
	}
	return strings.ToUpper(s[i:j])
}

func isResultKeyword(kw string) bool {
	switch kw {
	case "SELECT", "SHOW", "EXPLAIN", "DESCRIBE", "DESC", "WITH", "VALUES",
		"PRAGMA", "TABLE", "CALL", "EXEC":
		return true
	default:
		return false
	}
}

func isInternalTable(schema, name string) bool {
	s := strings.ToLower(schema)
	n := strings.ToLower(name)
	if strings.HasPrefix(s, "_") || strings.HasPrefix(n, "_") {
		return true
	}
	if strings.Contains(s, "timescaledb") || strings.Contains(n, "timescaledb") {
		return true
	}
	switch s {
	case "cron", "tiger", "topology", "partman", "pglogical", "repack":
		return true
	}
	return false
}

func sortTables(tables []TableInfo) {
	sort.SliceStable(tables, func(i, j int) bool {
		ii := tables[i].Internal || isInternalTable(tables[i].Schema, tables[i].Name)
		ij := tables[j].Internal || isInternalTable(tables[j].Schema, tables[j].Name)
		if ii != ij {
			return !ii
		}
		if tables[i].Schema != tables[j].Schema {
			return tables[i].Schema < tables[j].Schema
		}
		return tables[i].Name < tables[j].Name
	})
}

func isSystemDatabase(kind, name string) bool {
	switch kind {
	case "mysql":
		switch name {
		case "mysql", "information_schema", "performance_schema", "sys":
			return true
		}
	case "postgres":
		switch name {
		case "postgres", "template0", "template1":
			return true
		}
	case "clickhouse":
		lower := strings.ToLower(name)
		return lower == "system" || lower == "information_schema"
	}
	return false
}
