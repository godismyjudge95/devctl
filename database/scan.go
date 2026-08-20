package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const maxCellRunes = 8192

func normalizeValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		s := string(t)
		if json.Valid(t) && (len(t) > 0 && (t[0] == '{' || t[0] == '[')) {
			return json.RawMessage(append([]byte(nil), t...))
		}
		return truncateRunes(s)
	case time.Time:
		if t.IsZero() {
			return nil
		}
		return t.UTC().Format(time.RFC3339Nano)
	case int64, int32, int, float64, float32, bool:
		return t
	case string:
		return truncateRunes(t)
	default:
		return truncateRunes(fmt.Sprint(t))
	}
}

func truncateRunes(s string) string {
	n := 0
	for i := range s {
		if n == maxCellRunes {
			return s[:i] + "…"
		}
		n++
	}
	return s
}

func scanRows(rows *sql.Rows, limit int) (cols []Column, data [][]any, err error) {
	names, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	types, _ := rows.ColumnTypes()
	cols = make([]Column, len(names))
	for i, n := range names {
		c := Column{Name: n, Nullable: true}
		if types != nil && i < len(types) && types[i] != nil {
			c.Type = types[i].DatabaseTypeName()
			if nullable, ok := types[i].Nullable(); ok {
				c.Nullable = nullable
			}
		}
		cols[i] = c
	}

	for rows.Next() {
		if limit > 0 && len(data) >= limit {
			break
		}
		raw := make([]any, len(names))
		ptrs := make([]any, len(names))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make([]any, len(names))
		for i, v := range raw {
			row[i] = normalizeValue(v)
		}
		data = append(data, row)
	}
	if data == nil {
		data = [][]any{}
	}
	return cols, data, rows.Err()
}

func placeholders(dollar bool, n, start int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ", "
		}
		if dollar {
			out += "$" + strconv.Itoa(start+i)
		} else {
			out += "?"
		}
	}
	return out
}

func argsFromMap(cols []string, values map[string]any) []any {
	args := make([]any, 0, len(cols))
	for _, c := range cols {
		args = append(args, jsonToSQL(values[c]))
	}
	return args
}

func jsonToSQL(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, _ := t.Float64()
		return f
	case float64:
		// JSON numbers decode as float64. Prefer int when integral.
		if t == float64(int64(t)) {
			return int64(t)
		}
		return t
	default:
		return t
	}
}

func clampLimit(limit, fallback, max int) int {
	if limit <= 0 {
		return fallback
	}
	if limit > max {
		return max
	}
	return limit
}

func defaultTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return 30 * time.Second
	}
	return d
}
