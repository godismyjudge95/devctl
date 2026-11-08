// Package workers runs background processes for sites (queue workers, the
// scheduler, Vite, custom commands) under the services supervisor.
package workers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/node"
	"github.com/danielgormly/devctl/paths"
)

// Type names a kind of worker.
type Type string

// runtime is what a worker type runs its args with.
type runtime int

const (
	runtimePHP   runtime = iota // the site's php{version} wrapper
	runtimeNode                 // fnm exec with the site's Node version
	runtimeShell                // /bin/sh -c <command>
)

type kind struct {
	label   string
	runtime runtime
	args    []string
	// multiple allows more than one worker of this type on a site.
	multiple bool
}

var kinds = map[Type]kind{
	"queue":    {label: "Queue", runtime: runtimePHP, args: []string{"artisan", "queue:work"}},
	"horizon":  {label: "Horizon", runtime: runtimePHP, args: []string{"artisan", "horizon"}},
	"schedule": {label: "Scheduler", runtime: runtimePHP, args: []string{"artisan", "schedule:work"}},
	"vite":     {label: "Vite", runtime: runtimeNode, args: []string{"npm", "run", "dev"}},
	"custom":   {label: "Custom", runtime: runtimeShell, multiple: true},
}

// Types lists the worker types in display order.
var Types = []Type{"queue", "horizon", "schedule", "vite", "custom"}

// ErrInvalid marks a request the caller must fix (unknown type, bad command).
var ErrInvalid = errors.New("invalid worker")

// Validate checks the type and that only custom workers carry a command.
func Validate(t Type, command string) error {
	k, ok := kinds[t]
	if !ok {
		return fmt.Errorf("%w: unknown type %q (use %s)", ErrInvalid, t, typeList())
	}
	command = strings.TrimSpace(command)
	switch {
	case k.runtime == runtimeShell && command == "":
		return fmt.Errorf("%w: a %s worker needs a command", ErrInvalid, t)
	case k.runtime != runtimeShell && command != "":
		return fmt.Errorf("%w: a %s worker runs %q and takes no command; use a custom worker", ErrInvalid, t, display(t, "", ""))
	}
	return nil
}

// Multiple reports whether a site may have more than one worker of type t.
func (t Type) Multiple() bool { return kinds[t].multiple }

func typeList() string {
	names := make([]string, len(Types))
	for i, t := range Types {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
}

// argv is the full command line for a worker of the site in root.
func argv(serverRoot, root, phpVersion string, t Type, command string) ([]string, error) {
	k, ok := kinds[t]
	if !ok {
		return nil, fmt.Errorf("%w: unknown type %q", ErrInvalid, t)
	}
	switch k.runtime {
	case runtimePHP:
		php := filepath.Join(paths.BinDir(serverRoot), "php"+phpVersion)
		if _, err := os.Stat(php); err != nil {
			return nil, fmt.Errorf("PHP %s is not installed. Install it on the PHP page", phpVersion)
		}
		if _, err := os.Stat(filepath.Join(root, "artisan")); err != nil {
			return nil, fmt.Errorf("%s has no artisan file; a %s worker needs a Laravel app", root, t)
		}
		return append([]string{php}, k.args...), nil
	case runtimeNode:
		return node.Command(serverRoot, root, k.args...)
	default:
		return []string{"/bin/sh", "-c", command}, nil
	}
}

// display is the command a person would type to run the worker by hand.
func display(t Type, phpVersion, command string) string {
	k := kinds[t]
	switch k.runtime {
	case runtimePHP:
		return "php" + phpVersion + " " + strings.Join(k.args, " ")
	case runtimeNode:
		return strings.Join(k.args, " ")
	default:
		return command
	}
}
