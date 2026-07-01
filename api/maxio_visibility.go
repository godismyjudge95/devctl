package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/danielgormly/devctl/paths"
)

type maxioBucketVisibility struct {
	PublicRead bool `json:"publicRead"`
	PublicList bool `json:"publicList"`
}

type maxioBucketMetaFile struct {
	PublicRead bool `json:"public_read"`
	PublicList bool `json:"public_list"`
}

func (s *Server) maxioDataDir() (string, error) {
	envPath := filepath.Join(paths.ServiceDir(s.serverRoot, "maxio"), "config.env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return "", fmt.Errorf("maxio: read config.env: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MAXIO_DATA_DIR=") {
			dir := strings.TrimPrefix(line, "MAXIO_DATA_DIR=")
			if dir != "" {
				return dir, nil
			}
		}
	}
	return filepath.Join(paths.ServiceDir(s.serverRoot, "maxio"), "data"), nil
}

func validateMaxioBucketName(name string) error {
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid bucket name")
	}
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("invalid bucket name")
	}
	for _, r := range name {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) && r != '-' && r != '.' {
			return fmt.Errorf("invalid bucket name")
		}
	}
	first, last := rune(name[0]), rune(name[len(name)-1])
	if !unicode.IsLetter(first) && !unicode.IsDigit(first) {
		return fmt.Errorf("invalid bucket name")
	}
	if !unicode.IsLetter(last) && !unicode.IsDigit(last) {
		return fmt.Errorf("invalid bucket name")
	}
	if strings.Contains(name, "..") || strings.Contains(name, ".-") || strings.Contains(name, "-.") {
		return fmt.Errorf("invalid bucket name")
	}
	return nil
}

func maxioBucketMetaPath(dataDir, bucket string) (string, error) {
	if err := validateMaxioBucketName(bucket); err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "buckets", bucket, ".bucket.json"), nil
}

func readMaxioBucketVisibility(metaPath string) (maxioBucketVisibility, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return maxioBucketVisibility{}, fmt.Errorf("bucket not found")
		}
		return maxioBucketVisibility{}, err
	}
	var meta maxioBucketMetaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return maxioBucketVisibility{}, fmt.Errorf("parse bucket metadata: %w", err)
	}
	return maxioBucketVisibility{
		PublicRead: meta.PublicRead,
		PublicList: meta.PublicList,
	}, nil
}

func writeMaxioBucketVisibility(metaPath string, vis maxioBucketVisibility) (maxioBucketVisibility, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return maxioBucketVisibility{}, fmt.Errorf("bucket not found")
		}
		return maxioBucketVisibility{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return maxioBucketVisibility{}, fmt.Errorf("parse bucket metadata: %w", err)
	}
	raw["public_read"], err = json.Marshal(vis.PublicRead)
	if err != nil {
		return maxioBucketVisibility{}, err
	}
	raw["public_list"], err = json.Marshal(vis.PublicList)
	if err != nil {
		return maxioBucketVisibility{}, err
	}
	encoded, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return maxioBucketVisibility{}, err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(metaPath, encoded, 0644); err != nil {
		return maxioBucketVisibility{}, err
	}
	return vis, nil
}

func (s *Server) handleGetMaxIOBucketVisibility(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	dataDir, err := s.maxioDataDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	metaPath, err := maxioBucketMetaPath(dataDir, bucket)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	vis, err := readMaxioBucketVisibility(metaPath)
	if err != nil {
		if err.Error() == "bucket not found" {
			http.Error(w, "bucket not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, vis)
}

func (s *Server) handlePutMaxIOBucketVisibility(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	var req maxioBucketVisibility
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	dataDir, err := s.maxioDataDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	metaPath, err := maxioBucketMetaPath(dataDir, bucket)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	vis, err := writeMaxioBucketVisibility(metaPath, req)
	if err != nil {
		if err.Error() == "bucket not found" {
			http.Error(w, "bucket not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, vis)
}