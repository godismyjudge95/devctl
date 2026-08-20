package database

import (
	"context"
	"time"
)

// Capabilities describes what the UI may offer for an engine.
type Capabilities struct {
	CreateDatabase bool `json:"create_database"`
	DropDatabase   bool `json:"drop_database"`
	CreateTable    bool `json:"create_table"`
	DropTable      bool `json:"drop_table"`
	Truncate           bool `json:"truncate"`
	RowEdit            bool `json:"row_edit"`
	RowInsert          bool `json:"row_insert"`
	Schemas            bool `json:"schemas"`
	RenameDatabase     bool `json:"rename_database"`
	DuplicateDatabase  bool `json:"duplicate_database"`
	RenameTable        bool `json:"rename_table"`
	DuplicateTable     bool `json:"duplicate_table"`
	AlterTable         bool `json:"alter_table"`
}

// EngineInfo is a connection the dashboard can open.
type EngineInfo struct {
	ID           string       `json:"id"`
	Label        string       `json:"label"`
	Kind         string       `json:"kind"`
	Installed    bool         `json:"installed"`
	Running      bool         `json:"running"`
	Host         string       `json:"host,omitempty"`
	Port         string       `json:"port,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
}

// DatabaseInfo is one catalog / schema-container.
type DatabaseInfo struct {
	Name   string `json:"name"`
	System bool   `json:"system"`
}

// TableInfo is a table or view.
type TableInfo struct {
	Name     string `json:"name"`
	Schema   string `json:"schema,omitempty"`
	Type     string `json:"type"` // table | view
	Rows     *int64 `json:"rows,omitempty"`
	Engine   string `json:"engine,omitempty"`
	Comment  string `json:"comment,omitempty"`
	Collation string `json:"collation,omitempty"`
	Internal  bool   `json:"internal"`
}

// Column describes a table column.
type Column struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Nullable     bool    `json:"nullable"`
	Default      *string `json:"default"`
	Key          string  `json:"key,omitempty"`
	Extra        string  `json:"extra,omitempty"`
	Comment      string  `json:"comment,omitempty"`
	PrimaryKey   bool    `json:"primary_key"`
	AutoIncrement bool   `json:"auto_increment"`
}

// Index describes a table index.
type Index struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Primary bool     `json:"primary"`
	Columns []string `json:"columns"`
	Type    string   `json:"type,omitempty"`
}

// TableStructure is the Data / Structure tab payload.
type TableStructure struct {
	Columns  []Column `json:"columns"`
	Indexes  []Index  `json:"indexes"`
	CreateSQL string  `json:"create_sql,omitempty"`
	PrimaryKey []string `json:"primary_key"`
}

// ColumnDef is the input for CREATE TABLE.
type ColumnDef struct {
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Nullable      bool    `json:"nullable"`
	Default       *string `json:"default"`
	PrimaryKey    bool    `json:"primary_key"`
	Unique        bool    `json:"unique"`
	AutoIncrement bool    `json:"auto_increment"`
}

// RowsQuery is a paginated table read.
type RowsQuery struct {
	Database string
	Schema   string
	Table    string
	Limit    int
	Offset   int
	Sort     string
	Dir      string
	Where    string
}

// RowsResult is a data-grid page.
type RowsResult struct {
	Columns    []Column `json:"columns"`
	Rows       [][]any  `json:"rows"`
	Total      int64    `json:"total"`
	Estimated  bool     `json:"estimated"`
	Limit      int      `json:"limit"`
	Offset     int      `json:"offset"`
	PrimaryKey []string `json:"primary_key"`
	DurationMS int64    `json:"duration_ms"`
}

// QueryResult is a freeform SQL result.
type QueryResult struct {
	Columns      []Column `json:"columns"`
	Rows         [][]any  `json:"rows"`
	RowsAffected int64    `json:"rows_affected"`
	Limited      bool     `json:"limited"`
	DurationMS   int64    `json:"duration_ms"`
	Statement    string   `json:"statement"`
}

// Config is how an engine is opened.
type Config struct {
	Kind     string
	Host     string
	Port     string
	User     string
	Password string
	Socket   string
	Path      string // sqlite file or sqlite files root
	Files     []SQLiteFile
	SQLiteDir string
	Timeout   time.Duration
}

// SQLiteFile is one discovered sqlite database.
type SQLiteFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Engine talks to one database server (or sqlite collection).
type Engine interface {
	Kind() string
	Ping(ctx context.Context) error
	Caps() Capabilities
	ListDatabases() ([]DatabaseInfo, error)
	CreateDatabase(name string) error
	DropDatabase(name string) error
	ListTables(database string) ([]TableInfo, error)
	Structure(database, schema, table string) (*TableStructure, error)
	Rows(q RowsQuery) (*RowsResult, error)
	InsertRow(database, schema, table string, values map[string]any) error
	UpdateRow(database, schema, table string, key, values map[string]any) error
	DeleteRows(database, schema, table string, keys []map[string]any) error
	CreateTable(database, schema, table string, cols []ColumnDef) error
	DropTable(database, schema, table string) error
	TruncateTable(database, schema, table string) error
	RenameDatabase(oldName, newName string) error
	DuplicateDatabase(src, dst string) error
	RenameTable(database, schema, oldName, newName string) error
	DuplicateTable(database, schema, src, dst string) error
	AddColumn(database, schema, table string, col ColumnDef) error
	DropColumn(database, schema, table, column string) error
	AlterColumn(database, schema, table, column string, col ColumnDef) error
	RenameColumn(database, schema, table, oldName, newName string) error
	Export(q RowsQuery, format string) (*ExportResult, error)
	ExecQuery(database, sql string, limit int) (*QueryResult, error)
	Close() error
}

// ExportResult is a downloadable dump of table (or filtered) data.
type ExportResult struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"-"`
}
