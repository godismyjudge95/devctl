package sites

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a directory for new subdirectories and auto-discovers sites.
type Watcher struct {
	manager *Manager
	watcher *fsnotify.Watcher
}

// NewWatcher creates a Watcher backed by the given site Manager.
func NewWatcher(manager *Manager) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{manager: manager, watcher: fw}, nil
}

// Watch starts watching dir for new direct-child directories. It blocks
// until ctx is cancelled.
func (w *Watcher) Watch(ctx context.Context, dir string) error {
	// Ensure the directory exists.
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := w.watcher.Add(dir); err != nil {
		return err
	}
	defer w.watcher.Close()

	// Scan existing children on start (also prunes stale entries).
	w.scanExisting(ctx, dir)

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}
			if filepath.Dir(event.Name) != dir {
				continue
			}

			switch {
			case event.Op&fsnotify.Create != 0:
				// New directory — auto-discover as a site.
				info, err := os.Stat(event.Name)
				if err != nil || !info.IsDir() {
					continue
				}
				if err := w.manager.AutoDiscover(ctx, event.Name); err != nil {
					log.Printf("watcher: auto-discover %s: %v", event.Name, err)
				}

			case event.Op&(fsnotify.Remove|fsnotify.Rename) != 0:
				// Directory removed or renamed — clean up the site if tracked.
				w.manager.RemoveIfMissing(ctx, event.Name)
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("watcher: %v", err)
		}
	}
}

func (w *Watcher) scanExisting(ctx context.Context, dir string) {
	// First, remove any tracked sites whose directories no longer exist.
	w.manager.PruneStale(ctx)

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("watcher: read dir %s: %v", dir, err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if err := w.manager.AutoDiscover(ctx, full); err != nil {
			log.Printf("watcher: auto-discover %s: %v", full, err)
		}
	}
}
