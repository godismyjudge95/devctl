package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

type dbEngine struct {
	id     string
	scheme string
	port   string
}

var (
	engineMySQL      = dbEngine{id: "mysql", scheme: "mysql", port: "3306"}
	enginePostgres   = dbEngine{id: "postgres", scheme: "postgresql", port: "5432"}
	engineSQLite     = dbEngine{id: "sqlite", scheme: "sqlite"}
	engineClickHouse = dbEngine{id: "clickhouse", scheme: "clickhouse", port: "8123"}
)

// dbEngines maps engine names typed on the command line and Laravel
// DB_CONNECTION values to the engine. MariaDB clients use devctl's MySQL.
var dbEngines = map[string]dbEngine{
	"mysql":      engineMySQL,
	"mariadb":    engineMySQL,
	"postgres":   enginePostgres,
	"postgresql": enginePostgres,
	"pgsql":      enginePostgres,
	"sqlite":     engineSQLite,
	"clickhouse": engineClickHouse,
}

const dbEngineNames = "mysql, mariadb, postgres, pgsql, sqlite, clickhouse"

type dbTarget struct {
	Engine string `json:"engine"`
	Name   string `json:"database"`
}

func (t dbTarget) String() string { return t.Engine + "/" + t.Name }

type dbConnection struct {
	Site   string `json:"site"`
	Driver string `json:"driver"`
	Engine string `json:"engine"`
	// Name is what db:* commands take. For SQLite it is not the file path.
	Name     string `json:"name"`
	Host     string `json:"host,omitempty"`
	Port     string `json:"port,omitempty"`
	Database string `json:"database"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	URL      string `json:"url"`
}

func (c dbConnection) target() dbTarget { return dbTarget{Engine: c.Engine, Name: c.Name} }

// connectionFromEnv reads the DB_* keys of a site .env. Missing keys take
// Laravel's config/database.php defaults, because that is what the app uses.
func connectionFromEnv(env map[string]string, root string) (dbConnection, error) {
	driver := env["DB_CONNECTION"]
	if driver == "" {
		return dbConnection{}, fmt.Errorf("%s has no DB_CONNECTION", filepath.Join(root, ".env"))
	}
	eng, ok := dbEngines[strings.ToLower(driver)]
	if !ok {
		return dbConnection{}, fmt.Errorf("DB_CONNECTION=%s is not a devctl engine (%s)", driver, dbEngineNames)
	}
	conn := dbConnection{Driver: driver, Engine: eng.id, Database: env["DB_DATABASE"]}
	if eng == engineSQLite {
		path := conn.Database
		if path == "" {
			path = filepath.Join(root, "database", "database.sqlite")
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		conn.Database = path
		conn.Name = sqliteDBName(root, path)
		conn.URL = "sqlite://" + filepath.ToSlash(path)
		return conn, nil
	}
	conn.Host = firstNonEmpty(env["DB_HOST"], "127.0.0.1")
	conn.Port = firstNonEmpty(env["DB_PORT"], eng.port)
	conn.Database = firstNonEmpty(conn.Database, "laravel")
	conn.Username = firstNonEmpty(env["DB_USERNAME"], "root")
	conn.Password = env["DB_PASSWORD"]
	conn.Name = conn.Database
	u := url.URL{Scheme: eng.scheme, User: url.User(conn.Username), Host: net.JoinHostPort(conn.Host, conn.Port), Path: "/" + conn.Database}
	if conn.Password != "" {
		u.User = url.UserPassword(conn.Username, conn.Password)
	}
	conn.URL = u.String()
	return conn, nil
}

// sqliteDBName matches database.DiscoverSQLite: a site's database.sqlite is
// named after the site directory, any other file after its base name.
func sqliteDBName(root, path string) string {
	base, dir := filepath.Base(path), filepath.Dir(path)
	if base == "database.sqlite" && (dir == root || dir == filepath.Join(root, "database")) {
		return filepath.Base(root)
	}
	for _, ext := range []string{".sqlite", ".sqlite3", ".db"} {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

// parseDotenv reads KEY=value lines, with optional quotes, `export`, and
// trailing ` #` comments on unquoted values.
func parseDotenv(data string) map[string]string {
	env := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.HasPrefix(key, "#") {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) > 0 && (val[0] == '"' || val[0] == '\'') {
			if end := strings.IndexByte(val[1:], val[0]); end >= 0 {
				val = val[1 : end+1]
			}
		} else if i := strings.Index(val, " #"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		env[key] = val
	}
	return env
}

func cwdConnection(c *Client) (dbConnection, error) {
	site, err := findSite(c, ".", nil)
	if err != nil {
		return dbConnection{}, err
	}
	return siteConnection(site)
}

func siteConnection(site *Site) (dbConnection, error) {
	if site.RootPath == "" {
		return dbConnection{}, fmt.Errorf("%s is a proxy site with no .env", site.Domain)
	}
	data, err := os.ReadFile(filepath.Join(site.RootPath, ".env"))
	if err != nil {
		return dbConnection{}, fmt.Errorf("%s: %w", site.Domain, err)
	}
	conn, err := connectionFromEnv(parseDotenv(string(data)), site.RootPath)
	conn.Site = site.Domain
	return conn, err
}

// splitTarget takes a db:* command's positionals. When the first one names
// an engine, it and the next one are the target. Otherwise the target is the
// database of the site in the cwd.
func splitTarget(pos []string, fromSite func() (dbTarget, error)) (dbTarget, []string, error) {
	if len(pos) > 0 {
		if eng, ok := dbEngines[strings.ToLower(pos[0])]; ok {
			if len(pos) < 2 {
				return dbTarget{}, nil, fmt.Errorf("missing database name after %q", pos[0])
			}
			return dbTarget{Engine: eng.id, Name: pos[1]}, pos[2:], nil
		}
	}
	t, err := fromSite()
	return t, pos, err
}

// dbArgs parses fs anywhere in args, resolves the target, and checks that
// minRest to maxRest positionals (maxRest < 0: any number) follow it.
func dbArgs(c *Client, name string, fs *flag.FlagSet, args []string, minRest, maxRest int) (dbTarget, []string, error) {
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return dbTarget{}, nil, err
	}
	t, rest, err := splitTarget(pos, func() (dbTarget, error) {
		conn, err := cwdConnection(c)
		if err != nil {
			return dbTarget{}, fmt.Errorf("%w\npass <engine> <name>, or run this in a site directory", err)
		}
		return conn.target(), nil
	})
	if err != nil {
		return dbTarget{}, nil, err
	}
	if len(rest) < minRest || (maxRest >= 0 && len(rest) > maxRest) {
		return dbTarget{}, nil, fmt.Errorf("usage: devctl %s %s", name, Find(name).Usage)
	}
	return t, rest, nil
}

// parseInterspersed parses flags before, between, and after positionals.
// Everything after a bare "--" is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(pos, rest...), nil
		}
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func printDBDone(jsonMode bool, t dbTarget, msg string) {
	if jsonMode {
		PrintJSON(map[string]string{"status": "ok", "engine": t.Engine, "database": t.Name})
		return
	}
	PrintOK(msg)
}

// confirmDrop asks on the terminal. It refuses when stdin is not a terminal,
// so a script cannot drop a database without --force.
func confirmDrop(t dbTarget) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("refusing to drop %s without --force (stdin is not a terminal)", t)
	}
	fmt.Fprintf(os.Stderr, "Drop %s database %q and all its data? [y/N] ", t.Engine, t.Name)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return errors.New("aborted")
}

// exportToFile writes the dump to a temp file and renames it into place, so
// a failed export leaves no partial file and keeps an existing one intact.
func exportToFile(c *Client, t dbTarget, file string, progress io.Writer) (int64, error) {
	tmp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".*.partial")
	if err != nil {
		return 0, err
	}
	n, err := c.ExportDatabase(t.Engine, t.Name, io.MultiWriter(tmp, progress))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), file)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return n, err
	}
	return n, nil
}

// progress counts the bytes written to it and redraws one stderr line while
// a transfer runs. It stays silent unless stderr is a terminal.
type progress struct {
	verb  string
	total int64
	n     int64
	drawn time.Time
	on    bool
}

func newProgress(verb string, total int64, jsonMode bool) *progress {
	return &progress{verb: verb, total: total, on: !jsonMode && isatty.IsTerminal(os.Stderr.Fd())}
}

func (p *progress) Write(b []byte) (int, error) {
	p.n += int64(len(b))
	if p.on && time.Since(p.drawn) >= 200*time.Millisecond {
		p.drawn = time.Now()
		line := p.verb + " " + formatBytes(p.n)
		if p.total > 0 {
			line += fmt.Sprintf(" / %s (%d%%)", formatBytes(p.total), p.n*100/p.total)
		}
		fmt.Fprint(os.Stderr, "\r\033[K"+line)
	}
	return len(b), nil
}

func (p *progress) clear() {
	if p.on && !p.drawn.IsZero() {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(1024), 0
	for m := n / 1024; m >= 1024; m /= 1024 {
		div *= 1024
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func cellText(raw json.RawMessage) string {
	if string(raw) == "null" {
		return styleDim.Render("NULL")
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.ReplaceAll(s, "\n", `\n`)
	}
	return string(raw)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

var dbTargetArgs = []ArgDef{
	{Name: "engine", Description: "mysql, postgres, sqlite, or clickhouse (pgsql and mariadb also work). Default: DB_CONNECTION in the .env of the site in the cwd", Optional: true},
	{Name: "name", Description: "Database name. Default: DB_DATABASE in the .env of the site in the cwd", Optional: true},
}

func init() {
	Register(&Cmd{
		Name:        "db:list",
		Description: "List databases on one engine, or on every running engine",
		Usage:       "[engine]",
		Args:        []ArgDef{{Name: "engine", Description: "mysql, postgres, sqlite, or clickhouse", Optional: true}},
		Examples:    []string{"devctl db:list", "devctl db:list mysql --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) > 1 {
				return fmt.Errorf("usage: devctl db:list [engine]")
			}
			var ids []string
			if len(args) == 1 {
				eng, ok := dbEngines[strings.ToLower(args[0])]
				if !ok {
					return fmt.Errorf("unknown engine %q (%s)", args[0], dbEngineNames)
				}
				ids = []string{eng.id}
			} else {
				engines, err := c.ListDBEngines()
				if err != nil {
					return err
				}
				for _, e := range engines {
					if e.Installed && e.Running {
						ids = append(ids, e.ID)
					}
				}
			}
			type row struct {
				dbTarget
				System bool `json:"system"`
			}
			rows := []row{}
			for _, id := range ids {
				dbs, err := c.ListDatabases(id)
				if err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
				for _, d := range dbs {
					rows = append(rows, row{dbTarget{Engine: id, Name: d.Name}, d.System})
				}
			}
			if jsonMode {
				PrintJSON(rows)
				return nil
			}
			if len(rows) == 0 {
				fmt.Println("No databases.")
				return nil
			}
			table := make([][]string, 0, len(rows))
			for _, r := range rows {
				name := r.Name
				if r.System {
					name += styleDim.Render(" (system)")
				}
				table = append(table, []string{r.Engine, name})
			}
			Table([]string{"Engine", "Database"}, table)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "db:create",
		Description: "Create a database",
		Usage:       "[<engine> <name>]",
		Args:        dbTargetArgs,
		Examples:    []string{"devctl db:create mysql shop", "devctl db:create   # the site's DB_DATABASE"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			t, _, err := dbArgs(c, "db:create", flag.NewFlagSet("db:create", flag.ContinueOnError), args, 0, 0)
			if err != nil {
				return err
			}
			if err := c.CreateDatabase(t.Engine, t.Name); err != nil {
				return err
			}
			printDBDone(jsonMode, t, "Created "+t.String())
			return nil
		},
	})

	Register(&Cmd{
		Name:        "db:drop",
		MCP:         MCPConfirm,
		Description: "Drop a database (system databases are refused). Asks first unless --force",
		Usage:       "[<engine> <name>] [--force]",
		Args:        dbTargetArgs,
		Flags:       []FlagDef{{Name: "force", Default: "false", Description: "Drop without asking. Required when stdin is not a terminal"}},
		Examples:    []string{"devctl db:drop mysql shop", "devctl db:drop postgres scratch --force"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("db:drop", flag.ContinueOnError)
			force := fs.Bool("force", false, "")
			t, _, err := dbArgs(c, "db:drop", fs, args, 0, 0)
			if err != nil {
				return err
			}
			if !*force {
				if err := confirmDrop(t); err != nil {
					return err
				}
			}
			if err := c.DropDatabase(t.Engine, t.Name); err != nil {
				return err
			}
			printDBDone(jsonMode, t, "Dropped "+t.String())
			return nil
		},
	})

	Register(&Cmd{
		Name:        "db:duplicate",
		Description: "Copy a database (schema and data) to a new name",
		Usage:       "[<engine> <source>] <target>",
		Args: append(append([]ArgDef{}, dbTargetArgs...),
			ArgDef{Name: "target", Description: "Name of the new database"}),
		Examples: []string{"devctl db:duplicate mysql shop shop_backup", "devctl db:duplicate shop_before_migrate"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			t, rest, err := dbArgs(c, "db:duplicate", flag.NewFlagSet("db:duplicate", flag.ContinueOnError), args, 1, 1)
			if err != nil {
				return err
			}
			to := rest[0]
			if err := c.DuplicateDatabase(t.Engine, t.Name, to); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok", "engine": t.Engine, "from": t.Name, "to": to})
				return nil
			}
			PrintOK("Copied " + t.String() + " to " + to)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "db:import",
		MCP:         MCPConfirm,
		Description: "Import a .sql or .sql.gz dump into a MySQL or PostgreSQL database. Use - for stdin",
		Usage:       "[<engine> <name>] <file|-> [--create]",
		Args: append(append([]ArgDef{}, dbTargetArgs...),
			ArgDef{Name: "file", Description: "Dump file (.sql or .sql.gz), or - for stdin"}),
		Flags: []FlagDef{{Name: "create", Default: "false", Description: "Create the database when it does not exist"}},
		Examples: []string{
			"devctl db:import mysql shop dump.sql.gz --create",
			"devctl db:import backup.sql",
			"gunzip -c dump.sql.gz | devctl db:import postgres app -",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("db:import", flag.ContinueOnError)
			create := fs.Bool("create", false, "")
			t, rest, err := dbArgs(c, "db:import", fs, args, 1, 1)
			if err != nil {
				return err
			}
			var src io.Reader = os.Stdin
			var size int64
			if rest[0] == "-" {
				if isatty.IsTerminal(os.Stdin.Fd()) {
					return errors.New("stdin is a terminal: pipe a dump into db:import, or pass a file")
				}
			} else {
				f, err := os.Open(rest[0])
				if err != nil {
					return err
				}
				defer f.Close()
				if st, err := f.Stat(); err == nil {
					size = st.Size()
				}
				src = f
			}
			p := newProgress("Importing", size, jsonMode)
			res, err := c.ImportDatabase(t.Engine, t.Name, io.TeeReader(src, p), *create)
			p.clear()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(struct {
					dbTarget
					DBImportResult
				}{t, res})
				return nil
			}
			msg := fmt.Sprintf("Imported %s of SQL into %s", formatBytes(res.Bytes), t)
			if res.Created {
				msg += " (created)"
			}
			PrintOK(msg)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "db:export",
		Description: "Dump a MySQL or PostgreSQL database as SQL to a file, or to stdout without one",
		Usage:       "[<engine> <name>] [file]",
		Args: append(append([]ArgDef{}, dbTargetArgs...),
			ArgDef{Name: "file", Description: "Output file. Without it (or with -) the SQL goes to stdout", Optional: true}),
		Examples: []string{
			"devctl db:export mysql shop shop.sql",
			"devctl db:export > dump.sql",
			"devctl db:export postgres app | gzip > app.sql.gz",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			t, rest, err := dbArgs(c, "db:export", flag.NewFlagSet("db:export", flag.ContinueOnError), args, 0, 1)
			if err != nil {
				return err
			}
			p := newProgress("Exporting", 0, jsonMode)
			if len(rest) == 0 || rest[0] == "-" {
				_, err := c.ExportDatabase(t.Engine, t.Name, io.MultiWriter(os.Stdout, p))
				p.clear()
				return err
			}
			file := rest[0]
			n, err := exportToFile(c, t, file, p)
			p.clear()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]any{"status": "ok", "engine": t.Engine, "database": t.Name, "file": file, "bytes": n})
				return nil
			}
			PrintOK(fmt.Sprintf("Exported %s to %s (%s)", t, file, formatBytes(n)))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "db:query",
		Description: "Run SQL and print the result as a table (or JSON with --json)",
		Usage:       "[<engine> <name>] <sql> [--limit=N]",
		Args: append(append([]ArgDef{}, dbTargetArgs...),
			ArgDef{Name: "sql", Description: "SQL statement (quote it)"}),
		Flags: []FlagDef{{Name: "limit", Default: "500", Description: "Maximum rows to return (the daemon caps it at 5000)"}},
		Examples: []string{
			`devctl db:query mysql shop "SELECT id, email FROM users LIMIT 5"`,
			`devctl db:query "SELECT COUNT(*) FROM jobs" --json`,
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("db:query", flag.ContinueOnError)
			limit := fs.Int("limit", 500, "")
			t, rest, err := dbArgs(c, "db:query", fs, args, 1, -1)
			if err != nil {
				return err
			}
			res, err := c.QueryDatabase(t.Engine, t.Name, strings.Join(rest, " "), *limit)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(res)
				return nil
			}
			if len(res.Columns) == 0 {
				PrintOK(fmt.Sprintf("%s: %d rows affected (%d ms)", res.Statement, res.RowsAffected, res.DurationMS))
				return nil
			}
			headers := make([]string, len(res.Columns))
			for i, col := range res.Columns {
				headers[i] = col.Name
			}
			rows := make([][]string, len(res.Rows))
			for i, r := range res.Rows {
				rows[i] = make([]string, len(r))
				for j, cell := range r {
					rows[i][j] = cellText(cell)
				}
			}
			Table(headers, rows)
			footer := fmt.Sprintf("%d rows (%d ms)", len(res.Rows), res.DurationMS)
			if res.Limited {
				footer += fmt.Sprintf(", limited to %d: pass --limit for more", len(res.Rows))
			}
			fmt.Println(styleDim.Render(footer))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "db:connection",
		Description: "Show a site's database connection and URL from its .env (default: the site in the cwd)",
		Usage:       "[domain]",
		Args:        []ArgDef{{Name: "domain", Description: "Site domain, id, or path", Optional: true}},
		Examples:    []string{"devctl db:connection", "devctl db:connection myapp.test --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) > 1 {
				return fmt.Errorf("usage: devctl db:connection [domain]")
			}
			ident := "."
			if len(args) == 1 {
				ident = args[0]
			}
			site, err := findSite(c, ident, nil)
			if err != nil {
				return err
			}
			conn, err := siteConnection(site)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(conn)
				return nil
			}
			Header("Database: " + conn.Site)
			KV("Driver", conn.Driver)
			KV("Engine", conn.Engine)
			if conn.Engine == engineSQLite.id {
				KV("Name", conn.Name)
				KV("File", conn.Database)
			} else {
				KV("Host", conn.Host)
				KV("Port", conn.Port)
				KV("Database", conn.Database)
				KV("Username", conn.Username)
				KV("Password", orDash(conn.Password))
			}
			KV("URL", conn.URL)
			return nil
		},
	})
}
