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

	// Scan existing children on start.
	w.scanExisting(ctx, dir)

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}
			// Only care about Create events for direct children.
			if event.Op&fsnotify.Create == 0 {
				continue
			}
			if filepath.Dir(event.Name) != dir {
				continue
			}
			info, err := os.Stat(event.Name)
			if err != nil || !info.IsDir() {
				continue
			}
			if err := w.manager.AutoDiscover(ctx, event.Name); err != nil {
				log.Printf("watcher: auto-discover %s: %v", event.Name, err)
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
