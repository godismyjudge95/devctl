// Package newsite creates a new project in the sites root from a template
// (Laravel, Statamic, WordPress, plain PHP) and registers it as a site.
package newsite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Choice is one allowed value of an Option.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Option is one question a template asks. An option without Choices takes
// free text.
type Option struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Default string   `json:"default"`
	Choices []Choice `json:"choices,omitempty"`
}

// Tool names a program a template runs. The daemon resolves each to a path.
const (
	ToolLaravel  = "laravel"
	ToolStatamic = "statamic"
	ToolWP       = "wp"
)

// Template is one kind of project the wizard can create.
type Template struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Options     []Option `json:"options"`
	// Framework is stored on the site row (same values as sites.InspectPath).
	Framework string `json:"-"`
	PublicDir string `json:"-"`
	// Tools are the programs the steps run, besides PHP.
	Tools []string `json:"-"`
	steps func(p *Project) []Step
}

// Database option values. mysql and postgres are devctl services; sqlite is a
// file the Laravel installer creates.
const (
	dbSQLite   = "sqlite"
	dbMySQL    = "mysql"
	dbPostgres = "postgres"
)

var laravelDatabaseFlag = map[string]string{dbSQLite: "sqlite", dbMySQL: "mysql", dbPostgres: "pgsql"}

// Templates is the wizard's template table, in display order.
var Templates = []Template{
	{
		ID:          "laravel",
		Label:       "Laravel",
		Description: "A new Laravel application from the Laravel installer.",
		Framework:   "laravel",
		PublicDir:   "public",
		Tools:       []string{ToolLaravel},
		Options: []Option{
			{ID: "starter_kit", Label: "Starter kit", Default: "none", Choices: []Choice{
				{"none", "None"}, {"react", "React"}, {"vue", "Vue"}, {"svelte", "Svelte"}, {"livewire", "Livewire"},
			}},
			{ID: "testing", Label: "Testing framework", Default: "pest", Choices: []Choice{
				{"pest", "Pest"}, {"phpunit", "PHPUnit"},
			}},
			{ID: "database", Label: "Database", Default: dbSQLite, Choices: []Choice{
				{dbSQLite, "SQLite"}, {dbMySQL, "MySQL"}, {dbPostgres, "PostgreSQL"},
			}},
		},
		steps: laravelSteps,
	},
	{
		ID:          "statamic",
		Label:       "Statamic",
		Description: "A blank Statamic site from the Statamic CLI.",
		Framework:   "statamic",
		PublicDir:   "public",
		Tools:       []string{ToolStatamic},
		steps:       statamicSteps,
	},
	{
		ID:          "wordpress",
		Label:       "WordPress",
		Description: "WordPress with wp-config.php, a MySQL database, and an admin user.",
		Framework:   "wordpress",
		Tools:       []string{ToolWP},
		Options: []Option{
			{ID: "database", Label: "Database", Default: dbMySQL, Choices: []Choice{{dbMySQL, "MySQL"}}},
			{ID: "admin_user", Label: "Admin user", Default: "admin"},
			{ID: "admin_password", Label: "Admin password", Default: "password"},
			{ID: "admin_email", Label: "Admin email", Default: "admin@example.com"},
		},
		steps: wordpressSteps,
	},
	{
		ID:          "php",
		Label:       "Plain PHP",
		Description: "An empty directory with an index.php.",
		steps:       plainPHPSteps,
	},
}

// Lookup returns the template with the given ID.
func Lookup(id string) (*Template, bool) {
	for i := range Templates {
		if Templates[i].ID == id {
			return &Templates[i], true
		}
	}
	return nil, false
}

func laravelSteps(p *Project) []Step {
	args := []string{p.Tools[ToolPHP], p.Tools[ToolLaravel], "new", p.Name, "--no-interaction",
		"--database=" + laravelDatabaseFlag[p.Options["database"]], "--" + p.Options["testing"]}
	if kit := p.Options["starter_kit"]; kit != "none" {
		// Starter kit pages need built Vite assets.
		args = append(args, "--"+kit, "--npm")
	}
	steps := []Step{
		commandStep("Create the Laravel project", p.SitesRoot, args...),
		writeEnvStep(p),
	}
	if p.DB != nil {
		steps = append(steps, commandStep("Run the database migrations", p.Dir, p.Tools[ToolPHP], "artisan", "migrate", "--force"))
	}
	return steps
}

func statamicSteps(p *Project) []Step {
	return []Step{
		commandStep("Create the Statamic site", p.SitesRoot,
			p.Tools[ToolPHP], p.Tools[ToolStatamic], "new", p.Name, "--no-interaction"),
		writeEnvStep(p),
	}
}

func wordpressSteps(p *Project) []Step {
	wp := func(args ...string) []string {
		return append([]string{p.Tools[ToolPHP], p.Tools[ToolWP]}, args...)
	}
	db := p.DB
	return []Step{
		commandStep("Download WordPress", p.SitesRoot, wp("core", "download", "--path="+p.Dir)...),
		commandStep("Write wp-config.php", p.Dir, wp("config", "create",
			"--dbname="+db.Name, "--dbuser="+db.Server.Username, "--dbpass="+db.Server.Password,
			"--dbhost="+db.Server.Host+":"+db.Server.Port, "--skip-check")...),
		commandStep("Install WordPress and the admin user", p.Dir, wp("core", "install",
			"--url=https://"+p.Domain, "--title="+p.Name,
			"--admin_user="+p.Options["admin_user"], "--admin_password="+p.Options["admin_password"],
			"--admin_email="+p.Options["admin_email"], "--skip-email")...),
	}
}

func plainPHPSteps(p *Project) []Step {
	return []Step{
		{
			Label: "Create the project directory and index.php",
			Run: func(ctx context.Context, rt *Runtime) error {
				if err := os.Mkdir(p.Dir, 0o755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(p.Dir, "index.php"), []byte(plainIndexPHP(p.Name)), 0o644)
			},
		},
		writeEnvStep(p),
	}
}

func plainIndexPHP(name string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>%[1]s</title></head>
<body>
<h1>%[1]s</h1>
<p>PHP <?= PHP_VERSION ?> on <?= htmlspecialchars($_SERVER['HTTP_HOST'] ?? '') ?></p>
</body>
</html>
`, name)
}
