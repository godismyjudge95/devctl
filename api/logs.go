package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgormly/devctl/paths"
)

// LogFileInfo describes a single log file in the central logs directory.
type LogFileInfo struct {
	ID   string `json:"id"`   // derived from filename, e.g. "caddy"
	Name string `json:"name"` // display name, e.g. "caddy.log"
	Path string `json:"path"` // absolute path
	Size int64  `json:"size"` // bytes
}

// handleGetLogs returns the list of log files in the central logs directory.
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	logsDir := paths.LogsDir(s.serverRoot)
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, []LogFileInfo{})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var files []LogFileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Only include active log files (not rotated backups like .log.1).
		if !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(name, ".log")
		files = append(files, LogFileInfo{
			ID:   id,
			Name: name,
			Path: filepath.Join(logsDir, name),
			Size: info.Size(),
		})
	}
	if files == nil {
		files = []LogFileInfo{}
	}
	writeJSON(w, files)
}

// handleGetLogStream streams a log file from the central logs directory as SSE.
// The {id} path value is the log file stem (e.g. "caddy" for caddy.log).
func (s *Server) handleGetLogStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	logPath := paths.LogPath(s.serverRoot, id)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			sendSSE(w, flusher, "error", map[string]string{"message": fmt.Sprintf("log file not found: %s", id)})
			return
		}
		sendSSE(w, flusher, "error", map[string]string{"message": err.Error()})
		return
	}
	defer f.Close()

	// Seek to last 16KB for initial tail.
	if info, err := f.Stat(); err == nil && info.Size() > 16384 {
		f.Seek(-16384, io.SeekEnd)
		// Skip the partial first line.
		buf := make([]byte, 1)
		for {
			_, err := f.Read(buf)
			if err != nil || buf[0] == '\n' {
				break
			}
		}
	}

	buf := make([]byte, 4096)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			n, err := f.Read(buf)
			if n > 0 {
				sendSSE(w, flusher, "log", string(buf[:n]))
			}
			if err != nil && err != io.EOF {
				log.Printf("log stream error: %v", err)
				return
			}
		}
	}
}

// handleGetLogTail returns the last N bytes of a log file as plain text.
// The {id} path value is the log file stem (e.g. "caddy" for caddy.log).
// Query param: bytes=N (default 16384, max 131072).
func (s *Server) handleGetLogTail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	const defaultBytes = 16384
	const maxBytes = 131072

	tailBytes := int64(defaultBytes)
	if v := r.URL.Query().Get("bytes"); v != "" {
		var n int64
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			if n > maxBytes {
				n = maxBytes
			}
			tailBytes = n
		}
	}

	logPath := paths.LogPath(s.serverRoot, id)
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "log file not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	offset := info.Size() - tailBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// If we seeked into the middle of a line, skip to the next newline.
	if offset > 0 {
		buf := make([]byte, 1)
		for {
			_, readErr := f.Read(buf)
			if readErr != nil || buf[0] == '\n' {
				break
			}
		}
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.Copy(w, f) //nolint:errcheck
}

// handleClearLog truncates a log file in the central logs directory.
func (s *Server) handleClearLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	logPath := paths.LogPath(s.serverRoot, id)
	if err := os.Truncate(logPath, 0); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "log file not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
