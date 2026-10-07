package elevate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/danielgormly/devctl/config"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

const elevatedEnv = "DEVCTL_ELEVATED"

// RunDaemon is the long-lived privileged supervisor. It only execs services
// with NeedsElevatedBind (Caddy today). The dashboard process stays unprivileged.
func RunDaemon() error {
	if os.Geteuid() != 0 && os.Getenv(elevatedEnv) != "1" {
		return fmt.Errorf("elevate daemon: must run as root or with %s=1 (systemd bind cap)", elevatedEnv)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.ServerRoot == "" {
		return fmt.Errorf("DEVCTL_SERVER_ROOT is required")
	}

	sup := services.NewSupervisor(cfg.ServerRoot)
	defs := map[string]services.Definition{}
	for _, def := range config.DefaultServices(cfg.ServerRoot, cfg.SiteUser) {
		if def.NeedsElevatedBind {
			defs[def.ID] = def
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)

	for _, def := range defs {
		if def.ManagedCmd == "" {
			continue
		}
		if _, err := os.Stat(def.ManagedCmd); err != nil {
			log.Printf("elevate daemon: skip %s (not installed)", def.ID)
			continue
		}
		if err := sup.Start(def); err != nil {
			log.Printf("elevate daemon: start %s: %v", def.ID, err)
		}
	}

	ln, err := listenControl(cfg.ServerRoot, cfg.SiteUser)
	if err != nil {
		return err
	}
	defer ln.Close()
	defer os.Remove(paths.ElevateSocket(cfg.ServerRoot))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		out := make([]map[string]any, 0, len(defs))
		for id := range defs {
			out = append(out, map[string]any{"id": id, "running": sup.IsRunning(id)})
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /services/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := defs[id]; !ok {
			http.Error(w, "unknown service", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"id": id, "running": sup.IsRunning(id)})
	})
	mux.HandleFunc("POST /services/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		def, ok := defs[r.PathValue("id")]
		if !ok {
			http.Error(w, "unknown service", http.StatusNotFound)
			return
		}
		if err := sup.Start(def); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /services/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := defs[id]; !ok {
			http.Error(w, "unknown service", http.StatusNotFound)
			return
		}
		if err := sup.Stop(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /services/{id}/restart", func(w http.ResponseWriter, r *http.Request) {
		def, ok := defs[r.PathValue("id")]
		if !ok {
			http.Error(w, "unknown service", http.StatusNotFound)
			return
		}
		if err := sup.Restart(def); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	srv := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
		cancel()
		_ = srv.Close()
		sup.StopAll()
		return nil
	case err := <-errCh:
		cancel()
		sup.StopAll()
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func listenControl(serverRoot, siteUser string) (net.Listener, error) {
	path := paths.ElevateSocket(serverRoot)
	if err := os.MkdirAll(paths.DevctlDir(serverRoot), 0755); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", path, err)
	}
	if err := os.Chmod(path, 0660); err != nil {
		ln.Close()
		return nil, err
	}
	if siteUser != "" {
		if u, err := user.Lookup(siteUser); err == nil {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			_ = os.Chown(path, uid, gid)
		}
	}
	log.Printf("elevate daemon: control socket %s", path)
	return ln, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
