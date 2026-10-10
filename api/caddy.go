package api

import (
	"context"
	"log"
	"sync/atomic"
	"time"
)

// pushCaddyConfig waits for the Admin API, then writes the HTTP server
// block and syncs every vhost. Safe to call from start, install, and restore.
func (s *Server) pushCaddyConfig(reason string) {
	if err := s.caddy.WaitForAdmin(10 * time.Second); err != nil {
		log.Printf("%s: admin not ready: %v", reason, err)
		return
	}
	if err := s.caddy.EnsureHTTPServer(s.devctlAddr); err != nil {
		log.Printf("%s: ensure http server: %v", reason, err)
	}
	s.refreshPHPCABundle()
	if err := s.siteManager.SyncAll(context.Background()); err != nil {
		log.Printf("%s: sync sites: %v", reason, err)
	}
}

// WatchCaddyConfig re-pushes vhosts when Caddy is up but serving an empty
// config. That happens after a crash restart of `caddy run` without --resume,
// including on macOS where make install does not restart the elevate daemon.
func (s *Server) WatchCaddyConfig(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	var busy atomic.Bool
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.caddy.HasHTTPServer() {
				continue
			}
			if err := s.caddy.WaitForAdmin(time.Second); err != nil {
				continue
			}
			if !busy.CompareAndSwap(false, true) {
				continue
			}
			log.Printf("caddy: http server missing — restoring config")
			s.pushCaddyConfig("caddy restore")
			busy.Store(false)
		}
	}
}
