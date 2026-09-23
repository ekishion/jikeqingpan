package app

import (
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	persist      statePersistence
	persistMu    sync.Mutex
	flushStop    chan struct{}
	stateDirty   atomic.Bool
	version      string
}

// NewServer 创建并初始化应用服务器。
func NewServer(cfg *Config, staticContent fs.FS, version string) *Server {
	if version == "" {
		version = "dev"
	}
	sub, err := fs.Sub(staticContent, "static")
	if err != nil {
		// 允许测试传入已是 static 根的 FS
		sub = staticContent
	}
	s := &Server{
		cfg:          cfg,
		version:      version,
		limiter:      newRateLimiter(cfg.RateLimitPerSecond),
		mux:          http.NewServeMux(),
		baiduBaseURL: "https://pan.baidu.com",
		httpClient:   &http.Client{Timeout: baiduClientTimeout},
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
	s.restorePersistedState()
	s.startStateFlusher()
	return s
}

// Handler 返回 HTTP 路由处理器。
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Close 优雅关闭服务器的持久化与审计日志资源。
func (s *Server) Close() {
	s.closePersist()
	s.closeAudit()
}

// WriteTimeoutFor 按 list_max_pages 推导 WriteTimeout：最坏路径是
// maxPages 次串行百度请求（每次 15s 客户端超时 + 1s 余量），外加
// 60s 预览/README 流式转发余量；下限 120s 兜底常规场景。
func WriteTimeoutFor(cfg *Config) time.Duration {
	derived := 60*time.Second + time.Duration(cfg.listMaxPages())*16*time.Second
	if derived < 120*time.Second {
		return 120 * time.Second
	}
	return derived
}

// WarnRiskyConfig 启动时提示有风险但不阻断的配置组合。
func WarnRiskyConfig(cfg *Config) {
	if cfg.authEnabled() && len(cfg.AccessToken) < 16 {
		log.Printf("[WARN] access_token 长度不足 16 位，容易被穷举；建议用 openssl rand -hex 24 生成高熵令牌")
	}
	bindIP := net.ParseIP(cfg.BindAddress)
	bindLoopback := cfg.BindAddress == "localhost" || (bindIP != nil && bindIP.IsLoopback())
	if bindLoopback {
		return
	}
	for _, raw := range cfg.TrustedProxyIPs {
		raw = strings.TrimSpace(raw)
		loopback := false
		if ip := net.ParseIP(raw); ip != nil {
			loopback = ip.IsLoopback()
		} else if _, network, err := net.ParseCIDR(raw); err == nil {
			loopback = network.Contains(net.ParseIP("127.0.0.1")) || network.Contains(net.ParseIP("::1"))
		}
		if loopback {
			log.Printf("[WARN] trusted_proxy_ips 含环回地址 %q 且服务绑定在 %q：本机任意进程可伪造 X-Forwarded-For "+
				"获得新的限流桶并重置登录锁定。仅当反代确实经环回直连（非容器网络）时才安全；"+
				"容器部署请填写 docker 网关 CIDR 而非 127.0.0.1", raw, cfg.BindAddress)
			return
		}
	}
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

// ===== 状态持久化 =====

// restorePersistedState 启动时从持久化后端恢复短链状态。
// 读取失败（文件损坏等）时把原文件改名留存后重建，服务不因此拒绝启动。
func (s *Server) restorePersistedState() {
	persist, err := newStatePersistence(s.cfg)
	if err != nil {
		log.Printf("[WARN] 状态持久化初始化失败，本次运行仅内存模式: %v", err)
		return
	}
	if persist == nil {
		return
	}
	st, err := persist.Load()
	if err != nil {
		backup := s.cfg.StatePath + ".corrupt-" + strconv.FormatInt(time.Now().Unix(), 10)
		_ = persist.Close()
		if renameErr := os.Rename(s.cfg.StatePath, backup); renameErr == nil {
			log.Printf("[WARN] 状态文件损坏已留存为 %s: %v", backup, err)
		} else {
			log.Printf("[WARN] 状态文件损坏且无法留存: %v", err)
		}
		persist, err = newStatePersistence(s.cfg)
		if err != nil || persist == nil {
			return
		}
		st = &persistedState{Version: 1}
	}
	s.dirLinks.restore(st.DirLinks)
	s.shortLinks.restore(st.ShortLinks)
	s.cache.restoreDirCache(st.DirCache)
	s.persist = persist
	log.Printf("[状态] 已恢复目录短链 %d 条、下载短链 %d 条、目录缓存 %d 个", len(st.DirLinks), len(st.ShortLinks), len(st.DirCache))
}

// startStateFlusher 周期把变脏的状态落盘（30s）。
func (s *Server) startStateFlusher() {
	if s.persist == nil {
		return
	}
	s.flushStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !s.stateDirty.CompareAndSwap(true, false) {
					continue
				}
				if err := s.flushState(); err != nil {
					log.Printf("[WARN] 状态落盘失败，下个周期重试: %v", err)
					s.stateDirty.Store(true)
				}
			case <-s.flushStop:
				return
			}
		}
	}()
}

// markStateDirty 标记状态有变化，等待周期落盘。
func (s *Server) markStateDirty() {
	if s.persist != nil {
		s.stateDirty.Store(true)
	}
}

// flushState 立即把当前快照写入持久化后端。
// persistMu 串行化周期 flush 与关闭时的最终 flush：JSON 后端并发 Save
// 会竞争同一个 .tmp 文件，竞态下可能落盘损坏快照。
func (s *Server) flushState() error {
	if s.persist == nil {
		return nil
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	st := &persistedState{
		Version:    1,
		DirLinks:   s.dirLinks.snapshot(),
		ShortLinks: s.shortLinks.snapshot(),
		DirCache:   s.cache.exportDirCache(),
	}
	return s.persist.Save(st)
}

// closePersist 优雅关闭时强制落盘并关闭后端；可安全重复调用。
func (s *Server) closePersist() {
	if s.persist == nil {
		return
	}
	if s.flushStop != nil {
		close(s.flushStop)
		s.flushStop = nil
	}
	if err := s.flushState(); err != nil {
		log.Printf("[WARN] 关闭前状态落盘失败: %v", err)
	}
	if err := s.persist.Close(); err != nil {
		log.Printf("[WARN] 关闭持久化后端失败: %v", err)
	}
	// 迟到的周期 flush 变 no-op，避免打在已关闭的库上
	s.persist = nil
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

// staticRoutes 静态资源白名单：URL 路径 → 嵌入文件名。
// 新增静态文件只需在此登记一处（统一 no-store，避免部署后用旧界面）。
var staticRoutes = map[string]string{
	"/":            "index.html",
	"/index.html":  "index.html",
	"/app.js":      "app.js",
	"/icons.js":    "icons.js",
	"/markdown.js": "markdown.js",
	"/preview.js":  "preview.js",
	"/app.css":     "app.css",
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	name, ok := staticRoutes[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	http.ServeFileFS(w, r, s.staticRoot, name)
}
