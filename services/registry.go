package services

import (
	"github.com/danielgormly/devctl/config"
)

// Registry holds the static set of service definitions.
// Definitions are sourced from config.DefaultServices() at startup — there is
// no YAML file at runtime.
type Registry struct {
	defs []Definition
}

// NewRegistry converts a slice of config.ServiceDef into an in-memory Registry.
func NewRegistry(defs []config.ServiceDef) *Registry {
	out := make([]Definition, 0, len(defs))
	for _, d := range defs {
		out = append(out, Definition{
			ID:              d.ID,
			Label:           d.Label,
			Start:           d.Start,
			Stop:            d.Stop,
			Restart:         d.Restart,
			Status:          d.Status,
			StatusRegex:     d.StatusRegex,
			Version:         d.Version,
			VersionRegex:    d.VersionRegex,
			Log:             d.Log,
			CredentialsFile: d.CredentialsFile,
			Installable:     d.Installable,
			Required:        d.Required,
			Managed:         d.Managed,
			ManagedCmd:      d.ManagedCmd,
			ManagedArgs:     d.ManagedArgs,
			ManagedDir:      d.ManagedDir,
			ManagedEnvFile:  d.ManagedEnvFile,
		})
	}
	return &Registry{defs: out}
}

// All returns all loaded service definitions.
func (r *Registry) All() []Definition {
	return r.defs
}

// Get returns a definition by ID, or false if not found.
func (r *Registry) Get(id string) (Definition, bool) {
	for _, d := range r.defs {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}
