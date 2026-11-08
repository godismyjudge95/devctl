package newsite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTemplates_TableIsWellFormed(t *testing.T) {
	want := []string{"laravel", "statamic", "wordpress", "php"}
	if got := templateIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("template IDs = %v, want %v", got, want)
	}
	for _, tpl := range Templates {
		if tpl.Label == "" || tpl.Description == "" || tpl.steps == nil {
			t.Errorf("%s: label, description, and steps are required", tpl.ID)
		}
		seen := map[string]bool{}
		for _, o := range tpl.Options {
			if seen[o.ID] {
				t.Errorf("%s: duplicate option %s", tpl.ID, o.ID)
			}
			seen[o.ID] = true
			if len(o.Choices) > 0 && !hasChoice(o.Choices, o.Default) {
				t.Errorf("%s.%s: default %q is not a choice", tpl.ID, o.ID, o.Default)
			}
			if len(o.Choices) == 0 && o.Default == "" {
				t.Errorf("%s.%s: a text option needs a default", tpl.ID, o.ID)
			}
		}
		if seen["database"] {
			for _, c := range optionByID(t, tpl.ID, "database").Choices {
				if _, ok := laravelDatabaseFlag[c.Value]; !ok {
					t.Errorf("%s: database choice %q is not a known engine", tpl.ID, c.Value)
				}
			}
		}
	}
}

func TestTemplates_LaravelChoices(t *testing.T) {
	cases := map[string][]string{
		"starter_kit": {"none", "react", "vue", "svelte", "livewire"},
		"testing":     {"pest", "phpunit"},
		"database":    {"sqlite", "mysql", "postgres"},
	}
	for id, want := range cases {
		if got := choiceValues(optionByID(t, "laravel", id).Choices); !reflect.DeepEqual(got, want) {
			t.Errorf("laravel %s = %v, want %v", id, got, want)
		}
	}
}

func optionByID(t *testing.T, tplID, optID string) Option {
	t.Helper()
	tpl, ok := Lookup(tplID)
	if !ok {
		t.Fatalf("template %s missing", tplID)
	}
	for _, o := range tpl.Options {
		if o.ID == optID {
			return o
		}
	}
	t.Fatalf("%s: option %s missing", tplID, optID)
	return Option{}
}

func TestParse_AppliesDefaultsAndDerivesPaths(t *testing.T) {
	p, err := Parse(Request{Name: " my-app ", Template: "laravel"}, "/sites")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "my-app" || p.Domain != "my-app.test" || p.Dir != filepath.Join("/sites", "my-app") {
		t.Errorf("name/domain/dir = %q %q %q", p.Name, p.Domain, p.Dir)
	}
	want := map[string]string{"starter_kit": "none", "testing": "pest", "database": "sqlite"}
	if !reflect.DeepEqual(p.Options, want) {
		t.Errorf("options = %v, want %v", p.Options, want)
	}
	if p.DB != nil {
		t.Errorf("sqlite must not create a server database, got %+v", p.DB)
	}
}

func TestParse_ServerDatabaseName(t *testing.T) {
	for _, engine := range []string{"mysql", "postgres"} {
		p, err := Parse(Request{Name: "my-shop", Template: "laravel", Options: map[string]string{"database": engine}}, "/sites")
		if err != nil {
			t.Fatal(err)
		}
		if p.DB == nil || p.DB.Engine != engine || p.DB.Name != "my_shop" {
			t.Errorf("%s: DB = %+v, want engine %s name my_shop", engine, p.DB, engine)
		}
	}
	p, err := Parse(Request{Name: "blog", Template: "wordpress"}, "/sites")
	if err != nil {
		t.Fatal(err)
	}
	if p.DB == nil || p.DB.Engine != "mysql" {
		t.Errorf("wordpress DB = %+v, want mysql", p.DB)
	}
}

func TestParse_RejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		want string
	}{
		{"empty name", Request{Name: "", Template: "php"}, "invalid name"},
		{"uppercase", Request{Name: "MyApp", Template: "php"}, "invalid name"},
		{"underscore", Request{Name: "my_app", Template: "php"}, "invalid name"},
		{"leading hyphen", Request{Name: "-app", Template: "php"}, "invalid name"},
		{"path", Request{Name: "../etc", Template: "php"}, "invalid name"},
		{"too long", Request{Name: strings.Repeat("a", 64), Template: "php"}, "invalid name"},
		{"reserved", Request{Name: "server", Template: "php"}, "reserved"},
		{"unknown template", Request{Name: "app", Template: "rails"}, "unknown template"},
		{"unknown option", Request{Name: "app", Template: "php", Options: map[string]string{"database": "mysql"}}, "unknown option"},
		{"bad choice", Request{Name: "app", Template: "laravel", Options: map[string]string{"starter_kit": "angular"}}, "invalid starter_kit"},
		{"control char", Request{Name: "app", Template: "wordpress", Options: map[string]string{"admin_user": "a\nb"}}, "control characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.req, "/sites")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMergeEnv(t *testing.T) {
	cases := []struct {
		name, in string
		vars     []EnvVar
		want     string
	}{
		{"empty file", "", []EnvVar{{"APP_URL", "https://a.test"}}, "APP_URL=https://a.test\n"},
		{"replace in place", "APP_NAME=x\nAPP_URL=http://localhost\nAPP_KEY=k\n",
			[]EnvVar{{"APP_URL", "https://a.test"}}, "APP_NAME=x\nAPP_URL=https://a.test\nAPP_KEY=k\n"},
		{"uncomment", "DB_CONNECTION=sqlite\n# DB_HOST=127.0.0.1\n# DB_PORT=3306\n",
			[]EnvVar{{"DB_CONNECTION", "mysql"}, {"DB_HOST", "127.0.0.1"}},
			"DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\n# DB_PORT=3306\n"},
		{"active line wins over comment", "# MAIL_PORT=2525\nMAIL_PORT=587\n",
			[]EnvVar{{"MAIL_PORT", "1025"}}, "# MAIL_PORT=2525\nMAIL_PORT=1025\n"},
		{"prefix is not a match", "DB_HOST_EXTRA=1\n", []EnvVar{{"DB_HOST", "h"}}, "DB_HOST_EXTRA=1\nDB_HOST=h\n"},
		{"append keeps order", "A=1", []EnvVar{{"C", "3"}, {"B", "2"}}, "A=1\nC=3\nB=2\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MergeEnv(tc.in, tc.vars); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDotEnv(t *testing.T) {
	p, err := Parse(Request{Name: "shop", Template: "laravel", Options: map[string]string{"database": "postgres"}}, "/sites")
	if err != nil {
		t.Fatal(err)
	}
	p.DB.Server = DBServer{Connection: "pgsql", Host: "127.0.0.1", Port: "5432", Username: "root", Password: "pw"}
	p.ServiceEnv = EnvVarsFromMap(map[string]string{"MAIL_PORT": "1025", "MAIL_HOST": "127.0.0.1"})
	want := []EnvVar{
		{"APP_URL", "https://shop.test"},
		{"DB_CONNECTION", "pgsql"}, {"DB_HOST", "127.0.0.1"}, {"DB_PORT", "5432"},
		{"DB_DATABASE", "shop"}, {"DB_USERNAME", "root"}, {"DB_PASSWORD", "pw"},
		{"MAIL_HOST", "127.0.0.1"}, {"MAIL_PORT", "1025"},
	}
	if got := p.DotEnv(); !reflect.DeepEqual(got, want) {
		t.Errorf("DotEnv =\n%v\nwant\n%v", got, want)
	}
}

// recorder logs host calls and commands in one ordered list.
type recorder struct {
	log         []string
	registerErr error
	// onCmd simulates an installer: it runs for each command and may fail.
	onCmd func(dir string, argv []string) error
}

func (r *recorder) Register(_ context.Context, p *Project) error {
	r.log = append(r.log, "register "+p.Domain)
	return r.registerErr
}
func (r *recorder) Unregister(_ context.Context, p *Project) error {
	r.log = append(r.log, "unregister "+p.Domain)
	return nil
}
func (r *recorder) CreateDatabase(_ context.Context, engine, name string) error {
	r.log = append(r.log, "create-db "+engine+" "+name)
	return nil
}
func (r *recorder) DropDatabase(_ context.Context, engine, name string) error {
	r.log = append(r.log, "drop-db "+engine+" "+name)
	return nil
}
func (r *recorder) Run(_ context.Context, _ io.Writer, dir string, argv []string) error {
	r.log = append(r.log, "exec "+dir+": "+strings.Join(argv, " "))
	if r.onCmd != nil {
		return r.onCmd(dir, argv)
	}
	return nil
}

func newProject(t *testing.T, req Request) *Project {
	t.Helper()
	p, err := Parse(req, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.Tools = map[string]string{ToolPHP: "/bin/php", ToolLaravel: "/g/laravel", ToolStatamic: "/g/statamic", ToolWP: "/bin/wp"}
	if p.DB != nil {
		p.DB.Server = DBServer{Connection: "mysql", Host: "127.0.0.1", Port: "3306", Username: "root", Password: ""}
	}
	p.ServiceEnv = []EnvVar{{"MAIL_HOST", "127.0.0.1"}, {"REDIS_HOST", "127.0.0.1"}}
	return p
}

func run(p *Project, r *recorder) (string, error) {
	var out bytes.Buffer
	err := Run(context.Background(), p, Runtime{Host: r, Exec: r, Out: &out})
	return out.String(), err
}

// fakeLaravelNew creates the project directory and the .env the installer writes.
func fakeLaravelNew(p *Project) func(string, []string) error {
	return func(dir string, argv []string) error {
		if len(argv) > 2 && argv[2] == "new" {
			if err := os.Mkdir(p.Dir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(p.Dir, ".env"), []byte("APP_URL=http://localhost\nDB_CONNECTION=sqlite\n# DB_HOST=127.0.0.1\n"), 0o644)
		}
		return nil
	}
}

func TestRun_LaravelMySQL(t *testing.T) {
	p := newProject(t, Request{Name: "my-app", Template: "laravel",
		Options: map[string]string{"starter_kit": "react", "testing": "phpunit", "database": "mysql"}})
	r := &recorder{onCmd: fakeLaravelNew(p)}
	if _, err := run(p, r); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"register my-app.test",
		"create-db mysql my_app",
		"exec " + p.SitesRoot + ": /bin/php /g/laravel new my-app --no-interaction --database=mysql --phpunit --react --npm",
		"exec " + p.Dir + ": /bin/php artisan migrate --force",
	}
	if !reflect.DeepEqual(r.log, want) {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(r.log, "\n"), strings.Join(want, "\n"))
	}
	env, _ := os.ReadFile(filepath.Join(p.Dir, ".env"))
	for _, line := range []string{"APP_URL=https://my-app.test", "DB_CONNECTION=mysql", "DB_HOST=127.0.0.1", "DB_DATABASE=my_app", "MAIL_HOST=127.0.0.1", "REDIS_HOST=127.0.0.1"} {
		if !strings.Contains(string(env), line+"\n") {
			t.Errorf(".env missing %q:\n%s", line, env)
		}
	}
}

func TestRun_LaravelSQLiteDefaults(t *testing.T) {
	p := newProject(t, Request{Name: "app", Template: "laravel"})
	r := &recorder{onCmd: fakeLaravelNew(p)}
	if _, err := run(p, r); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"register app.test",
		"exec " + p.SitesRoot + ": /bin/php /g/laravel new app --no-interaction --database=sqlite --pest",
	}
	if !reflect.DeepEqual(r.log, want) {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(r.log, "\n"), strings.Join(want, "\n"))
	}
	env, _ := os.ReadFile(filepath.Join(p.Dir, ".env"))
	if !strings.Contains(string(env), "DB_CONNECTION=sqlite\n") || strings.Contains(string(env), "DB_DATABASE") {
		t.Errorf("sqlite .env must keep the installer's database settings:\n%s", env)
	}
}

func TestRun_Statamic(t *testing.T) {
	p := newProject(t, Request{Name: "docs", Template: "statamic"})
	r := &recorder{onCmd: func(string, []string) error { return os.Mkdir(p.Dir, 0o755) }}
	if _, err := run(p, r); err != nil {
		t.Fatal(err)
	}
	want := "exec " + p.SitesRoot + ": /bin/php /g/statamic new docs --no-interaction"
	if len(r.log) != 2 || r.log[1] != want {
		t.Fatalf("calls = %v, want register then %q", r.log, want)
	}
	if env, _ := os.ReadFile(filepath.Join(p.Dir, ".env")); !strings.Contains(string(env), "APP_URL=https://docs.test\n") {
		t.Errorf(".env = %q", env)
	}
}

func TestRun_WordPress(t *testing.T) {
	p := newProject(t, Request{Name: "blog", Template: "wordpress",
		Options: map[string]string{"admin_user": "daniel", "admin_password": "it's secret"}})
	r := &recorder{}
	if _, err := run(p, r); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"register blog.test",
		"create-db mysql blog",
		"exec " + p.SitesRoot + ": /bin/php /bin/wp core download --path=" + p.Dir,
		"exec " + p.Dir + ": /bin/php /bin/wp config create --dbname=blog --dbuser=root --dbpass= --dbhost=127.0.0.1:3306 --skip-check",
		"exec " + p.Dir + ": /bin/php /bin/wp core install --url=https://blog.test --title=blog --admin_user=daniel --admin_password=it's secret --admin_email=admin@example.com --skip-email",
	}
	if !reflect.DeepEqual(r.log, want) {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(r.log, "\n"), strings.Join(want, "\n"))
	}
}

func TestRun_PlainPHP(t *testing.T) {
	p := newProject(t, Request{Name: "hello", Template: "php"})
	r := &recorder{}
	out, err := run(p, r)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.log, []string{"register hello.test"}) {
		t.Errorf("plain PHP must not run commands: %v", r.log)
	}
	index, err := os.ReadFile(filepath.Join(p.Dir, "index.php"))
	if err != nil || !strings.Contains(string(index), "<?= PHP_VERSION ?>") {
		t.Errorf("index.php = %q, %v", index, err)
	}
	if env, _ := os.ReadFile(filepath.Join(p.Dir, ".env")); string(env) != "APP_URL=https://hello.test\nMAIL_HOST=127.0.0.1\nREDIS_HOST=127.0.0.1\n" {
		t.Errorf(".env = %q", env)
	}
	if !strings.Contains(out, "==> Write .env\n") {
		t.Errorf("progress output = %q", out)
	}
}

func TestRun_OnStepReportsEveryStep(t *testing.T) {
	p := newProject(t, Request{Name: "hello", Template: "php"})
	var got []string
	rt := Runtime{Host: &recorder{}, Exec: &recorder{}, Out: io.Discard, OnStep: func(i, n int, label string) {
		got = append(got, fmt.Sprintf("%d %d %s", i, n, label))
	}}
	if err := Run(context.Background(), p, rt); err != nil {
		t.Fatal(err)
	}
	want := []string{"1 3 Register hello.test", "2 3 Create the project directory and index.php", "3 3 Write .env"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}

func TestRun_FailureUndoesCompletedSteps(t *testing.T) {
	p := newProject(t, Request{Name: "shop", Template: "laravel", Options: map[string]string{"database": "postgres"}})
	r := &recorder{onCmd: func(dir string, argv []string) error {
		// The installer fails after it created part of the project.
		if err := os.MkdirAll(filepath.Join(p.Dir, "vendor"), 0o755); err != nil {
			return err
		}
		return errors.New("exit status 1")
	}}
	_, err := run(p, r)
	if err == nil || !strings.Contains(err.Error(), "Create the Laravel project: exit status 1") {
		t.Fatalf("err = %v", err)
	}
	want := []string{
		"register shop.test",
		"create-db postgres shop",
		"exec " + p.SitesRoot + ": /bin/php /g/laravel new shop --no-interaction --database=pgsql --pest",
		"drop-db postgres shop",
		"unregister shop.test",
	}
	if !reflect.DeepEqual(r.log, want) {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(r.log, "\n"), strings.Join(want, "\n"))
	}
	if _, err := os.Stat(p.Dir); !os.IsNotExist(err) {
		t.Errorf("project directory still exists after a failed run: %v", err)
	}
}

func TestRun_RegisterFailureLeavesExistingDirectory(t *testing.T) {
	p := newProject(t, Request{Name: "taken", Template: "php"})
	if err := os.Mkdir(p.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &recorder{registerErr: errors.New("UNIQUE constraint failed: sites.domain")}
	if _, err := run(p, r); err == nil {
		t.Fatal("want an error")
	}
	if !reflect.DeepEqual(r.log, []string{"register taken.test"}) {
		t.Errorf("calls = %v: nothing may be undone when registration fails", r.log)
	}
	if _, err := os.Stat(p.Dir); err != nil {
		t.Errorf("a directory the run does not own was removed: %v", err)
	}
}

func TestUserExecutor_QuotesArgsAndPutsBinDirFirst(t *testing.T) {
	bin := t.TempDir()
	probe := filepath.Join(bin, "devctl-newsite-probe")
	if err := os.WriteFile(probe, []byte("#!/bin/sh\nprintf '%s|' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	work := t.TempDir()
	var out bytes.Buffer
	x := UserExecutor{BinDir: bin, Home: home}
	if err := x.Run(context.Background(), &out, work, []string{"devctl-newsite-probe", "it's", "$HOME", "a b"}); err != nil {
		t.Fatalf("run: %v (output %q)", err, out.String())
	}
	if got := out.String(); got != "it's|$HOME|a b|" {
		t.Errorf("output = %q", got)
	}
}
