package elevate

import (
	"os"
)

// Status describes which elevate targets are currently satisfied.
type Status struct {
	TrustConfigured    bool   `json:"trust_configured"`
	ResolverConfigured bool   `json:"resolver_configured"`
	PortsConfigured    bool   `json:"ports_configured"`
	UnitPath           string `json:"unit_path"`
	UnitRunsAsUser     bool   `json:"unit_runs_as_user"`
	RunningAsRoot      bool   `json:"running_as_root"`
	CAPath             string `json:"ca_path"`
	ResolverPath       string `json:"resolver_path"`
	Notes              []string `json:"notes,omitempty"`
}

// CollectStatus gathers elevate status without requiring root.
// caFingerprint is optional; when set, trust is checked against it.
func CollectStatus(caFingerprint string) Status {
	st := Status{
		TrustConfigured:    CATrusted(caFingerprint),
		ResolverConfigured: ResolverConfigured(),
		PortsConfigured:    UnitHasAmbientBind(ServiceUnitPath),
		UnitPath:           ServiceUnitPath,
		UnitRunsAsUser:     UnitRunsAsUser(ServiceUnitPath),
		RunningAsRoot:      os.Geteuid() == 0,
		CAPath:             InstallCAPath(),
		ResolverPath:       ResolvedDropinFile,
	}
	if !st.PortsConfigured {
		st.Notes = append(st.Notes, "run: sudo devctl elevate ports")
	}
	if !st.ResolverConfigured {
		st.Notes = append(st.Notes, "run: sudo devctl elevate resolver")
	}
	if !st.TrustConfigured {
		st.Notes = append(st.Notes, "run: sudo devctl elevate trust")
	}
	return st
}
