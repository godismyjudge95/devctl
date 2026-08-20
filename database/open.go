package database

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Pool caches *sql.DB handles keyed by driver+dsn.
type Pool struct {
	mu    sync.Mutex
	items map[string]*sql.DB
}

func NewPool() *Pool {
	return &Pool{items: map[string]*sql.DB{}}
}

func (p *Pool) SQL(driver, dsn string) (*sql.DB, error) {
	key := driver + "\x00" + dsn
	p.mu.Lock()
	defer p.mu.Unlock()
	if db, ok := p.items[key]; ok {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err == nil {
			return db, nil
		}
		db.Close()
		delete(p.items, key)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	p.items[key] = db
	return db, nil
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, db := range p.items {
		db.Close()
		delete(p.items, k)
	}
}

// Open returns an Engine for cfg. Pass a Pool to reuse connections.
func Open(cfg Config, pool *Pool) (Engine, error) {
	if pool == nil {
		pool = NewPool()
	}
	switch cfg.Kind {
	case "mysql":
		return openMySQL(cfg, pool)
	case "postgres":
		return openPostgres(cfg, pool)
	case "sqlite":
		return openSQLite(cfg, pool)
	case "clickhouse":
		return openClickHouse(cfg)
	default:
		return nil, fmt.Errorf("unknown engine kind %q", cfg.Kind)
	}
}

func openMySQL(cfg Config, pool *Pool) (Engine, error) {
	user := cfg.User
	if user == "" {
		user = "root"
	}
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Port
	if port == "" {
		port = "3306"
	}
	params := "/?parseTime=true&charset=utf8mb4&loc=UTC&timeout=5s&readTimeout=30s&writeTimeout=30s"
	tcpDSN := user + ":" + cfg.Password + "@tcp(" + net.JoinHostPort(host, port) + ")" + params
	dsn := tcpDSN
	if cfg.Socket != "" {
		dsn = user + ":" + cfg.Password + "@unix(" + cfg.Socket + ")" + params
	}
	db, err := pool.SQL("mysql", dsn)
	if err != nil && cfg.Socket != "" {
		db, err = pool.SQL("mysql", tcpDSN)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql: %w", err)
	}
	return &sqlEngine{kind: "mysql", db: db}, nil
}

func openPostgres(cfg Config, pool *Pool) (Engine, error) {
	user := cfg.User
	if user == "" {
		user = "root"
	}
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Port
	if port == "" {
		port = "5432"
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, cfg.Password),
		Host:   net.JoinHostPort(host, port),
		Path:   "/postgres",
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("connect_timeout", "5")
	u.RawQuery = q.Encode()
	db, err := pool.SQL("pgx", u.String())
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	openDB := func(ctx context.Context, database string) (*sql.DB, error) {
		if err := validateIdent(database); err != nil {
			return nil, err
		}
		du := u
		du.Path = "/" + database
		return pool.SQL("pgx", du.String())
	}
	return &sqlEngine{kind: "postgres", db: db, openDB: openDB}, nil
}

func openSQLite(cfg Config, pool *Pool) (Engine, error) {
	files := cfg.Files
	if len(files) == 0 && cfg.Path != "" {
		files = []SQLiteFile{{Name: sqliteName(cfg.Path), Path: cfg.Path}}
	}
	openDB := func(ctx context.Context, path string) (*sql.DB, error) {
		return pool.SQL("sqlite", path)
	}
	var db *sql.DB
	if len(files) == 1 {
		var err error
		db, err = openDB(context.Background(), files[0].Path)
		if err != nil {
			return nil, fmt.Errorf("sqlite: %w", err)
		}
	}
	return &sqlEngine{kind: "sqlite", db: db, files: files, sqliteDir: cfg.SQLiteDir, openDB: openDB}, nil
}

func sqliteName(path string) string {
	base := path
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		base = path[i+1:]
	}
	base = strings.TrimSuffix(base, ".sqlite")
	base = strings.TrimSuffix(base, ".sqlite3")
	base = strings.TrimSuffix(base, ".db")
	if base == "" {
		return "sqlite"
	}
	return base
}

// DiscoverSQLite finds Laravel-style sqlite files under sitesRoot.
func DiscoverSQLite(sitesRoot string) []SQLiteFile {
	entries, err := os.ReadDir(sitesRoot)
	if err != nil {
		return nil
	}
	var out []SQLiteFile
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "server" {
			continue
		}
		siteDir := filepath.Join(sitesRoot, e.Name())
		seen := map[string]bool{}
		add := func(name, path string) {
			if seen[path] {
				return
			}
			if st, err := os.Stat(path); err == nil && !st.IsDir() {
				seen[path] = true
				out = append(out, SQLiteFile{Name: name, Path: path})
			}
		}
		add(e.Name(), filepath.Join(siteDir, "database", "database.sqlite"))
		add(e.Name(), filepath.Join(siteDir, "database.sqlite"))
		if entries2, err := os.ReadDir(filepath.Join(siteDir, "database")); err == nil {
			for _, f := range entries2 {
				if f.IsDir() {
					continue
				}
				n := f.Name()
				if strings.HasSuffix(n, ".sqlite") || strings.HasSuffix(n, ".sqlite3") {
					add(sqliteName(n), filepath.Join(siteDir, "database", n))
				}
			}
		}
	}
	return out
}

func clickhouseCaps() Capabilities {
	return Capabilities{
		CreateDatabase: true, DropDatabase: true,
		CreateTable: false, DropTable: true, Truncate: true,
		RowEdit: false, RowInsert: true,
		RenameDatabase: true, DuplicateDatabase: true,
		RenameTable: true, DuplicateTable: true, AlterTable: true,
	}
}
