package app

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestJSONPersistence_DirCache(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	p, err := newJSONPersistence(statePath)
	if err != nil {
		t.Fatalf("newJSONPersistence failed: %v", err)
	}

	st := &persistedState{
		Version: 1,
		DirCache: []PersistedDirCache{
			{
				Dir:      "/home",
				Body:     json.RawMessage(`{"errno":0,"list":[]}`),
				CachedAt: time.Now().Unix(),
			},
		},
	}

	if err := p.Save(st); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	_ = p.Close()

	// Load back
	p2, err := newJSONPersistence(statePath)
	if err != nil {
		t.Fatalf("newJSONPersistence 2 failed: %v", err)
	}
	defer p2.Close()

	loaded, err := p2.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.DirCache) != 1 || loaded.DirCache[0].Dir != "/home" {
		t.Fatalf("unexpected loaded DirCache: %+v", loaded.DirCache)
	}
}

func TestSQLitePersistence_DirCache(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.db")

	p, err := newSQLitePersistence(dbPath)
	if err != nil {
		t.Fatalf("newSQLitePersistence failed: %v", err)
	}

	st := &persistedState{
		Version: 1,
		DirLinks: []persistedLink{
			{Token: "tok1", Path: "/myfolder", ExpiresAt: 9999999999, CreatedAt: 1111111111},
		},
		DirCache: []PersistedDirCache{
			{
				Dir:      "/myfolder",
				Body:     json.RawMessage(`{"errno":0,"list":[{"fs_id":888,"path":"/myfolder/a.txt"}]}`),
				CachedAt: time.Now().Unix(),
			},
		},
	}

	if err := p.Save(st); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	_ = p.Close()

	// Load back
	p2, err := newSQLitePersistence(dbPath)
	if err != nil {
		t.Fatalf("newSQLitePersistence 2 failed: %v", err)
	}
	defer p2.Close()

	loaded, err := p2.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.DirLinks) != 1 || loaded.DirLinks[0].Token != "tok1" {
		t.Fatalf("unexpected DirLinks: %+v", loaded.DirLinks)
	}
	if len(loaded.DirCache) != 1 || loaded.DirCache[0].Dir != "/myfolder" {
		t.Fatalf("unexpected DirCache: %+v", loaded.DirCache)
	}
}
