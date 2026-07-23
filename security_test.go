package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"testing/fstest"
	"time"
)

func TestIsValidBaiduPath(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"/", true},
		{"/a/b", true},
		{"/中文/文件.txt", true},
		{"", false},
		{"a/b", false},
		{"/a/../b", false},
		{"/a/./b", false},
		{"/a//b", false}, // path.Clean collapses
		{"/a/\x00b", false},
		{"/a/b/", false}, // cleaned to /a/b
	}
	for _, tc := range cases {
		if got := isValidBaiduPath(tc.in); got != tc.want {
			t.Fatalf("isValidBaiduPath(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsAllowedBaiduDownloadURL(t *testing.T) {
	ok := []string{
		"https://d.pcs.baidu.com/file/xxx",
		"https://bjb.baidupcs.com/file/x",
		"https://www.baidu.com/x",
	}
	bad := []string{
		"http://d.pcs.baidu.com/file/xxx",
		"https://evil.com/",
		"https://baidu.com.evil.com/",
		"javascript:alert(1)",
		"",
	}
	for _, u := range ok {
		if !isAllowedBaiduDownloadURL(u) {
			t.Fatalf("expected allow %q", u)
		}
	}
	for _, u := range bad {
		if isAllowedBaiduDownloadURL(u) {
			t.Fatalf("expected deny %q", u)
		}
	}
}

func TestStripDownloadLinks(t *testing.T) {
	in := []byte(`{"errno":0,"list":[{"path":"/a","dlink":"https://x","name":"a"}]}`)
	out, err := stripDownloadLinks(in)
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		List []map[string]any `json:"list"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("list len=%d", len(resp.List))
	}
	if _, ok := resp.List[0]["dlink"]; ok {
		t.Fatal("dlink should be stripped")
	}
	if resp.List[0]["path"] != "/a" {
		t.Fatalf("path missing: %v", resp.List[0])
	}
}

func TestShortLinkStoreExpireAndUses(t *testing.T) {
	store := newShortLinkStoreWithLimits(50*time.Millisecond, 100, 1)
	token, err := store.create("/file.bin")
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := store.resolve(token, true); !ok || p != "/file.bin" {
		t.Fatalf("first resolve failed: %q %v", p, ok)
	}
	if _, ok := store.resolve(token, true); ok {
		t.Fatal("second resolve should fail after max uses=1")
	}

	store2 := newShortLinkStoreWithLimits(30*time.Millisecond, 100, 0)
	token2, err := store2.create("/a")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := store2.resolve(token2, true); ok {
		t.Fatal("expired token should fail")
	}
}

func TestConfigAuthAndDefaults(t *testing.T) {
	cfg := &Config{
		Port:        0,
		BaiduCookie: "cookie",
		AccessToken: "  secret  ",
	}
	if err := cfg.normalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 4172 {
		t.Fatalf("port default %d", cfg.Port)
	}
	if cfg.AccessToken != "secret" {
		t.Fatalf("token trim %q", cfg.AccessToken)
	}
	if !cfg.authEnabled() {
		t.Fatal("auth should enable")
	}
	if cfg.shortLinkTTL() != time.Hour {
		t.Fatal("ttl default")
	}
}

func TestAuthMiddleware(t *testing.T) {
	cfg := &Config{
		Port:               4172,
		BindAddress:        "127.0.0.1",
		BaiduCookie:        "c",
		RateLimitPerSecond: 100,
		AccessToken:        "s3cret-token",
	}
	if err := cfg.normalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	static := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("ok")},
		"static/app.js":     &fstest.MapFile{Data: []byte("//")},
	}
	srv := newServer(cfg, static)

	// files without auth -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/files?dir=/", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", rec.Code, rec.Body.String())
	}

	// healthz open
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz %d", rec.Code)
	}

	// static open
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("static %d", rec.Code)
	}
}

func TestWithPrivateCacheControl(t *testing.T) {
	u := withPrivateCacheControl("https://d.pcs.baidu.com/file?x=1")
	if u != "https://d.pcs.baidu.com/file?x=1&response-cache-control=private" {
		t.Fatalf("got %s", u)
	}
	u2 := withPrivateCacheControl("https://d.pcs.baidu.com/file")
	if u2 != "https://d.pcs.baidu.com/file?response-cache-control=private" {
		t.Fatalf("got %s", u2)
	}
}

func TestLoadConfigEnvOnlyWhenFileMissing(t *testing.T) {
	t.Setenv("BAIDU_COOKIE", "cookie-from-env")
	t.Setenv("ACCESS_TOKEN", "token-from-env")
	t.Setenv("BIND_ADDRESS", "0.0.0.0")
	t.Setenv("PORT", "18080")
	t.Setenv("FORCE_SECURE_COOKIE", "true")

	cfg, err := loadConfig("this-file-should-not-exist-jikeqingpan.json")
	if err != nil {
		t.Fatalf("loadConfig env-only: %v", err)
	}
	if cfg.BaiduCookie != "cookie-from-env" {
		t.Fatalf("cookie=%q", cfg.BaiduCookie)
	}
	if cfg.AccessToken != "token-from-env" {
		t.Fatalf("token=%q", cfg.AccessToken)
	}
	if cfg.BindAddress != "0.0.0.0" {
		t.Fatalf("bind=%q", cfg.BindAddress)
	}
	if cfg.Port != 18080 {
		t.Fatalf("port=%d", cfg.Port)
	}
	if !cfg.ForceSecureCookie {
		t.Fatal("force_secure_cookie expected true")
	}
}

func TestLoadConfigEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + string(os.PathSeparator) + "cfg.json"
	raw := []byte(`{
  "port": 4172,
  "bind_address": "127.0.0.1",
  "baidu_cookie": "file-cookie",
  "access_token": "file-token",
  "force_secure_cookie": false
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BAIDU_COOKIE", "env-cookie")
	t.Setenv("ACCESS_TOKEN", "env-token")
	t.Setenv("PORT", "9090")

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaiduCookie != "env-cookie" || cfg.AccessToken != "env-token" || cfg.Port != 9090 {
		t.Fatalf("override failed: cookie=%q token=%q port=%d", cfg.BaiduCookie, cfg.AccessToken, cfg.Port)
	}
	if cfg.BindAddress != "127.0.0.1" {
		t.Fatalf("bind should stay from file: %q", cfg.BindAddress)
	}
}
