package newsite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/internal/runuser"
)

// Host is what the daemon does for the wizard.
type Host interface {
	Register(ctx context.Context, p *Project) error
	Unregister(ctx context.Context, p *Project) error
	CreateDatabase(ctx context.Context, engine, name string) error
	DropDatabase(ctx context.Context, engine, name string) error
}

// Executor runs one program with its output going to out.
type Executor interface {
	Run(ctx context.Context, out io.Writer, dir string, argv []string) error
}

// Runtime is what a step runs against.
type Runtime struct {
	Host Host
	Exec Executor
	Out  io.Writer
	// OnStep, when set, is called before each step (1-based index).
	OnStep func(index, total int, label string)
}

// Step is one unit of work. Undo, when set, reverses a step that completed.
type Step struct {
	Label string
	Run   func(ctx context.Context, rt *Runtime) error
	Undo  func(ctx context.Context, rt *Runtime) error
}

// Steps is the full pipeline for p. The site is registered before its
// directory exists, so the sites watcher finds the domain taken and does not
// auto-discover the half-built directory with guessed settings.
func (p *Project) Steps() []Step {
	steps := []Step{registerStep(p)}
	if p.DB != nil {
		steps = append(steps, databaseStep(p.DB))
	}
	return append(steps, p.Template.steps(p)...)
}

// Run executes the pipeline. When a step fails, the completed steps are undone
// in reverse order, so a failed run leaves no site, directory, or database.
func Run(ctx context.Context, p *Project, rt Runtime) error {
	steps := p.Steps()
	var done []Step
	for i, st := range steps {
		if rt.OnStep != nil {
			rt.OnStep(i+1, len(steps), st.Label)
		}
		fmt.Fprintf(rt.Out, "==> %s\n", st.Label)
		if err := st.Run(ctx, &rt); err != nil {
			err = fmt.Errorf("%s: %w", st.Label, err)
			return errors.Join(err, undo(context.WithoutCancel(ctx), &rt, done))
		}
		done = append(done, st)
	}
	return nil
}

func undo(ctx context.Context, rt *Runtime, done []Step) error {
	var errs []error
	for i := len(done) - 1; i >= 0; i-- {
		if done[i].Undo == nil {
			continue
		}
		fmt.Fprintf(rt.Out, "==> Undo: %s\n", done[i].Label)
		if err := done[i].Undo(ctx, rt); err != nil {
			errs = append(errs, fmt.Errorf("undo %s: %w", done[i].Label, err))
		}
	}
	return errors.Join(errs...)
}

// registerStep owns the project directory: undo removes it too, because a
// failed installer can leave a partial directory behind.
func registerStep(p *Project) Step {
	return Step{
		Label: "Register " + p.Domain,
		Run:   func(ctx context.Context, rt *Runtime) error { return rt.Host.Register(ctx, p) },
		Undo: func(ctx context.Context, rt *Runtime) error {
			return errors.Join(rt.Host.Unregister(ctx, p), os.RemoveAll(p.Dir))
		},
	}
}

func databaseStep(db *Database) Step {
	return Step{
		Label: fmt.Sprintf("Create the %s database %s", db.Engine, db.Name),
		Run: func(ctx context.Context, rt *Runtime) error {
			return rt.Host.CreateDatabase(ctx, db.Engine, db.Name)
		},
		Undo: func(ctx context.Context, rt *Runtime) error {
			return rt.Host.DropDatabase(ctx, db.Engine, db.Name)
		},
	}
}

func commandStep(label, dir string, argv ...string) Step {
	return Step{
		Label: label,
		Run: func(ctx context.Context, rt *Runtime) error {
			return rt.Exec.Run(ctx, rt.Out, dir, argv)
		},
	}
}

func writeEnvStep(p *Project) Step {
	return Step{
		Label: "Write .env",
		Run: func(ctx context.Context, rt *Runtime) error {
			path := filepath.Join(p.Dir, ".env")
			old, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			return os.WriteFile(path, []byte(MergeEnv(string(old), p.DotEnv())), 0o644)
		},
	}
}

// UserExecutor runs commands as the site user with the devctl bin directory
// first on PATH, so `php` and `composer` inside the installers are devctl's.
type UserExecutor struct {
	User, Home, BinDir string
}

func (x UserExecutor) Run(ctx context.Context, out io.Writer, dir string, argv []string) error {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := "PATH=" + shellQuote(x.BinDir) + `:"$PATH" ` + strings.Join(quoted, " ")
	_, err := runuser.RunAsUserW(ctx, out, x.User, x.Home, dir, cmd)
	return err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
