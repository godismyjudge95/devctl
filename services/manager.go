package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const cmdTimeout = 10 * time.Second

// Manager runs service commands and parses their output.
type Manager struct {
	registry   *Registry
	supervisor *Supervisor
	// installers is populated after construction to break an import cycle.
	// It maps service ID → IsInstalled func.
	installers map[string]func() bool
}

// NewManager creates a Manager backed by the given Registry and Supervisor.
func NewManager(registry *Registry, supervisor *Supervisor) *Manager {
	return &Manager{
		registry:   registry,
		supervisor: supervisor,
		installers: make(map[string]func() bool),
	}
}

// SetInstallerCheck registers an IsInstalled func for a service ID.
// Called from main.go after the install registry is built.
func (m *Manager) SetInstallerCheck(id string, fn func() bool) {
	m.installers[id] = fn
}

// GetStatus returns the running Status for a service.
func (m *Manager) GetStatus(def Definition) Status {
	if def.Managed {
		if m.supervisor.IsRunning(def.ID) {
			if def.HealthCheck != "" {
				if _, err := runCommand(def.HealthCheck); err != nil {
					return StatusWarning
				}
			}
			return StatusRunning
		}
		return StatusStopped
	}

	if def.Status == "" {
		return StatusUnknown
	}

	out, err := runCommand(def.Status)
	if err != nil && out == "" {
		return StatusUnknown
	}

	if def.StatusRegex == "" {
		if err == nil {
			return StatusRunning
		}
		return StatusStopped
	}

	re, err2 := regexp.Compile(def.StatusRegex)
	if err2 != nil {
		return StatusUnknown
	}

	match := re.FindStringSubmatch(out)
	idx := re.SubexpIndex("status")
	if idx < 0 || int(idx) >= len(match) {
		return StatusUnknown
	}

	switch strings.TrimSpace(match[idx]) {
	case "active":
		return StatusRunning
	case "inactive", "failed", "dead":
		return StatusStopped
	case "activating", "deactivating", "reloading":
		return StatusPending
	default:
		return StatusUnknown
	}
}

// GetVersion runs the version command and returns the parsed version string.
func (m *Manager) GetVersion(def Definition) string {
	if def.Version == "" {
		return ""
	}

	out, err := runCommand(def.Version)
	if err != nil {
		// Tolerate non-zero exit codes (some binaries, e.g. typesense-server
		// and mailpit, print the version but exit 1). Only bail on exec
		// failures (binary not found, permission denied, etc.).
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return ""
		}
	}

	if def.VersionRegex == "" {
		return strings.TrimSpace(out)
	}

	re, err := regexp.Compile(def.VersionRegex)
	if err != nil {
		return ""
	}

	match := re.FindStringSubmatch(out)
	idx := re.SubexpIndex("version")
	if idx < 0 || int(idx) >= len(match) {
		return ""
	}

	return strings.TrimSpace(match[idx])
}

// Start starts a service via the supervisor (Managed) or a shell command.
func (m *Manager) Start(def Definition) error {
	if def.Managed {
		return m.supervisor.Start(def)
	}
	if def.Start == "" {
		return fmt.Errorf("no start command defined for %s", def.ID)
	}
	_, err := runCommand(def.Start)
	return err
}

// Stop stops a service via the supervisor (Managed) or a shell command.
func (m *Manager) Stop(def Definition) error {
	if def.Managed {
		return m.supervisor.Stop(def.ID)
	}
	if def.Stop == "" {
		return fmt.Errorf("no stop command defined for %s", def.ID)
	}
	_, err := runCommand(def.Stop)
	return err
}

// Restart restarts a service via the supervisor (Managed) or a shell command.
func (m *Manager) Restart(def Definition) error {
	if def.Managed {
		return m.supervisor.Restart(def)
	}
	if def.Restart == "" {
		return fmt.Errorf("no restart command defined for %s", def.ID)
	}
	_, err := runCommand(def.Restart)
	return err
}

// GetState returns the full ServiceState for a definition.
func (m *Manager) GetState(def Definition) ServiceState {
	installed := !def.Installable
	if fn, ok := m.installers[def.ID]; ok {
		installed = fn()
	}
	version := ""
	if installed {
		version = m.GetVersion(def)
	}
	return ServiceState{
		ID:             def.ID,
		Label:          def.Label,
		Status:         m.GetStatus(def),
		Version:        version,
		Log:            def.Log,
		Installed:      installed,
		Installable:    def.Installable,
		Required:       def.Required,
		Description:    def.Description,
		InstallVersion: def.InstallVersion,
		HasCredentials: def.HasCredentials,
	}
}

// runCommand runs a shell command with a timeout and returns combined output.
func runCommand(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return buf.String(), err
}
