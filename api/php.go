package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/services"
)

// configFilePath resolves the absolute path for an allowed PHP config file.
// Returns ("", false) if the filename is not in the allowlist.
func configFilePath(ver, serverRoot, file string) (string, bool) {
	switch file {
	case "php.ini":
		return php.PHPIniPath(ver, serverRoot), true
	case "php-fpm.conf":
		return php.FPMConfigPath(ver, serverRoot), true
	default:
		return "", false
	}
}

func (s *Server) handleGetPHPVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := php.InstalledVersions(s.serverRoot)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if versions == nil {
		versions = []php.Version{}
	}
	s.enrichPHPVersions(r.Context(), versions)
	writeJSON(w, versions)
}

func (s *Server) handleInstallPHP(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}

	if err := php.Install(r.Context(), ver, s.serverRoot, s.siteUser, s.siteHome); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.writePHPIni(r.Context(), ver, false); err != nil {
		log.Printf("php: write managed ini for %s: %v", ver, err)
	}

	// Register a supervised definition and start FPM for the new version.
	def := s.phpFPMServiceDef(ver)
	s.registry.Register(def)
	if _, statErr := os.Stat(def.ManagedCmd); statErr == nil {
		if err := s.supervisor.Start(def); err != nil {
			// Non-fatal: log but don't fail the install response.
			_ = err
		}
	}

	versions, _ := php.InstalledVersions(s.serverRoot)
	if versions == nil {
		versions = []php.Version{}
	}
	s.enrichPHPVersions(r.Context(), versions)
	go s.refreshPHPLatestManifest(context.Background())
	writeJSON(w, versions)
}

func (s *Server) handleUninstallPHP(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}

	// Stop and unregister the supervised process first.
	id := php.FPMServiceID(ver)
	_ = s.supervisor.Stop(id)
	s.registry.Unregister(id)

	if err := php.Uninstall(r.Context(), ver, s.serverRoot); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePHPFPMStart(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}
	def := s.phpFPMServiceDef(ver)
	if err := s.supervisor.Start(def); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePHPFPMStop(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}
	if err := s.supervisor.Stop(php.FPMServiceID(ver)); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePHPFPMRestart(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}
	def := s.phpFPMServiceDef(ver)
	if err := s.supervisor.Restart(def); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refreshPHPCABundle writes the combined CA bundle (OS CAs + Caddy local
// root) so PHP HTTPS to *.test succeeds. Safe to call whenever Caddy is up.
func (s *Server) refreshPHPCABundle() {
	var chain []byte
	if cert, err := s.caddy.CAChain(); err != nil {
		log.Printf("php: caddy ca chain: %v", err)
	} else {
		chain = cert
	}
	if err := php.ApplyCABundle(s.serverRoot, chain); err != nil {
		log.Printf("php: apply ca bundle: %v", err)
	}
}

// phpFPMServiceDef builds the supervised Definition for a PHP-FPM version.
func (s *Server) phpFPMServiceDef(ver string) services.Definition {
	versionCmd := php.FPMBinary(ver, s.serverRoot) + " -v"
	if patch, err := php.InstalledPatchVersion(ver, s.serverRoot); err == nil && patch != "" {
		versionCmd = fmt.Sprintf("printf 'PHP %s\n'", patch)
	}
	return services.Definition{
		ID:              php.FPMServiceID(ver),
		Label:           "PHP " + ver + " FPM",
		Managed:         true,
		ManagedCmd:      php.FPMBinary(ver, s.serverRoot),
		ManagedArgs:     fmt.Sprintf("-c %s/php.ini --nodaemonize --fpm-config %s", php.PHPDir(ver, s.serverRoot), php.FPMConfigPath(ver, s.serverRoot)),
		ManagedDir:      php.PHPDir(ver, s.serverRoot),
		ManagedExtraEnv: php.CertEnv(s.serverRoot),
		Log:             php.FPMLogPath(ver, s.serverRoot),
		Version:         versionCmd,
		VersionRegex:    `PHP (?P<version>[\d.]+)`,
		PreStart:        func() error { return php.StopLeftover(ver, s.serverRoot) },
	}
}

func (s *Server) enrichPHPVersions(ctx context.Context, versions []php.Version) {
	manifest := s.GetPHPLatestManifest()
	ds, err := listPHPIniDirectives(ctx, s.queries)
	if err != nil {
		log.Printf("php: list ini directives: %v", err)
	}
	for i, v := range versions {
		versions[i].Xdebug = php.XdebugStatusFor(v.Version, s.serverRoot, php.EffectiveDirectives(ds, v.Version))
		id := php.FPMServiceID(v.Version)
		if s.supervisor.IsRunning(id) {
			versions[i].Status = string(services.StatusRunning)
		} else {
			versions[i].Status = string(services.StatusStopped)
		}
		if patch, err := php.InstalledPatchVersion(v.Version, s.serverRoot); err == nil {
			versions[i].PatchVersion = patch
		}
		if manifest != nil && manifest.PHPVersions != nil {
			versions[i].LatestVersion = manifest.PHPVersions[v.Version]
			versions[i].UpdateAvail = php.UpdateAvailable(v.Version, versions[i].PatchVersion, s.serverRoot, manifest)
		}
	}
}

func (s *Server) refreshPHPLatestManifest(ctx context.Context) {
	manifest, err := php.LatestReleaseManifest(ctx)
	if err != nil {
		log.Printf("php-update-checker: %v", err)
		return
	}
	s.SetPHPLatestManifest(manifest)
	go s.poller.Poll()
}

// handleGetPHPConfig reads a PHP config file and returns its content as JSON.
func (s *Server) handleGetPHPConfig(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	file := r.PathValue("file")
	if ver == "" || file == "" {
		writeError(w, "version and file required", http.StatusBadRequest)
		return
	}
	path, ok := configFilePath(ver, s.serverRoot, file)
	if !ok {
		writeError(w, "file must be php.ini or php-fpm.conf", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"content": string(data)})
}

// handleSetPHPConfig writes content to a PHP config file.
func (s *Server) handleSetPHPConfig(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	file := r.PathValue("file")
	if ver == "" || file == "" {
		writeError(w, "version and file required", http.StatusBadRequest)
		return
	}
	path, ok := configFilePath(ver, s.serverRoot, file)
	if !ok {
		writeError(w, "file must be php.ini or php-fpm.conf", http.StatusBadRequest)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	unlock := php.LockIni()
	err := os.WriteFile(path, []byte(body.Content), 0644)
	unlock()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
