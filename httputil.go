package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
)

const (
	csrfTokenCookieName   = "csrf_token"
	accessTokenCookieName = "access_token"
	csrfTokenMaxAge       = 3600
	accessTokenMaxAge     = 7 * 24 * 3600 // 7 天，适合少数人反复访问
	defaultBaiduUA        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
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

// extractAccessToken 从 Header / Cookie 提取访问令牌（不接受 query，避免进日志）。
func extractAccessToken(r *http.Request) string {
	if h := strings.TrimSpace(r.Header.Get("X-Access-Token")); h != "" {
		return h
	}
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		const prefix = "Bearer "
		if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
	}
	if c, err := r.Cookie(accessTokenCookieName); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

func subtleCompareToken(got, want string) bool {
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) hasValidAccess(r *http.Request) bool {
	if s.cfg == nil || !s.cfg.authEnabled() {
		return true
	}
	return subtleCompareToken(extractAccessToken(r), s.cfg.AccessToken)
}

func (s *Server) setAccessCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessTokenCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   accessTokenMaxAge,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(r),
		HttpOnly: true,
	})
}

func (s *Server) clearAccessCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(r),
		HttpOnly: true,
	})
}
