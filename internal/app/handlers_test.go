package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHandleFiles_CacheHit(t *testing.T) {
	cfg := &Config{
		StateBackend: "none",
	}
	memFS := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	// Pre-populate dir cache
	sampleDir := []byte(`{"errno":0,"list":[{"fs_id":111,"path":"/cached/file.txt","server_filename":"file.txt","isdir":0,"dlink":"https://fake/dlink"}]}`)
	s.cache.update(sampleDir)
	s.cache.setDirList("/cached", sampleDir)

	// Make request without refresh
	reqBody := []byte(`{"dir":"/cached"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/files", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache: HIT, got %s", rec.Header().Get("X-Cache"))
	}

	var resp struct {
		List []struct {
			FsID int64  `json:"fs_id"`
			Path string `json:"path"`
		} `json:"list"`
		ResolvedDir string `json:"resolved_dir"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.List) != 1 || resp.List[0].FsID != 111 {
		t.Fatalf("unexpected list: %+v", resp.List)
	}
	if resp.ResolvedDir != "/cached" {
		t.Fatalf("unexpected resolved_dir: %s", resp.ResolvedDir)
	}
}

func TestHandleReadme_CacheHit(t *testing.T) {
	cfg := &Config{
		StateBackend: "none",
	}
	memFS := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	// Pre-populate readme cache
	s.cache.setReadme("/docs/README.md", "README.md", "# Preloaded Content")

	reqBody := []byte(`{"path":"/docs/README.md"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/readme", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleReadme(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache: HIT, got %s", rec.Header().Get("X-Cache"))
	}

	var resp struct {
		Found   bool   `json:"found"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse readme response: %v", err)
	}
	if !resp.Found || resp.Content != "# Preloaded Content" {
		t.Fatalf("unexpected readme resp: %+v", resp)
	}
}
