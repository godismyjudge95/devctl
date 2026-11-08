// Package projectconfig reads the opt-in project file (.devctl.yaml, with an
// optional uncommitted .devctl.local.yaml on top) that declares how devctl runs
// a site, and plans the changes that make a site match it.
package projectconfig

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/danielgormly/devctl/node"
	"github.com/danielgormly/devctl/workers"
	"go.yaml.in/yaml/v3"
)

const (
	FileName      = ".devctl.yaml"
	LocalFileName = ".devctl.local.yaml"
	// SchemaURL is where editors fetch the schema. It names the file in the
	// repository so it stays stable across releases.
	SchemaURL = "https://raw.githubusercontent.com/godismyjudge95/devctl/main/projectconfig/schema.json"
)

// Schema is the JSON Schema of the file.
//
//go:embed schema.json
var Schema []byte

// Config is one parsed file, or the merge of both. A nil field is not set, so
// devctl leaves that value of the site alone.
type Config struct {
	PHP       *string   `yaml:"php,omitempty"`
	Node      *string   `yaml:"node,omitempty"`
	PublicDir *string   `yaml:"public_dir,omitempty"`
	HTTPS     *bool     `yaml:"https,omitempty"`
	CORS      *bool     `yaml:"cors,omitempty"`
	SPX       *bool     `yaml:"spx,omitempty"`
	Aliases   *[]string `yaml:"aliases,omitempty"`
	Workers   *[]Worker `yaml:"workers,omitempty"`
	Services  *[]string `yaml:"services,omitempty"`
}

// Worker is a worker the project wants. In the file it is a type name
// ("queue") or a mapping ({type: custom, command: ...}).
type Worker struct {
	Type    workers.Type `yaml:"type"`
	Command string       `yaml:"command,omitempty"`
}

func (w *Worker) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		w.Type = workers.Type(n.Value)
		return nil
	case yaml.MappingNode:
		for i := 0; i < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if v.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: worker %s must be a string", v.Line, k.Value)
			}
			switch k.Value {
			case "type":
				w.Type = workers.Type(v.Value)
			case "command":
				w.Command = strings.TrimSpace(v.Value)
			default:
				return fmt.Errorf("line %d: unknown worker key %q (use type, command)", k.Line, k.Value)
			}
		}
		return nil
	}
	return fmt.Errorf("line %d: a worker is a type name or a mapping with type and command", n.Line)
}

func (w Worker) MarshalYAML() (any, error) {
	if w.Command == "" {
		return string(w.Type), nil
	}
	return struct {
		Type    workers.Type `yaml:"type"`
		Command string       `yaml:"command"`
	}{w.Type, w.Command}, nil
}

// Keys are the top-level keys of the file, in the order Encode writes them.
var Keys = []string{"php", "node", "public_dir", "https", "cors", "spx", "aliases", "workers", "services"}

// ServiceIDs are the services a project can require: every installable
// service that is not always on (Caddy, DNS).
var ServiceIDs = []string{"redis", "mysql", "postgres", "mailpit", "meilisearch", "typesense", "reverb", "maxio", "clickhouse"}

// Load reads FileName and LocalFileName from root and merges them (the local
// file wins key by key). files lists the files that exist. When neither exists
// the Config is empty and files is nil.
func Load(root string) (cfg Config, files []string, err error) {
	for _, name := range []string{FileName, LocalFileName} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		files = append(files, name)
		if err != nil {
			return Config{}, files, err
		}
		c, err := Parse(name, data)
		if err != nil {
			return Config{}, files, err
		}
		cfg = cfg.Merge(c)
	}
	return cfg, files, nil
}

// Parse decodes and validates one file. Errors name the file and line.
func Parse(name string, data []byte) (Config, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Config{}, fmt.Errorf("%s: %w", name, err)
	}
	if len(doc.Content) == 0 {
		return Config{}, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return Config{}, fmt.Errorf("%s: line %d: the file must be a mapping of keys (php, https, …)", name, root.Line)
	}

	for i := 0; i < len(root.Content); i += 2 {
		if k := root.Content[i]; !slices.Contains(Keys, k.Value) {
			return Config{}, fmt.Errorf("%s: line %d: unknown key %q (use %s)", name, k.Line, k.Value, strings.Join(Keys, ", "))
		}
	}

	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		var te *yaml.TypeError
		if errors.As(err, &te) && len(te.Errors) > 0 {
			return Config{}, fmt.Errorf("%s: %s", name, te.Errors[0])
		}
		return Config{}, fmt.Errorf("%s: %w", name, err)
	}
	if p := cfg.problem(); p != nil {
		return Config{}, fmt.Errorf("%s: line %d: %s", name, p.line(root), p.msg)
	}
	return cfg, nil
}

// problem is a validation error at a key, or at item index of a list key.
type problem struct {
	key   string
	index int
	msg   string
}

func (p problem) line(root *yaml.Node) int {
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Value != p.key {
			continue
		}
		if p.index >= 0 && p.index < len(v.Content) {
			return v.Content[p.index].Line
		}
		return k.Line
	}
	return root.Line
}

var phpVersion = regexp.MustCompile(`^\d+\.\d+$`)

func (c Config) problem() *problem {
	at := func(key string, index int, format string, args ...any) *problem {
		return &problem{key: key, index: index, msg: key + ": " + fmt.Sprintf(format, args...)}
	}
	if c.PHP != nil && !phpVersion.MatchString(*c.PHP) {
		return at("php", -1, "%q is not a PHP version like 8.4", *c.PHP)
	}
	if c.Node != nil && (strings.ContainsAny(*c.Node, "\r\n") || !node.Valid(*c.Node)) {
		return at("node", -1, "%q is not a Node version (use 22, 20.11.1, lts/*, or a range like >=20)", *c.Node)
	}
	if c.PublicDir != nil && *c.PublicDir != "" && !filepath.IsLocal(*c.PublicDir) {
		return at("public_dir", -1, "%q must be a path inside the project", *c.PublicDir)
	}
	if c.Aliases != nil {
		for i, a := range *c.Aliases {
			if !hostname.MatchString(a) {
				return at("aliases", i, "%q is not a host name", a)
			}
			if slices.Index(*c.Aliases, a) != i {
				return at("aliases", i, "%q is listed twice", a)
			}
		}
	}
	if c.Workers != nil {
		for i, w := range *c.Workers {
			if err := workers.Validate(w.Type, w.Command); err != nil {
				return at("workers", i, "%s", strings.TrimPrefix(err.Error(), workers.ErrInvalid.Error()+": "))
			}
			if !w.Type.Multiple() && slices.IndexFunc(*c.Workers, func(o Worker) bool { return o.Type == w.Type }) != i {
				return at("workers", i, "only one %s worker is allowed", w.Type)
			}
		}
	}
	if c.Services != nil {
		for i, s := range *c.Services {
			if !slices.Contains(ServiceIDs, s) {
				return at("services", i, "unknown service %q (use %s)", s, strings.Join(ServiceIDs, ", "))
			}
			if slices.Index(*c.Services, s) != i {
				return at("services", i, "%q is listed twice", s)
			}
		}
	}
	return nil
}

var hostname = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// Merge returns c with every key that over sets replaced.
func (c Config) Merge(over Config) Config {
	if over.PHP != nil {
		c.PHP = over.PHP
	}
	if over.Node != nil {
		c.Node = over.Node
	}
	if over.PublicDir != nil {
		c.PublicDir = over.PublicDir
	}
	if over.HTTPS != nil {
		c.HTTPS = over.HTTPS
	}
	if over.CORS != nil {
		c.CORS = over.CORS
	}
	if over.SPX != nil {
		c.SPX = over.SPX
	}
	if over.Aliases != nil {
		c.Aliases = over.Aliases
	}
	if over.Workers != nil {
		c.Workers = over.Workers
	}
	if over.Services != nil {
		c.Services = over.Services
	}
	return c
}

// Managed lists the site fields the config sets, by their API names.
func (c Config) Managed() []string {
	out := []string{}
	add := func(set bool, field string) {
		if set {
			out = append(out, field)
		}
	}
	add(c.PHP != nil, "php_version")
	add(c.Node != nil, "node")
	add(c.PublicDir != nil, "public_dir")
	add(c.HTTPS != nil, "https")
	add(c.CORS != nil, "cors")
	add(c.SPX != nil, "spx_enabled")
	add(c.Aliases != nil, "aliases")
	add(c.Workers != nil, "workers")
	add(c.Services != nil, "services")
	return out
}

// Site is the part of a site that a config can set.
type Site struct {
	PHP       string
	Node      string // the version the site's Node files name; "" when none
	PublicDir string
	HTTPS     bool
	CORS      bool
	SPX       bool
	Aliases   []string
	Workers   []Worker
}

// Plan is what makes a site match its config.
type Plan struct {
	Want Site
	// SiteRow is true when a value stored on the site row changes.
	SiteRow bool
	SPX     bool
	Node    bool
	// AddWorkers are wanted workers the site lacks. RemoveWorkers are indexes
	// into the current workers that the config does not list.
	AddWorkers    []Worker
	RemoveWorkers []int
	// Changes describe each change for the log.
	Changes []string
}

// Empty reports whether the site already matches.
func (p Plan) Empty() bool { return len(p.Changes) == 0 }

// Plan compares the site with the config.
func (c Config) Plan(cur Site) Plan {
	want := cur
	if c.PHP != nil {
		want.PHP = *c.PHP
	}
	if c.Node != nil {
		want.Node = *c.Node
	}
	if c.PublicDir != nil {
		want.PublicDir = cleanDir(*c.PublicDir)
	}
	if c.HTTPS != nil {
		want.HTTPS = *c.HTTPS
	}
	if c.CORS != nil {
		want.CORS = *c.CORS
	}
	if c.SPX != nil {
		want.SPX = *c.SPX
	}
	if c.Aliases != nil {
		want.Aliases = *c.Aliases
	}
	if c.Workers != nil {
		want.Workers = *c.Workers
	}

	p := Plan{Want: want}
	change := func(differs bool, field string, from, to any) bool {
		if differs {
			p.Changes = append(p.Changes, fmt.Sprintf("%s: %v -> %v", field, from, to))
		}
		return differs
	}
	row := change(cur.PHP != want.PHP, "php_version", cur.PHP, want.PHP)
	row = change(cur.PublicDir != want.PublicDir, "public_dir", cur.PublicDir, want.PublicDir) || row
	row = change(cur.HTTPS != want.HTTPS, "https", cur.HTTPS, want.HTTPS) || row
	row = change(cur.CORS != want.CORS, "cors", cur.CORS, want.CORS) || row
	row = change(!slices.Equal(cur.Aliases, want.Aliases), "aliases", cur.Aliases, want.Aliases) || row
	p.SPX = change(cur.SPX != want.SPX, "spx_enabled", cur.SPX, want.SPX)
	p.SiteRow = row || p.SPX
	p.Node = change(cur.Node != want.Node, "node", cur.Node, want.Node)

	if c.Workers != nil {
		unmatched := slices.Clone(want.Workers)
		for i, w := range cur.Workers {
			if j := slices.Index(unmatched, w); j >= 0 {
				unmatched = slices.Delete(unmatched, j, j+1)
				continue
			}
			p.RemoveWorkers = append(p.RemoveWorkers, i)
			change(true, "workers", "remove "+w.label(), "")
		}
		p.AddWorkers = unmatched
		for _, w := range unmatched {
			change(true, "workers", "", "add "+w.label())
		}
	}
	return p
}

func cleanDir(d string) string {
	if d = filepath.Clean(d); d == "." {
		return ""
	}
	return d
}

func (w Worker) label() string {
	if w.Command == "" {
		return string(w.Type)
	}
	return string(w.Type) + " (" + w.Command + ")"
}

// Encode writes a config file that sets every value of s, with the schema
// line editors read.
func Encode(s Site) ([]byte, error) {
	aliases := s.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	cfg := Config{
		PHP:       &s.PHP,
		PublicDir: &s.PublicDir,
		HTTPS:     &s.HTTPS,
		CORS:      &s.CORS,
		SPX:       &s.SPX,
		Aliases:   &aliases,
	}
	if s.Node != "" {
		cfg.Node = &s.Node
	}
	if len(s.Workers) > 0 {
		cfg.Workers = &s.Workers
	}
	var buf bytes.Buffer
	buf.WriteString("# yaml-language-server: $schema=" + SchemaURL + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Fingerprint changes whenever either file in root is created, changed, or
// removed. It is "" when neither file exists.
func Fingerprint(root string) string {
	var b strings.Builder
	for _, name := range []string{FileName, LocalFileName} {
		if fi, err := os.Stat(filepath.Join(root, name)); err == nil {
			fmt.Fprintf(&b, "%s:%d:%d;", name, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return root + ";" + b.String()
}
