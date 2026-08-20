package database

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
)

const maxExportRows = 50_000

func FormatExport(format, table string, kind string, cols []Column, data [][]any) (*ExportResult, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "csv"
	}
	safe := table
	if safe == "" {
		safe = "export"
	}
	switch format {
	case "csv":
		body, err := exportCSV(cols, data)
		if err != nil {
			return nil, err
		}
		return &ExportResult{Filename: safe + ".csv", ContentType: "text/csv; charset=utf-8", Body: body}, nil
	case "json":
		body, err := exportJSON(cols, data)
		if err != nil {
			return nil, err
		}
		return &ExportResult{Filename: safe + ".json", ContentType: "application/json", Body: body}, nil
	case "sql":
		body := exportSQL(kind, table, cols, data)
		return &ExportResult{Filename: safe + ".sql", ContentType: "application/sql; charset=utf-8", Body: body}, nil
	case "md", "markdown":
		body := exportMarkdown(cols, data)
		return &ExportResult{Filename: safe + ".md", ContentType: "text/markdown; charset=utf-8", Body: body}, nil
	default:
		return nil, fmt.Errorf("unsupported export format %q (csv, json, sql, markdown)", format)
	}
}

func (e *sqlEngine) Export(q RowsQuery, format string) (*ExportResult, error) {
	q.Limit = maxExportRows
	q.Offset = 0
	res, err := e.Rows(q)
	if err != nil {
		return nil, err
	}
	name := q.Table
	if q.Schema != "" {
		name = q.Schema + "_" + q.Table
	}
	return FormatExport(format, name, e.kind, res.Columns, res.Rows)
}

func (e *clickhouseEngine) Export(q RowsQuery, format string) (*ExportResult, error) {
	q.Limit = maxExportRows
	q.Offset = 0
	res, err := e.Rows(q)
	if err != nil {
		return nil, err
	}
	return FormatExport(format, q.Table, "clickhouse", res.Columns, res.Rows)
}

func exportCSV(cols []Column, data [][]any) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.Name
	}
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, row := range data {
		rec := make([]string, len(cols))
		for i := range cols {
			var v any
			if i < len(row) {
				v = row[i]
			}
			if v == nil {
				rec[i] = ""
			} else {
				rec[i] = fmt.Sprint(v)
			}
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func exportJSON(cols []Column, data [][]any) ([]byte, error) {
	out := make([]map[string]any, 0, len(data))
	for _, row := range data {
		m := map[string]any{}
		for i, c := range cols {
			var v any
			if i < len(row) {
				v = row[i]
			}
			m[c.Name] = v
		}
		out = append(out, m)
	}
	return json.MarshalIndent(out, "", "  ")
}

func exportSQL(kind, table string, cols []Column, data [][]any) []byte {
	var b strings.Builder
	if table == "" {
		table = "exported"
	}
	ref := quoteIdent(kind, table)
	quotedCols := make([]string, len(cols))
	for i, c := range cols {
		quotedCols[i] = quoteIdent(kind, c.Name)
	}
	colList := strings.Join(quotedCols, ", ")
	for _, row := range data {
		b.WriteString("INSERT INTO ")
		b.WriteString(ref)
		b.WriteString(" (")
		b.WriteString(colList)
		b.WriteString(") VALUES (")
		for i := range cols {
			if i > 0 {
				b.WriteString(", ")
			}
			var v any
			if i < len(row) {
				v = row[i]
			}
			b.WriteString(sqlLiteral(kind, v))
		}
		b.WriteString(");\n")
	}
	return []byte(b.String())
}

func exportMarkdown(cols []Column, data [][]any) []byte {
	var b strings.Builder
	b.WriteString("|")
	for _, c := range cols {
		fmt.Fprintf(&b, " %s |", c.Name)
	}
	b.WriteString("\n|")
	for range cols {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, row := range data {
		b.WriteString("|")
		for i := range cols {
			var v any
			if i < len(row) {
				v = row[i]
			}
			s := ""
			if v != nil {
				s = strings.ReplaceAll(fmt.Sprint(v), "|", "\\|")
			}
			fmt.Fprintf(&b, " %s |", s)
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

func sqlLiteral(kind string, v any) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case bool:
		if t {
			return "TRUE"
		}
		return "FALSE"
	case int, int32, int64, float32, float64, json.Number:
		return fmt.Sprint(t)
	default:
		s := fmt.Sprint(t)
		if kind == "mysql" || kind == "clickhouse" || kind == "sqlite" {
			return "'" + strings.ReplaceAll(s, "'", "''") + "'"
		}
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
}
