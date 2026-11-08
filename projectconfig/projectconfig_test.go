package projectconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/config"
	"github.com/danielgormly/devctl/workers"
	"github.com/google/jsonschema-go/jsonschema"
	"go.yaml.in/yaml/v3"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParse_FullExample(t *testing.T) {
	cfg, err := Parse(FileName, readTestdata(t, "full.devctl.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if *cfg.PHP != "8.4" || *cfg.Node != "22" || *cfg.PublicDir != "public" || !*cfg.HTTPS || *cfg.CORS || *cfg.SPX {
		t.Errorf("scalars = %q %q %q %v %v %v", *cfg.PHP, *cfg.Node, *cfg.PublicDir, *cfg.HTTPS, *cfg.CORS, *cfg.SPX)
	}
	if !slices.Equal(*cfg.Aliases, []string{"www.myapp.test"}) {
		t.Errorf("aliases = %v", *cfg.Aliases)
	}
	wantWorkers := []Worker{{Type: "queue"}, {Type: "vite"}, {Type: "custom", Command: "php8.4 artisan reverb:start"}}
	if !slices.Equal(*cfg.Workers, wantWorkers) {
		t.Errorf("workers = %v, want %v", *cfg.Workers, wantWorkers)
	}
	if !slices.Equal(*cfg.Services, []string{"mysql", "redis", "mailpit"}) {
		t.Errorf("services = %v", *cfg.Services)
	}
}

func TestParse_UnquotedNumbersAreVersions(t *testing.T) {
	cfg, err := Parse(FileName, []byte("php: 8.10\nnode: 20\n"))
	if err != nil {
		t.Fatal(err)
	}
	if *cfg.PHP != "8.10" || *cfg.Node != "20" {
		t.Errorf("php = %q, node = %q; want the text as written", *cfg.PHP, *cfg.Node)
	}
}

func TestParse_EmptyFileSetsNothing(t *testing.T) {
	for _, in := range []string{"", "# just a comment\n"} {
		cfg, err := Parse(FileName, []byte(in))
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if got := cfg.Managed(); len(got) != 0 {
			t.Errorf("Parse(%q) manages %v, want nothing", in, got)
		}
	}
}

func TestParse_ErrorsNameFileAndLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"php: 8.4\nphpp: 8.3\n", `.devctl.yaml: line 2: unknown key "phpp"`},
		{"https: true\nphp: \"8\"\n", `.devctl.yaml: line 2: php: "8" is not a PHP version`},
		{"node: banana\n", `.devctl.yaml: line 1: node: "banana" is not a Node version`},
		{"public_dir: ../other\n", `line 1: public_dir: "../other" must be a path inside the project`},
		{"public_dir: /var/www\n", `line 1: public_dir: "/var/www" must be a path inside the project`},
		{"aliases:\n  - www.a.test\n  - Bad_Host\n", `line 3: aliases: "Bad_Host" is not a host name`},
		{"aliases:\n  - a.test\n  - a.test\n", `line 3: aliases: "a.test" is listed twice`},
		{"workers:\n  - queue\n  - cron\n", `line 3: workers: unknown type "cron"`},
		{"workers:\n  - type: custom\n", `line 2: workers: a custom worker needs a command`},
		{"workers:\n  - type: queue\n    command: x\n", `line 2: workers: a queue worker runs`},
		{"workers:\n  - queue\n  - vite\n  - queue\n", `line 4: workers: only one queue worker is allowed`},
		{"workers:\n  - type: custom\n    cmd: x\n", `line 3: unknown worker key "cmd"`},
		{"services:\n  - mysql\n  - mongodb\n", `line 3: services: unknown service "mongodb"`},
		{"services: [redis, redis]\n", `line 1: services: "redis" is listed twice`},
		{"- php\n", `line 1: the file must be a mapping`},
		{"\nhttps: [true]\n", `.devctl.yaml: line 2: cannot unmarshal`},
		{"php: [\n", `.devctl.yaml: yaml: line 1`},
	}
	for _, c := range cases {
		_, err := Parse(FileName, []byte(c.in))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%q) = %v, want an error containing %q", c.in, err, c.want)
		}
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_NoFiles(t *testing.T) {
	cfg, files, err := Load(t.TempDir())
	if err != nil || files != nil || len(cfg.Managed()) != 0 {
		t.Errorf("Load(empty dir) = %v, %v, %v; want nothing", cfg.Managed(), files, err)
	}
}

// A proxy site has no root path. Its config must not come from the daemon's
// working directory.
func TestLoad_EmptyRootReadsNothing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, FileName, "php: \"8.4\"\n")
	t.Chdir(dir)
	if _, files, err := Load(""); files != nil || err != nil {
		t.Errorf("Load(\"\") = %v, %v; want nothing", files, err)
	}
	if fp := Fingerprint(""); fp != "" {
		t.Errorf("Fingerprint(\"\") = %q, want empty", fp)
	}
}

func TestLoad_LocalFileOverridesKeyByKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, FileName, string(readTestdata(t, "full.devctl.yaml")))
	writeFile(t, dir, LocalFileName, string(readTestdata(t, "local.devctl.local.yaml")))

	cfg, files, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(files, []string{FileName, LocalFileName}) {
		t.Errorf("files = %v", files)
	}
	if *cfg.PHP != "8.2" {
		t.Errorf("php = %q, want the local 8.2", *cfg.PHP)
	}
	if !*cfg.HTTPS || *cfg.Node != "22" {
		t.Errorf("https = %v, node = %q; want the shared values", *cfg.HTTPS, *cfg.Node)
	}
	if cfg.Aliases == nil || len(*cfg.Aliases) != 0 || cfg.Workers == nil || len(*cfg.Workers) != 0 {
		t.Errorf("aliases = %v, workers = %v; want the local empty lists to win", cfg.Aliases, cfg.Workers)
	}
}

func TestLoad_LocalFileAlone(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, LocalFileName, "https: false\n")
	cfg, files, err := Load(dir)
	if err != nil || !slices.Equal(files, []string{LocalFileName}) || cfg.HTTPS == nil || *cfg.HTTPS {
		t.Errorf("Load = %v, %v, %v", cfg.Managed(), files, err)
	}
}

func TestLoad_ErrorNamesTheFileWithTheProblem(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, FileName, "php: \"8.4\"\n")
	writeFile(t, dir, LocalFileName, "\nphp: eight\n")
	_, files, err := Load(dir)
	if err == nil || !strings.HasPrefix(err.Error(), ".devctl.local.yaml: line 2: php:") {
		t.Errorf("err = %v", err)
	}
	if len(files) != 2 {
		t.Errorf("files = %v, want both", files)
	}
}

func TestManaged_ListsSetKeysByAPIName(t *testing.T) {
	cfg, err := Parse(FileName, []byte("spx: true\nphp: \"8.4\"\nworkers: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Managed(); !slices.Equal(got, []string{"php_version", "spx_enabled", "workers"}) {
		t.Errorf("Managed() = %v", got)
	}
}

func site() Site {
	return Site{
		PHP: "8.4", Node: "20", PublicDir: "public", HTTPS: true, Aliases: []string{"www.a.test"},
		Workers: []Worker{{Type: "queue"}, {Type: "custom", Command: "a"}, {Type: "custom", Command: "b"}},
	}
}

func plan(t *testing.T, yml string, cur Site) Plan {
	t.Helper()
	cfg, err := Parse(FileName, []byte(yml))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Plan(cur)
}

func TestPlan_MatchingSiteIsEmpty(t *testing.T) {
	p := plan(t, "php: \"8.4\"\nnode: \"20\"\npublic_dir: public/\nhttps: true\ncors: false\nspx: false\naliases: [www.a.test]\nworkers:\n  - type: custom\n    command: b\n  - queue\n  - type: custom\n    command: a\n", site())
	if !p.Empty() || p.SiteRow || p.Node || p.SPX || len(p.AddWorkers)+len(p.RemoveWorkers) != 0 {
		t.Errorf("plan = %+v, want empty", p)
	}
}

func TestPlan_UnsetKeysKeepSiteValues(t *testing.T) {
	p := plan(t, "cors: true\n", site())
	want := site()
	want.CORS = true
	if !p.SiteRow || p.SPX || p.Node || len(p.AddWorkers)+len(p.RemoveWorkers) != 0 {
		t.Errorf("plan = %+v", p)
	}
	if p.Want.PHP != want.PHP || p.Want.PublicDir != want.PublicDir || !p.Want.CORS || !slices.Equal(p.Want.Workers, want.Workers) {
		t.Errorf("want = %+v", p.Want)
	}
	if !slices.Equal(p.Changes, []string{"cors: false -> true"}) {
		t.Errorf("changes = %v", p.Changes)
	}
}

func TestPlan_SiteRowNodeAndSPX(t *testing.T) {
	p := plan(t, "php: \"8.3\"\naliases: []\npublic_dir: \".\"\n", site())
	if !p.SiteRow || p.SPX || p.Node {
		t.Errorf("php/aliases/public_dir plan = %+v", p)
	}
	if p.Want.PHP != "8.3" || len(p.Want.Aliases) != 0 || p.Want.PublicDir != "" {
		t.Errorf("want = %+v", p.Want)
	}

	p = plan(t, "spx: true\n", site())
	if !p.SPX || !p.SiteRow {
		t.Errorf("spx plan = %+v, want SPX and SiteRow", p)
	}

	p = plan(t, "node: lts/*\n", site())
	if !p.Node || p.SiteRow || p.Want.Node != "lts/*" {
		t.Errorf("node plan = %+v, want only Node", p)
	}
}

func TestPlan_WorkersAddMissingAndRemoveUnlisted(t *testing.T) {
	p := plan(t, "workers:\n  - vite\n  - type: custom\n    command: b\n", site())
	if !slices.Equal(p.AddWorkers, []Worker{{Type: "vite"}}) {
		t.Errorf("add = %v", p.AddWorkers)
	}
	if !slices.Equal(p.RemoveWorkers, []int{0, 1}) {
		t.Errorf("remove = %v, want the queue and custom a workers", p.RemoveWorkers)
	}
	if p.SiteRow {
		t.Error("workers alone must not save the site row")
	}

	p = plan(t, "workers: []\n", site())
	if !slices.Equal(p.RemoveWorkers, []int{0, 1, 2}) || len(p.AddWorkers) != 0 {
		t.Errorf("empty list plan = %+v, want every worker removed", p)
	}

	p = plan(t, "php: \"8.4\"\n", site())
	if len(p.RemoveWorkers) != 0 {
		t.Errorf("no workers key removed %v", p.RemoveWorkers)
	}
}

func TestEncode_RoundTripsToAnEmptyPlan(t *testing.T) {
	s := site()
	s.CORS, s.SPX = true, true
	data, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(string(data), "\n"); first != "# yaml-language-server: $schema="+SchemaURL {
		t.Errorf("first line = %q", first)
	}
	cfg, err := Parse(FileName, data)
	if err != nil {
		t.Fatalf("Parse(Encode()) = %v\n%s", err, data)
	}
	if p := cfg.Plan(s); !p.Empty() {
		t.Errorf("plan after round trip = %v\n%s", p.Changes, data)
	}
	if cfg.Services != nil {
		t.Error("Encode must not guess services")
	}
}

func TestEncode_LeavesOutNodeAndWorkersWhenNone(t *testing.T) {
	data, err := Encode(Site{PHP: "8.4"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(FileName, data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Node != nil || cfg.Workers != nil {
		t.Errorf("encoded %s", data)
	}
	if cfg.Aliases == nil || len(*cfg.Aliases) != 0 {
		t.Errorf("aliases = %v, want an empty list", cfg.Aliases)
	}
}

func TestFingerprint_ChangesWithEitherFile(t *testing.T) {
	dir := t.TempDir()
	if fp := Fingerprint(dir); fp != "" {
		t.Fatalf("no files: %q", fp)
	}
	writeFile(t, dir, FileName, "php: \"8.4\"\n")
	a := Fingerprint(dir)
	writeFile(t, dir, FileName, "php: \"8.33\"\n")
	b := Fingerprint(dir)
	writeFile(t, dir, LocalFileName, "https: false\n")
	c := Fingerprint(dir)
	if err := os.Remove(filepath.Join(dir, LocalFileName)); err != nil {
		t.Fatal(err)
	}
	d := Fingerprint(dir)
	if a == "" || a == b || b == c || c == d {
		t.Errorf("fingerprints did not change: %q %q %q %q", a, b, c, d)
	}
}

// --- Schema ---

func resolvedSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()
	var s jsonschema.Schema
	if err := json.Unmarshal(Schema, &s); err != nil {
		t.Fatal(err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func yamlToJSON(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := yaml.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if v == nil {
		v = map[string]any{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSchema_ExamplesAndEncodedFilesAreValid(t *testing.T) {
	rs := resolvedSchema(t)
	encoded, err := Encode(site())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"encoded": encoded}
	for _, name := range []string{"full.devctl.yaml", "minimal.devctl.yaml", "local.devctl.local.yaml"} {
		files[name] = readTestdata(t, name)
	}
	for name, data := range files {
		if err := rs.Validate(yamlToJSON(t, data)); err != nil {
			t.Errorf("%s: schema: %v", name, err)
		}
		if _, err := Parse(name, data); err != nil {
			t.Errorf("%s: Parse: %v", name, err)
		}
	}
}

// Both the schema (what editors check) and Parse (what devctl checks) reject
// these files.
func TestSchema_AgreesWithParseOnInvalidFiles(t *testing.T) {
	rs := resolvedSchema(t)
	for _, in := range []string{
		"phpp: 8.4\n",
		"php: \"8\"\n",
		"public_dir: /var/www\n",
		"public_dir: ../x\n",
		"https: \"true\"\n",
		"aliases: [Bad_Host]\n",
		"aliases: [a.test, a.test]\n",
		"workers: [cron]\n",
		"workers:\n  - type: custom\n",
		"workers:\n  - type: queue\n    command: x\n",
		"workers:\n  - type: custom\n    cmd: x\n",
		"services: [mongodb]\n",
		"services: [redis, redis]\n",
	} {
		if err := rs.Validate(yamlToJSON(t, []byte(in))); err == nil {
			t.Errorf("schema accepts %q", in)
		}
		if _, err := Parse(FileName, []byte(in)); err == nil {
			t.Errorf("Parse accepts %q", in)
		}
	}
}

type schemaDoc struct {
	ID         string `json:"$id"`
	Properties map[string]struct {
		Items struct {
			Enum  []string `json:"enum"`
			OneOf []struct {
				Enum       []string `json:"enum"`
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"oneOf"`
		} `json:"items"`
	} `json:"properties"`
}

func TestSchema_MatchesTheGoModel(t *testing.T) {
	var doc schemaDoc
	if err := json.Unmarshal(Schema, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.ID != SchemaURL {
		t.Errorf("$id = %q, want SchemaURL %q", doc.ID, SchemaURL)
	}
	var keys []string
	for k := range doc.Properties {
		keys = append(keys, k)
	}
	if !sameSet(keys, Keys) {
		t.Errorf("schema keys %v, Go keys %v", keys, Keys)
	}
	if got := doc.Properties["services"].Items.Enum; !sameSet(got, ServiceIDs) {
		t.Errorf("schema services %v, Go %v", got, ServiceIDs)
	}

	var all, single []string
	for _, wt := range workers.Types {
		all = append(all, string(wt))
		if !wt.Multiple() {
			single = append(single, string(wt))
		}
	}
	oneOf := doc.Properties["workers"].Items.OneOf
	if len(oneOf) != 2 {
		t.Fatalf("workers oneOf has %d entries", len(oneOf))
	}
	if !sameSet(oneOf[0].Enum, single) {
		t.Errorf("schema worker names %v, want the built-in types %v", oneOf[0].Enum, single)
	}
	if got := oneOf[1].Properties["type"].Enum; !sameSet(got, all) {
		t.Errorf("schema worker types %v, want %v", got, all)
	}
}

func TestServiceIDs_AreTheOptionalInstallableServices(t *testing.T) {
	var want []string
	for _, def := range config.DefaultServices(t.TempDir(), "nobody") {
		if def.Installable && !def.Required {
			want = append(want, def.ID)
		}
	}
	if !sameSet(ServiceIDs, want) {
		t.Errorf("ServiceIDs = %v, want %v", ServiceIDs, want)
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	sort.Strings(a)
	sort.Strings(b)
	return slices.Equal(a, b)
}
