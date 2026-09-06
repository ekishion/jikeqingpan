package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestAllowedPathScope(t *testing.T) {
	cfg := &Config{BaiduCookie: "cookie", AllowedPaths: []string{" /共享资料 ", "/共享资料/电影"}}
	if err := cfg.normalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	srv := &Server{cfg: cfg}
	for _, p := range []string{"/共享资料", "/共享资料/文档/a.txt", "/共享资料/电影/movie.mp4"} {
		if !srv.pathAllowed(p) {
			t.Fatalf("expected allowed path %q", p)
		}
	}
	for _, p := range []string{"/私人文件", "/共享资料2", "/共享"} {
		if srv.pathAllowed(p) {
			t.Fatalf("expected denied path %q", p)
		}
	}
}

func TestNestedAllowedPathKeepsOnlyNavigationAncestorsVisible(t *testing.T) {
	cfg := &Config{BaiduCookie: "cookie", AllowedPaths: []string{"/共享资料/电影"}}
	if err := cfg.normalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	srv := &Server{cfg: cfg}
	if !srv.pathNavigable("/") || !srv.pathNavigable("/共享资料") {
		t.Fatal("ancestors of an allowed path should remain navigable")
	}
	if srv.pathNavigable("/私人文件") {
		t.Fatal("unrelated directory should not be navigable")
	}
	if !srv.pathVisible("/共享资料/电影") || !srv.pathVisible("/共享资料") {
		t.Fatal("allowed path and its navigation ancestor should be visible")
	}
	if srv.pathVisible("/共享资料/其他") {
		t.Fatal("unallowed sibling should be hidden")
	}
}

func TestFilterFileListHidesUnallowedEntries(t *testing.T) {
	cfg := &Config{BaiduCookie: "cookie", AllowedPaths: []string{"/共享资料/电影"}}
	if err := cfg.normalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	srv := &Server{cfg: cfg}
	body := []byte(`{"errno":0,"list":[{"path":"/共享资料","isdir":1},{"path":"/共享资料/电影","isdir":1},{"path":"/共享资料/其他","isdir":1},{"path":"/私人文件/a.txt","isdir":0}]}`)
	filtered, err := srv.filterFileList(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(filtered), "/共享资料/其他") || strings.Contains(string(filtered), "/私人文件/a.txt") {
		t.Fatalf("unallowed entries leaked: %s", filtered)
	}
	if !strings.Contains(string(filtered), "/共享资料/电影") || !strings.Contains(string(filtered), "/共享资料\"") {
		t.Fatalf("allowed entry or navigation ancestor missing: %s", filtered)
	}
}

func TestDownloadAndListingRejectUnallowedPath(t *testing.T) {
	srv := testAuthServer(t)
	srv.cfg.AllowedPaths = []string{"/共享资料"}
	csrf := csrfHandshake(t, srv)

	get := httptest.NewRequest(http.MethodGet, "/api/files?dir=/私人文件", nil)
	get.Header.Set("X-Access-Token", "s3cret-token")
	get.AddCookie(csrf)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, get)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "path_not_allowed") {
		t.Fatalf("listing should reject unallowed path, got %d body=%s", rec.Code, rec.Body.String())
	}

	post := httptest.NewRequest(http.MethodPost, "/api/download", strings.NewReader(`{"path":"/私人文件/a.txt"}`))
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("X-Access-Token", "s3cret-token")
	post.Header.Set("X-CSRF-Token", csrf.Value)
	post.AddCookie(csrf)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "path_not_allowed") {
		t.Fatalf("download should reject unallowed path, got %d body=%s", rec.Code, rec.Body.String())
	}

	token, err := srv.shortLinks.create("/私人文件/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	short := httptest.NewRequest(http.MethodGet, "/d/"+token, nil)
	short.Header.Set("X-Access-Token", "s3cret-token")
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, short)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "path_not_allowed") {
		t.Fatalf("shortlink should reject path after allowlist change, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInvalidAllowedPathConfig(t *testing.T) {
	cfg := &Config{BaiduCookie: "cookie", AllowedPaths: []string{"/共享资料/../私人"}}
	if err := cfg.normalizeAndValidate(); err == nil {
		t.Fatal("invalid allowed path should fail config validation")
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
	t.Setenv("ALLOWED_PATHS", "/共享资料,/临时中转")

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
	if len(cfg.AllowedPaths) != 2 || cfg.AllowedPaths[0] != "/共享资料" || cfg.AllowedPaths[1] != "/临时中转" {
		t.Fatalf("allowed paths=%v", cfg.AllowedPaths)
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
  "allowed_paths": ["/文件"],
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
	if len(cfg.AllowedPaths) != 1 || cfg.AllowedPaths[0] != "/文件" {
		t.Fatalf("allowed paths should load from file: %v", cfg.AllowedPaths)
	}
}

func TestPreviewRejectsInvalidAndUnallowedPath(t *testing.T) {
	srv := testAuthServer(t)
	srv.cfg.AllowedPaths = []string{"/共享资料"}
	csrf := csrfHandshake(t, srv)

	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/preview", strings.NewReader(`{"path":"`+path+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Access-Token", "s3cret-token")
		req.Header.Set("X-CSRF-Token", csrf.Value)
		req.AddCookie(csrf)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("/a/../b.txt"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_path") {
		t.Fatalf("invalid path should be 400 invalid_path, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := post("/私人文件/a.jpg"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "path_not_allowed") {
		t.Fatalf("unallowed path should be 403 path_not_allowed, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// fakeBaiduServer 返回最小可用的百度接口桩：list / getinfo / locatedownload。
func fakeBaiduServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/api/list"):
			_, _ = w.Write([]byte(`{"errno":0,"list":[` +
				`{"path":"/file.bin","fs_id":1,"md5":"m1","isdir":0,"dlink":""},` +
				`{"path":"/file.txt","fs_id":2,"md5":"m2","isdir":0,"dlink":""},` +
				`{"path":"/file.png","fs_id":3,"md5":"m3","isdir":0,"dlink":""}]}`))
		case strings.Contains(r.URL.Path, "/user/getinfo"):
			_, _ = w.Write([]byte(`{"errno":0,"records":[{"uk":42,"sk":"sk-value"}]}`))
		case strings.Contains(r.URL.Path, "/locatedownload"):
			_, _ = w.Write([]byte(`{"errno":0,"url":"https://d.pcs.baidu.com/file/x"}`))
		default:
			_, _ = w.Write([]byte(`{"errno":0}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestShortLinkConsumesOnlyAfterSuccessfulRedirect(t *testing.T) {
	// 成功解析：302 跳转后短链按 max_uses 消耗
	srv := testAuthServer(t)
	srv.baiduBaseURL = fakeBaiduServer(t).URL
	srv.shortLinks = newShortLinkStoreWithLimits(time.Hour, 100, 1)

	token, err := srv.shortLinks.create("/file.bin")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/d/"+token, nil)
	req.Header.Set("X-Access-Token", "s3cret-token")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302 redirect, got %d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "https://d.pcs.baidu.com/file/x") {
		t.Fatalf("unexpected redirect target %q", loc)
	}
	if _, ok := srv.shortLinks.resolve(token, false); ok {
		t.Fatal("link should be consumed after successful redirect (max_uses=1)")
	}

	// 解析失败（上游 500）：短链不应被消耗
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	srv2 := testAuthServer(t)
	srv2.baiduBaseURL = failing.URL
	srv2.shortLinks = newShortLinkStoreWithLimits(time.Hour, 100, 1)

	token2, err := srv2.shortLinks.create("/file.bin")
	if err != nil {
		t.Fatal(err)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/d/"+token2, nil)
	req2.Header.Set("X-Access-Token", "s3cret-token")
	rec2 := httptest.NewRecorder()
	srv2.mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadGateway {
		t.Fatalf("want 502 when upstream fails, got %d body=%s", rec2.Code, rec2.Body.String())
	}
	if _, ok := srv2.shortLinks.resolve(token2, false); !ok {
		t.Fatal("failed resolution must not consume the short link")
	}
}

func TestHeadRequestsSupported(t *testing.T) {
	srv := testAuthServer(t)
	// 用真实 HTTP 服务器验证 HEAD：ResponseRecorder 不模拟 HEAD 去 body 的行为。
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	for _, path := range []string{"/healthz", "/"} {
		req, err := http.NewRequest(http.MethodHead, ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("HEAD %s want 200 got %d", path, resp.StatusCode)
		}
		if len(body) != 0 {
			t.Fatalf("HEAD %s should carry no body, got %d bytes", path, len(body))
		}
	}
}

func TestHealthzSkipsCSRFCookie(t *testing.T) {
	srv := testAuthServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfTokenCookieName {
			t.Fatal("healthz should not issue a csrf cookie")
		}
	}
}

// stubContentTransport 拦截对百度直链域（*.pcs.baidu.com）的请求并返回预设内容，
// 其余请求走默认传输（本地假百度 API）。
type stubContentTransport struct {
	content []byte
}

func (tr *stubContentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "pcs.baidu.com") {
		header := http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(bytes.NewReader(tr.content)),
		}, nil
	}
	return http.DefaultTransport.RoundTrip(req)
}

// newPreviewTestServer 起一个带鉴权 CSRF 的服务器，百度 API 打到本地假服务，
// 直链内容由 stub 提供。返回 (server, csrfCookie)。
func newPreviewTestServer(t *testing.T, content []byte) (*Server, *http.Cookie) {
	t.Helper()
	srv := testAuthServer(t)
	srv.baiduBaseURL = fakeBaiduServer(t).URL
	srv.httpClient = &http.Client{Transport: &stubContentTransport{content: content}}
	return srv, csrfHandshake(t, srv)
}

func postJSON(t *testing.T, srv *Server, csrf *http.Cookie, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Access-Token", "s3cret-token")
	req.Header.Set("X-CSRF-Token", csrf.Value)
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	return rec
}

func TestTextPreviewEndpoint(t *testing.T) {
	// UTF-8 文本正常返回
	srv, csrf := newPreviewTestServer(t, []byte("你好，世界\nsecond line\n"))
	rec := postJSON(t, srv, csrf, "/api/text", `{"path":"/file.txt"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("text preview want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var data struct {
		Found     bool   `json:"found"`
		Name      string `json:"name"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if !data.Found || data.Content != "你好，世界\nsecond line\n" || data.Truncated {
		t.Fatalf("unexpected payload: %+v", data)
	}

	// 不在白名单的扩展名拒绝
	srv2, csrf2 := newPreviewTestServer(t, []byte("x"))
	if rec := postJSON(t, srv2, csrf2, "/api/text", `{"path":"/file.exe"}`); rec.Code != http.StatusUnsupportedMediaType ||
		!strings.Contains(rec.Body.String(), "text_not_allowed") {
		t.Fatalf("exe should be 415 text_not_allowed, got %d body=%s", rec.Code, rec.Body.String())
	}

	// 二进制内容拒绝
	srv3, csrf3 := newPreviewTestServer(t, []byte{0xff, 0xfe, 0x00, 0x01, 0x02})
	if rec := postJSON(t, srv3, csrf3, "/api/text", `{"path":"/file.txt"}`); rec.Code != http.StatusUnsupportedMediaType ||
		!strings.Contains(rec.Body.String(), "text_not_text") {
		t.Fatalf("binary should be 415 text_not_text, got %d body=%s", rec.Code, rec.Body.String())
	}

	// 超长内容按 UTF-8 边界截断并标记 truncated
	big := bytes.Repeat([]byte("好"), 300*1024) // 每个字符 3 字节，总量超过 512KB
	srv4, csrf4 := newPreviewTestServer(t, big)
	rec4 := postJSON(t, srv4, csrf4, "/api/text", `{"path":"/file.txt"}`)
	if rec4.Code != http.StatusOK {
		t.Fatalf("oversize text want 200 got %d", rec4.Code)
	}
	var data4 struct {
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(rec4.Body.Bytes(), &data4); err != nil {
		t.Fatal(err)
	}
	if !data4.Truncated {
		t.Fatal("oversize content should be marked truncated")
	}
	if len(data4.Content) >= len(big) || len(data4.Content)%3 != 0 {
		t.Fatalf("content should be cut at a rune boundary: %d bytes", len(data4.Content))
	}
}

func TestImagePreviewStreamsBody(t *testing.T) {
	// 最小 PNG 魔数即可通过嗅探
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0x00}, 64)...)
	srv, csrf := newPreviewTestServer(t, png)
	rec := postJSON(t, srv, csrf, "/api/preview", `{"path":"/file.png"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("image preview want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type = %q, want image/png", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), png) {
		t.Fatalf("streamed body mismatch: got %d bytes want %d", rec.Body.Len(), len(png))
	}
}

func TestTunableConfigDefaultsAndOverrides(t *testing.T) {
	cfg := &Config{BaiduCookie: "c"}
	if err := cfg.normalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	if cfg.listMaxPages() != 15 || cfg.previewMaxBytes() != 16*1024*1024 ||
		cfg.readmeMaxBytes() != 512*1024 || cfg.fileCacheTTL() != 15*time.Minute ||
		cfg.dlinkCacheTTL() != 5*time.Minute {
		t.Fatalf("unexpected defaults: pages=%d preview=%d readme=%d fileTTL=%v dlinkTTL=%v",
			cfg.listMaxPages(), cfg.previewMaxBytes(), cfg.readmeMaxBytes(), cfg.fileCacheTTL(), cfg.dlinkCacheTTL())
	}

	t.Setenv("BAIDU_COOKIE", "cookie-from-env")
	t.Setenv("LIST_MAX_PAGES", "30")
	t.Setenv("PREVIEW_MAX_BYTES", "1048576")
	t.Setenv("README_MAX_BYTES", "65536")
	t.Setenv("FILE_CACHE_TTL_SECONDS", "60")
	t.Setenv("DLINK_CACHE_TTL_SECONDS", "120")
	cfg2, err := loadConfig("this-file-should-not-exist-jikeqingpan.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.listMaxPages() != 30 || cfg2.previewMaxBytes() != 1048576 ||
		cfg2.readmeMaxBytes() != 65536 || cfg2.fileCacheTTL() != time.Minute ||
		cfg2.dlinkCacheTTL() != 2*time.Minute {
		t.Fatalf("env override failed: pages=%d preview=%d readme=%d fileTTL=%v dlinkTTL=%v",
			cfg2.listMaxPages(), cfg2.previewMaxBytes(), cfg2.readmeMaxBytes(), cfg2.fileCacheTTL(), cfg2.dlinkCacheTTL())
	}

	bad := &Config{BaiduCookie: "c", ListMaxPages: 0}
	if err := bad.normalizeAndValidate(); err != nil {
		t.Fatal("zero list_max_pages should mean default")
	}
	bad = &Config{BaiduCookie: "c", ListMaxPages: -1}
	if err := bad.normalizeAndValidate(); err == nil {
		t.Fatal("negative list_max_pages should fail validation")
	}
}

func TestCacheTTLWiringAndEvictionCap(t *testing.T) {
	cache := newFileListCacheWithLimits(3, time.Hour, time.Hour)
	body := []byte(`{"errno":0,"list":[` +
		`{"path":"/a","fs_id":1,"md5":"m1","isdir":0},` +
		`{"path":"/b","fs_id":2,"md5":"m2","isdir":0},` +
		`{"path":"/c","fs_id":3,"md5":"m3","isdir":0}]}`)
	cache.update(body)
	if len(cache.filesByPath) != 3 {
		t.Fatalf("want 3 entries, got %d", len(cache.filesByPath))
	}
	// 超出上限时必须淘汰，不能无界增长。
	// 直接写入第 4 条并显式设置更晚的访问时间：Windows 时钟粒度可能让两次
	// update 的时间戳打平，而采样淘汰在平局时是随机选择的。
	cache.mu.Lock()
	cache.filesByPath["/d"] = fileMeta{
		FsID:         4,
		MD5:          "m4",
		CachedAt:     time.Now(),
		LastAccessAt: cache.filesByPath["/a"].LastAccessAt.Add(time.Hour),
	}
	cache.enforceLimitLocked()
	cache.mu.Unlock()
	if len(cache.filesByPath) > 3 {
		t.Fatalf("cache exceeded cap: %d", len(cache.filesByPath))
	}
	if _, ok := cache.getFileMeta("/d"); !ok {
		t.Fatal("newest entry should survive eviction")
	}

	// dlink TTL 由构造参数控制
	short := newFileListCacheWithLimits(10, time.Hour, 30*time.Millisecond)
	listWithDlink := []byte(`{"errno":0,"list":[{"path":"/a","fs_id":1,"md5":"m","isdir":0,"dlink":"https://d.pcs.baidu.com/x"}]}`)
	short.update(listWithDlink)
	if _, ok := short.getCachedDLink("/a", ""); !ok {
		t.Fatal("dlink should be cached right after update")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := short.getCachedDLink("/a", ""); ok {
		t.Fatal("dlink should expire per configured dlinkTTL")
	}
}

func TestListingCacheSkipsOutOfScopeEntries(t *testing.T) {
	srv := testAuthServer(t)
	srv.cfg.AllowedPaths = []string{"/共享资料/电影"}
	body := []byte(`{"errno":0,"list":[` +
		`{"path":"/共享资料/电影/a.mp4","fs_id":1,"md5":"m1","isdir":0},` +
		`{"path":"/共享资料/其他/b.mp4","fs_id":2,"md5":"m2","isdir":0},` +
		`{"path":"/私人文件/c.txt","fs_id":3,"md5":"m3","isdir":0}]}`)
	filtered, err := srv.filterFileList(body)
	if err != nil {
		t.Fatal(err)
	}
	srv.cache.update(filtered)
	if _, ok := srv.cache.getFileMeta("/共享资料/其他/b.mp4"); ok {
		t.Fatal("out-of-scope entry must not be cached")
	}
	if _, ok := srv.cache.getFileMeta("/私人文件/c.txt"); ok {
		t.Fatal("out-of-scope entry must not be cached")
	}
	if _, ok := srv.cache.getFileMeta("/共享资料/电影/a.mp4"); !ok {
		t.Fatal("in-scope entry should be cached")
	}
}

func TestAuditLogRotation(t *testing.T) {
	srv := testAuthServer(t)
	srv.cfg.AuditLogPath = t.TempDir() + string(os.PathSeparator) + "audit.jsonl"
	// 把滚动阈值压到极小以触发轮转
	srv.auditMax = 1
	defer srv.closeAudit()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	srv.audit(req, "shortlink_issued", "/file.bin")
	srv.audit(req, "shortlink_accessed", "/file.bin")

	raw, err := os.ReadFile(srv.cfg.AuditLogPath)
	if err != nil {
		t.Fatalf("audit file missing: %v", err)
	}
	if !strings.Contains(string(raw), "shortlink_issued") && !strings.Contains(string(raw), "shortlink_accessed") {
		t.Fatalf("audit content unexpected: %s", raw)
	}
	old, err := os.ReadFile(srv.cfg.AuditLogPath + ".old")
	if err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	if len(old) == 0 {
		t.Fatal("rotated file should not be empty")
	}
}
