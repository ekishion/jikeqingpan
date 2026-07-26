package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testAuthServer(t *testing.T) *Server {
	t.Helper()
	cfg := &Config{
		Port:               4172,
		BindAddress:        "127.0.0.1",
		BaiduCookie:        "c",
		RateLimitPerSecond: 1000,
		AccessToken:        "s3cret-token",
	}
	if err := cfg.normalizeAndValidate(); err != nil {
		t.Fatal(err)
	}
	static := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("ok")},
		"static/app.js":     &fstest.MapFile{Data: []byte("//")},
	}
	return newServer(cfg, static)
}

func TestSessionManagerRoundTrip(t *testing.T) {
	m := newSessionManager([]byte("k"), time.Hour)
	now := time.Unix(1_700_000_000, 0)
	tok, err := m.issue(now)
	if err != nil {
		t.Fatal(err)
	}
	if !m.validate(tok, now) {
		t.Fatal("freshly issued token should validate")
	}
	// 过期后失效
	if m.validate(tok, now.Add(2*time.Hour)) {
		t.Fatal("expired token should not validate")
	}
	// 篡改载荷失效
	parts := strings.Split(tok, ".")
	tampered := parts[0] + "." + parts[1] + "x." + parts[2]
	if m.validate(tampered, now) {
		t.Fatal("tampered payload must fail")
	}
	// 换密钥（模拟 session_secret 轮换）后旧令牌失效
	other := newSessionManager([]byte("k2"), time.Hour)
	if other.validate(tok, now) {
		t.Fatal("token signed with a different key must fail")
	}
	// 垃圾输入
	for _, bad := range []string{"", "v1", "v1.a.b.c", "v2." + parts[1] + "." + parts[2]} {
		if m.validate(bad, now) {
			t.Fatalf("malformed token should fail: %q", bad)
		}
	}
}

func TestSubtleCompareTokenConstantLength(t *testing.T) {
	if !subtleCompareToken("abc", "abc") {
		t.Fatal("equal tokens must match")
	}
	if subtleCompareToken("abc", "abcd") {
		t.Fatal("different-length tokens must not match")
	}
	if subtleCompareToken("abc", "abd") {
		t.Fatal("same-length different tokens must not match")
	}
}

// csrfHandshake 走一次 GET 拿到 csrf cookie，返回 (cookieValue)。
func csrfHandshake(t *testing.T, srv *Server) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfTokenCookieName {
			return c
		}
	}
	t.Fatal("csrf cookie not issued on GET")
	return nil
}

func TestLoginIssuesSessionCookieAndGrantsAccess(t *testing.T) {
	srv := testAuthServer(t)
	csrf := csrfHandshake(t, srv)

	// 正确令牌登录
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"token":"s3cret-token"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf.Value)
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login want 200 got %d body=%s", rec.Code, rec.Body.String())
	}

	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			session = c
		}
		if c.Name == legacyAccessCookieName && c.Value != "" && c.MaxAge >= 0 {
			t.Fatal("login must not set the raw access_token cookie")
		}
	}
	if session == nil || session.Value == "" {
		t.Fatal("login should set jkqp_session cookie")
	}
	if !session.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}
	if strings.Contains(session.Value, "s3cret-token") {
		t.Fatal("session cookie must not contain the raw access token")
	}

	// 用会话 Cookie 访问受保护接口
	req = httptest.NewRequest(http.MethodGet, "/api/files?dir=/", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("session cookie should grant access, got 401 body=%s", rec.Body.String())
	}
}

func TestLegacyRawTokenCookieRejected(t *testing.T) {
	srv := testAuthServer(t)
	// 旧版本把 access_token 原文写进 Cookie；新版应不再据此放行。
	req := httptest.NewRequest(http.MethodGet, "/api/files?dir=/", nil)
	req.AddCookie(&http.Cookie{Name: legacyAccessCookieName, Value: "s3cret-token"})
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("legacy raw-token cookie must be rejected, got %d", rec.Code)
	}
}

func TestHeaderTokenStillGrantsAccess(t *testing.T) {
	srv := testAuthServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/files?dir=/", nil)
	req.Header.Set("X-Access-Token", "s3cret-token")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("programmatic header token should grant access, got 401")
	}
}

func TestLoginGuardExponentialLockout(t *testing.T) {
	g := newLoginGuard()
	now := time.Unix(1_700_000_000, 0)
	ip := "1.2.3.4"
	// 前 freeAttempts 次失败不锁定
	for i := 0; i < g.freeAttempts; i++ {
		g.recordFailure(ip, now)
		if ok, _ := g.allow(ip, now); !ok {
			t.Fatalf("should not lock within free attempts (i=%d)", i)
		}
	}
	// 第 freeAttempts+1 次失败起进入锁定
	g.recordFailure(ip, now)
	ok, retry := g.allow(ip, now)
	if ok || retry <= 0 {
		t.Fatalf("expected lockout after exceeding free attempts, ok=%v retry=%v", ok, retry)
	}
	// 锁定到期后可再次尝试
	if ok, _ := g.allow(ip, now.Add(retry+time.Second)); !ok {
		t.Fatal("should be allowed again after lockout elapses")
	}
	// 成功登录清零
	g.recordSuccess(ip)
	if ok, _ := g.allow(ip, now); !ok {
		t.Fatal("recordSuccess should clear the lockout")
	}
}

func TestLoginLockoutReturns429(t *testing.T) {
	srv := testAuthServer(t)
	csrf := csrfHandshake(t, srv)
	// 连续错误令牌直到触发锁定
	var last int
	for i := 0; i < 12; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"token":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf.Value)
		req.AddCookie(csrf)
		req.RemoteAddr = "9.9.9.9:12345"
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		last = rec.Code
		if last == http.StatusTooManyRequests {
			if rec.Header().Get("Retry-After") == "" {
				t.Fatal("429 lockout should carry Retry-After")
			}
			return
		}
	}
	t.Fatalf("expected a 429 lockout after repeated failures, last=%d", last)
}
