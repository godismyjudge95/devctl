package newsite

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Request is the wizard input from the API and the CLI.
type Request struct {
	Name       string            `json:"name"`
	Template   string            `json:"template"`
	PHPVersion string            `json:"php_version,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
}

// ToolPHP is the PHP CLI wrapper the commands run with.
const ToolPHP = "php"

// Project is a validated Request plus everything the daemon resolved for it.
type Project struct {
	Name       string
	Domain     string
	SitesRoot  string
	Dir        string
	PHPVersion string // "" = the newest installed version
	Template   *Template
	// Options holds every option of the template, defaults applied.
	Options map[string]string
	// DB is the server database to create. nil for SQLite and no database.
	DB *Database
	// ServiceEnv holds the .env values of the mail and Redis services.
	ServiceEnv []EnvVar
	// Tools maps ToolPHP and each Template.Tools entry to a path.
	Tools map[string]string
}

// Database is a MySQL or PostgreSQL database the wizard creates.
type Database struct {
	Engine string // "mysql" or "postgres"
	Name   string
	Server DBServer
}

// DBServer is the connection to a database service, from its credentials file.
type DBServer struct {
	Connection, Host, Port, Username, Password string
}

// EnvVar is one KEY=value line of a .env file.
type EnvVar struct {
	Key, Value string
}

var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Parse validates req and returns the project it describes. The caller still
// resolves PHP, tools, database credentials, and service .env values, and
// refuses names that already exist.
func Parse(req Request, sitesRoot string) (*Project, error) {
	name := strings.TrimSpace(req.Name)
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid name %q: use lowercase letters, digits, and hyphens (at most 63)", req.Name)
	}
	// {sitesRoot}/server holds devctl itself.
	if name == "server" {
		return nil, fmt.Errorf("the name %q is reserved", name)
	}
	tpl, ok := Lookup(req.Template)
	if !ok {
		return nil, fmt.Errorf("unknown template %q: want one of %s", req.Template, strings.Join(templateIDs(), ", "))
	}
	opts, err := resolveOptions(tpl, req.Options)
	if err != nil {
		return nil, err
	}
	p := &Project{
		Name:       name,
		Domain:     name + ".test",
		SitesRoot:  sitesRoot,
		Dir:        filepath.Join(sitesRoot, name),
		PHPVersion: strings.TrimSpace(req.PHPVersion),
		Template:   tpl,
		Options:    opts,
	}
	if engine := opts["database"]; engine == dbMySQL || engine == dbPostgres {
		// Same name the Laravel installer derives.
		p.DB = &Database{Engine: engine, Name: strings.ReplaceAll(name, "-", "_")}
	}
	return p, nil
}

func templateIDs() []string {
	ids := make([]string, len(Templates))
	for i, t := range Templates {
		ids[i] = t.ID
	}
	return ids
}

func resolveOptions(tpl *Template, in map[string]string) (map[string]string, error) {
	known := make(map[string]bool, len(tpl.Options))
	for _, o := range tpl.Options {
		known[o.ID] = true
	}
	for k := range in {
		if !known[k] {
			return nil, fmt.Errorf("unknown option %q for template %s", k, tpl.ID)
		}
	}
	out := make(map[string]string, len(tpl.Options))
	for _, o := range tpl.Options {
		v := strings.TrimSpace(in[o.ID])
		if v == "" {
			v = o.Default
		}
		if len(o.Choices) > 0 {
			if !hasChoice(o.Choices, v) {
				return nil, fmt.Errorf("invalid %s %q: want one of %s", o.ID, v, strings.Join(choiceValues(o.Choices), ", "))
			}
		} else if strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("invalid %s: control characters are not allowed", o.ID)
		}
		out[o.ID] = v
	}
	return out, nil
}

func hasChoice(cs []Choice, v string) bool {
	for _, c := range cs {
		if c.Value == v {
			return true
		}
	}
	return false
}

func choiceValues(cs []Choice) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Value
	}
	return out
}

// DotEnv returns the values the wizard writes to the project .env.
func (p *Project) DotEnv() []EnvVar {
	vars := []EnvVar{{"APP_URL", "https://" + p.Domain}}
	if db := p.DB; db != nil {
		vars = append(vars,
			EnvVar{"DB_CONNECTION", db.Server.Connection},
			EnvVar{"DB_HOST", db.Server.Host},
			EnvVar{"DB_PORT", db.Server.Port},
			EnvVar{"DB_DATABASE", db.Name},
			EnvVar{"DB_USERNAME", db.Server.Username},
			EnvVar{"DB_PASSWORD", db.Server.Password},
		)
	}
	return append(vars, p.ServiceEnv...)
}

// EnvVarsFromMap returns m as EnvVars sorted by key.
func EnvVarsFromMap(m map[string]string) []EnvVar {
	out := make([]EnvVar, 0, len(m))
	for k, v := range m {
		out = append(out, EnvVar{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// MergeEnv sets each var in the .env content. A line that sets the key, or a
// commented-out `# KEY=` line, is replaced in place. Other keys are appended.
func MergeEnv(content string, vars []EnvVar) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, v := range vars {
		line := v.Key + "=" + v.Value
		if i := envLineIndex(lines, v.Key); i >= 0 {
			lines[i] = line
		} else {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func envLineIndex(lines []string, key string) int {
	commented := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, key+"=") {
			return i
		}
		if commented < 0 && strings.HasPrefix(t, "#") && strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(t, "#")), key+"=") {
			commented = i
		}
	}
	return commented
}
