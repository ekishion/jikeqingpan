package main

import (
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Server 应用服务器
type Server struct {
	cfg          *Config
	limiter      *RateLimiter
	mux          *http.ServeMux
	baiduBaseURL string
	httpClient   *http.Client
	shortLinks   *shortLinkStore
	dirLinks     *dirLinkStore
	cache        *fileListCache
	staticRoot   fs.FS
	sessions     *sessionManager
	loginGuard   *loginGuard
	uk           int64
	sk           string
	sessionAt    time.Time
	sessionMu    sync.Mutex
	auditMu      sync.Mutex
	auditFile    *os.File
	auditSize    int64
	auditMax     int64
	trustedProxy []*net.IPNet
}

func newServer(cfg *Config, staticContent fs.FS) *Server {
	sub, err := fs.Sub(staticContent, "static")
	if err != nil {
		// 允许测试传入已是 static 根的 FS
		sub = staticContent
	}
	s := &Server{
		cfg:          cfg,
		limiter:      newRateLimiter(cfg.RateLimitPerSecond),
		mux:          http.NewServeMux(),
		baiduBaseURL: "https://pan.baidu.com",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		shortLinks:   newShortLinkStore(cfg.shortLinkTTL(), cfg.ShortLinkMaxUses),
		dirLinks:     newDirLinkStore(cfg.dirLinkTTL()),
		cache:        newFileListCacheWithLimits(maxCachedFiles, cfg.fileCacheTTL(), cfg.dlinkCacheTTL()),
		auditMax:     defaultAuditMaxBytes,
		staticRoot:   sub,
		sessions:     newSessionManager(cfg.sessionSigningKey(), cfg.authSessionTTL()),
		loginGuard:   newLoginGuard(),
	}
	for _, raw := range cfg.TrustedProxyIPs {
		raw = strings.TrimSpace(raw)
		if ip := net.ParseIP(raw); ip != nil {
			bits := 128
			if ip4 := ip.To4(); ip4 != nil {
				ip, bits = ip4, 32
			}
			s.trustedProxy = append(s.trustedProxy, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		} else if _, network, err := net.ParseCIDR(raw); err == nil {
			s.trustedProxy = append(s.trustedProxy, network)
		}
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/auth/status", s.withSecurity(s.handleAuthStatus, securityOpts{
		requireAuth: false,
		csrf:        false,
		rateLimit:   true,
	}))
	s.mux.HandleFunc("/api/login", s.withSecurity(s.handleLogin, securityOpts{
		requireAuth: false,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/logout", s.withSecurity(s.handleLogout, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/files", s.withSecurity(s.handleFiles, securityOpts{
		requireAuth: true,
		csrf:        true, // POST 校验；GET 跳过
		rateLimit:   true,
		methods:     []string{http.MethodGet, http.MethodPost},
	}))
	s.mux.HandleFunc("/api/dir-link", s.withSecurity(s.handleDirLink, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/readme", s.withSecurity(s.handleReadme, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/download", s.withSecurity(s.handleDownload, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/preview", s.withSecurity(s.handlePreview, securityOpts{
		requireAuth: true, csrf: true, rateLimit: true, methods: []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/text", s.withSecurity(s.handleTextPreview, securityOpts{
		requireAuth: true, csrf: true, rateLimit: true, methods: []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/healthz", s.withSecurity(s.handleHealthz, securityOpts{
		requireAuth: false,
		csrf:        false,
		rateLimit:   false,
		methods:     []string{http.MethodGet, http.MethodHead},
	}))
	s.mux.HandleFunc("/readyz", s.withSecurity(s.handleReadyz, securityOpts{
		requireAuth: true, // 避免未鉴权刷百度探测
		csrf:        false,
		rateLimit:   true,
	}))
	s.mux.HandleFunc("/d/", s.withSecurity(s.handleShortDownload, securityOpts{
		requireAuth: true,
		csrf:        false,
		rateLimit:   true,
		methods:     []string{http.MethodGet, http.MethodHead},
	}))
	s.mux.HandleFunc("/", s.withSecurity(s.handleStatic, securityOpts{
		requireAuth: false, // 登录页需要能加载静态资源
		csrf:        false,
		rateLimit:   false,
		methods:     []string{http.MethodGet, http.MethodHead},
	}))
}

type securityOpts struct {
	requireAuth bool
	csrf        bool
	rateLimit   bool
	methods     []string
}

func (s *Server) withSecurity(next http.HandlerFunc, opts securityOpts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 方法限制
		if len(opts.methods) > 0 {
			ok := false
			for _, m := range opts.methods {
				if r.Method == m {
					ok = true
					break
				}
			}
			if !ok {
				writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方式不受支持")
				return
			}
		} else if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方式不受支持")
			return
		}

		// 安全响应头
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		// media-src 与 pathsec.go 的下载域名白名单保持一致：<video>/<audio> 经 /d/{token}
		// 302 到百度直链播放，重定向目标必须被 media-src 允许，否则被默认 'self' 拦截。
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; "+
				"media-src 'self' https://baidu.com https://*.baidu.com https://baidupcs.com https://*.baidupcs.com https://bdstatic.com https://*.bdstatic.com; "+
				"frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cache-Control", "no-store")

		// CSRF cookie 下发（GET；健康检查无需 Cookie，避免探活请求反复种 Cookie）
		if r.Method == http.MethodGet && r.URL.Path != "/healthz" {
			if _, err := r.Cookie(csrfTokenCookieName); err != nil {
				token, tokenErr := newCSRFToken()
				if tokenErr != nil {
					writeJSONError(w, http.StatusInternalServerError, "csrf_issue", "服务异常，请刷新页面重试")
					return
				}
				s.setCSRFCookie(w, r, token)
			}
		}

		// CSRF 校验：仅保护 POST 且 opts.csrf
		if opts.csrf && r.Method == http.MethodPost {
			if !hasValidCSRFToken(r) {
				writeJSONError(w, http.StatusForbidden, "csrf_invalid", "页面已过期，请刷新后重试")
				return
			}
			if cookie, err := r.Cookie(csrfTokenCookieName); err == nil && cookie.Value != "" {
				s.setCSRFCookie(w, r, cookie.Value)
			}
		}

		// 访问鉴权
		if opts.requireAuth && s.cfg.authEnabled() && !s.hasValidAccess(r) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "请先验证访问权限")
			return
		}

		// 限流
		if opts.rateLimit {
			ip := s.requestClientIP(r)
			if !s.limiter.Allow(ip) {
				writeJSONError(w, http.StatusTooManyRequests, "rate_limited", "操作太频繁了，请稍后再试")
				return
			}
		}

		next(w, r)
	}
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	// 只允许访问已知静态文件，防止目录枚举
	var name string
	switch r.URL.Path {
	case "/", "/index.html":
		name = "index.html"
	case "/app.js":
		name = "app.js"
	case "/icons.js":
		name = "icons.js"
	case "/markdown.js":
		name = "markdown.js"
	case "/preview.js":
		name = "preview.js"
	case "/app.css":
		name = "app.css"
	default:
		http.NotFound(w, r)
		return
	}
	// HTML/JS/CSS 更新频繁且嵌入二进制，禁用强缓存，避免部署后仍用旧界面
	switch name {
	case "index.html", "app.js", "icons.js", "markdown.js", "preview.js", "app.css":
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Pragma", "no-cache")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	http.ServeFileFS(w, r, s.staticRoot, name)
}
