package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danielgormly/devctl/internal/versioncache"
)

// Lookup returns the tool with the given binary name.
func Lookup(name string) (Tool, bool) {
	return lookupIn(AllTools, name)
}

func lookupIn(list []Tool, name string) (Tool, bool) {
	for _, t := range list {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// States returns the catalog snapshot for every registered tool.
func States(ctx context.Context, binDir string, latest map[string]string) []State {
	return statesFor(ctx, AllTools, binDir, latest)
}

func statesFor(ctx context.Context, list []Tool, binDir string, latest map[string]string) []State {
	out := make([]State, 0, len(list))
	for _, t := range list {
		out = append(out, stateFor(ctx, t, binDir, latest[t.Name]))
	}
	return out
}

func stateFor(ctx context.Context, t Tool, binDir, latest string) State {
	label := t.Label
	if label == "" {
		label = t.Name
	}
	binPath := filepath.Join(binDir, t.Name)
	ver := ""
	if t.InstalledVersion != nil {
		ver = t.InstalledVersion(ctx, binPath)
	}
	installed := ver != "" || isPresent(t, binDir)
	return State{
		ID:              t.Name,
		Label:           label,
		Description:     t.Description,
		Homepage:        t.Homepage,
		Aliases:         t.Aliases,
		Installed:       installed,
		Default:         t.Default,
		Version:         ver,
		LatestVersion:   latest,
		UpdateAvailable: installed && versioncache.Available(ver, latest),
	}
}

func isPresent(t Tool, binDir string) bool {
	_, err := os.Stat(filepath.Join(binDir, t.Name))
	return err == nil
}

// Uninstall removes the binary and alias symlinks. Default tools cannot be
// uninstalled.
func Uninstall(t Tool, binDir string) error {
	if t.Default {
		return fmt.Errorf("%s is a default helper and cannot be uninstalled", t.Name)
	}
	for _, alias := range t.Aliases {
		_ = os.Remove(filepath.Join(binDir, alias))
	}
	binPath := filepath.Join(binDir, t.Name)
	if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: remove: %w", t.Name, err)
	}
	return nil
}
