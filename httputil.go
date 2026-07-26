package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	csrfTokenCookieName = "csrf_token"
	// sessionCookieName 存放签名会话令牌（非 access_token 原文）。
	sessionCookieName = "jkqp_session"
	// legacyAccessCookieName 旧版本把 access_token 原文写入的 Cookie，仅用于登出时清理。
	legacyAccessCookieName = "access_token"
	csrfTokenMaxAge        = 3600
	defaultBaiduUA         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
)

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}

func newCSRFToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 CSRF 令牌失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func (s *Server) cookieSecure(r *http.Request) bool {
	return r.TLS != nil || (s.cfg != nil && s.cfg.ForceSecureCookie)
}

func (s *Server) setCSRFCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfTokenCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   csrfTokenMaxAge,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(r),
		// 双提交 CSRF 需要前端 JS 读取 cookie，因此不能 HttpOnly。
		HttpOnly: false,
	})
}

func hasValidCSRFToken(r *http.Request) bool {
	cookie, err := r.Cookie(csrfTokenCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	headerToken := r.Header.Get("X-CSRF-Token")
	if headerToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(headerToken)) == 1
}

func normalizeDownloadUA(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return defaultBaiduUA
	}
	return ua
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

// headerAccessToken 从请求头提取原始访问令牌（编程式客户端用；不接受 query 与
// Cookie，避免令牌进日志、也不把 Cookie 当原始令牌）。浏览器会话走签名 Cookie。
func headerAccessToken(r *http.Request) string {
	if h := strings.TrimSpace(r.Header.Get("X-Access-Token")); h != "" {
		return h
	}
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		const prefix = "Bearer "
		if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
	}
	return ""
}

// subtleCompareToken 对两侧做 SHA-256 摘要后再常量时间比较，摘要长度恒定，
// 因此不泄露令牌长度这一侧信道。
func subtleCompareToken(got, want string) bool {
	g := sha256.Sum256([]byte(got))
	w := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}

func (s *Server) hasValidAccess(r *http.Request) bool {
	if s.cfg == nil || !s.cfg.authEnabled() {
		return true
	}
	// 1) 编程式客户端：请求头携带原始令牌。
	if tok := headerAccessToken(r); tok != "" && subtleCompareToken(tok, s.cfg.AccessToken) {
		return true
	}
	// 2) 浏览器会话：签名会话 Cookie。
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if s.sessions != nil && s.sessions.validate(c.Value, time.Now()) {
			return true
		}
	}
	return false
}

// issueSession 登录成功后签发会话令牌并写入 HttpOnly Cookie。
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request) error {
	token, err := s.sessions.issue(time.Now())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.cfg.authSessionTTL().Seconds()),
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(r),
		HttpOnly: true,
	})
	return nil
}

func (s *Server) clearAccessCookie(w http.ResponseWriter, r *http.Request) {
	secure := s.cookieSecure(r)
	for _, name := range []string{sessionCookieName, legacyAccessCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			SameSite: http.SameSiteLaxMode,
			Secure:   secure,
			HttpOnly: true,
		})
	}
}
