package app

import (
	"strings"
	"testing"
	"time"
)

func TestFileListCache_DirAndReadme(t *testing.T) {
	cache := newFileListCacheWithLimits(100, 100*time.Millisecond, 50*time.Millisecond)

	// 1. Directory caching
	dirJSON := []byte(`{"errno":0,"list":[{"fs_id":12345,"path":"/test/file1.txt","server_filename":"file1.txt","size":100,"isdir":0,"md5":"dummy_md5"}]}`)
	cache.update(dirJSON)
	cache.setDirList("/test", dirJSON)

	got, ok := cache.getDirList("/test")
	if !ok {
		t.Fatalf("expected getDirList to return true")
	}
	if string(got) != string(dirJSON) {
		t.Fatalf("getDirList content mismatch, got: %s", string(got))
	}

	// Check that fileMeta was populated
	meta, found := cache.getFileMeta("/test/file1.txt")
	if !found || meta.FsID != 12345 {
		t.Fatalf("expected fileMeta with FsID=12345, got %+v", meta)
	}

	// 2. README caching
	cache.setReadme("/test/README.md", "README.md", "# Hello Test")
	name, content, ok := cache.getReadme("/test/README.md")
	if !ok || name != "README.md" || content != "# Hello Test" {
		t.Fatalf("getReadme mismatch: name=%s content=%s ok=%v", name, content, ok)
	}

	// 3. Invalidate dir
	cache.invalidateDir("/test")
	if _, ok := cache.getDirList("/test"); ok {
		t.Fatalf("expected dir cache to be invalidated")
	}
	if _, _, ok := cache.getReadme("/test/README.md"); ok {
		t.Fatalf("expected readme cache to be invalidated after invalidateDir")
	}

	// 4. TTL expiration
	cache.setDirList("/test2", dirJSON)
	cache.setReadme("/test2/README.md", "README.md", "# Hello 2")
	time.Sleep(120 * time.Millisecond)

	if _, ok := cache.getDirList("/test2"); ok {
		t.Fatalf("expected dir cache to expire after TTL")
	}
	if _, _, ok := cache.getReadme("/test2/README.md"); ok {
		t.Fatalf("expected readme cache to expire after TTL")
	}
}

func TestFileListCache_ExportAndRestore(t *testing.T) {
	cache1 := newFileListCacheWithLimits(100, 10*time.Minute, 5*time.Minute)
	dirJSON := []byte(`{"errno":0,"list":[{"fs_id":9999,"path":"/folder/doc.pdf","server_filename":"doc.pdf","size":200,"isdir":0,"md5":"abc"}]}`)
	cache1.setDirList("/folder", dirJSON)

	exported := cache1.exportDirCache()
	if len(exported) != 1 {
		t.Fatalf("expected 1 exported dir, got %d", len(exported))
	}
	if exported[0].Dir != "/folder" {
		t.Fatalf("expected dir /folder, got %s", exported[0].Dir)
	}

	cache2 := newFileListCacheWithLimits(100, 10*time.Minute, 5*time.Minute)
	cache2.restoreDirCache(exported)

	got, ok := cache2.getDirList("/folder")
	if !ok || !strings.Contains(string(got), "doc.pdf") {
		t.Fatalf("expected restored dir list, got ok=%v body=%s", ok, string(got))
	}

	// Verify that restoreDirCache automatically warmed up filesByPath
	meta, found := cache2.getFileMeta("/folder/doc.pdf")
	if !found || meta.FsID != 9999 {
		t.Fatalf("expected warmed up fileMeta FsID=9999, got %+v", meta)
	}
}
