// 临时盘 - Go后端
// 代理百度网盘青春版API，将Cookie保存在服务端，不暴露给前端
package main

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------- 配置 ----------

// Config 服务配置
type Config struct {
	Port               int    `json:"port"`
	BaiduCookie        string `json:"baidu_cookie"`
	RateLimitPerSecond int    `json:"rate_limit_per_second"`
	BaiduAppID         string `json:"baidu_app_id"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("port 必须在 1 到 65535 之间")
	}
	if cfg.RateLimitPerSecond == 0 {
		cfg.RateLimitPerSecond = 10
	}
	if cfg.RateLimitPerSecond < 1 {
		return nil, fmt.Errorf("rate_limit_per_second 必须大于 0")
	}
	if cfg.BaiduAppID == "" {
		cfg.BaiduAppID = "250528"
	}
	return &cfg, nil
}

// ---------- 频率限制 ----------

// RateLimiter 简单的令牌桶频率限制器（按IP）
type RateLimiter struct {
	mu       sync.Mutex
	clients  map[string]*clientState
	rate     int // 每秒允许的请求数
	interval time.Duration
}

type clientState struct {
	tokens   int
	lastSeen time.Time
}

func newRateLimiter(ratePerSecond int) *RateLimiter {
	rl := &RateLimiter{
		clients:  make(map[string]*clientState),
		rate:     ratePerSecond,
		interval: time.Second,
	}
	// 定期清理过期客户端记录
	go func() {
		for range time.Tick(time.Minute) {
			rl.mu.Lock()
			for ip, cs := range rl.clients {
				if time.Since(cs.lastSeen) > 5*time.Minute {
					delete(rl.clients, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

// Allow 判断该IP是否被允许访问
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cs, ok := rl.clients[ip]
	now := time.Now()
	if !ok {
		rl.clients[ip] = &clientState{tokens: rl.rate - 1, lastSeen: now}
		return true
	}

	// 按时间间隔补充令牌
	elapsed := now.Sub(cs.lastSeen)
	refill := int(elapsed / rl.interval)
	if refill > 0 {
		cs.tokens += refill * rl.rate
		if cs.tokens > rl.rate {
			cs.tokens = rl.rate
		}
		cs.lastSeen = now
	}

	if cs.tokens <= 0 {
		return false
	}
	cs.tokens--
	return true
}

// ---------- 路径校验 ----------

// validPathRe 只允许以/开头的合法路径，防止路径穿越
var validPathRe = regexp.MustCompile(`^/[^\x00-\x1f]*$`)

func isValidBaiduPath(p string) bool {
	if !validPathRe.MatchString(p) {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	// 拒绝会被规范化的路径，避免路径语义在校验后发生变化。
	cleaned := path.Clean(p)
	return cleaned == p && strings.HasPrefix(cleaned, "/")
}

// fileMeta 文件元数据，包含下载签名计算所需的 id 和 md5
type fileMeta struct {
	FsID          int64     `json:"fs_id"`
	MD5           string    `json:"md5"`
	DLink         string    `json:"dlink"`
	DLinkCachedAt time.Time `json:"-"`
}

// fileListCache 缓存文件列表中每个文件的元数据，按路径索引
type fileListCache struct {
	mu          sync.RWMutex
	filesByPath map[string]fileMeta // 文件路径 -> 元数据
	updatedAt   time.Time
}

const cachedDLinkTTL = 5 * time.Minute

func newFileListCache() *fileListCache {
	return &fileListCache{filesByPath: make(map[string]fileMeta)}
}

// update 解析 Baidu 列表响应并更新索引
func (c *fileListCache) update(listJSON []byte) {
	var resp struct {
		Errno int `json:"errno"`
		List  []struct {
			Path  string `json:"path"`
			FsID  int64  `json:"fs_id"`
			MD5   string `json:"md5"`
			DLink string `json:"dlink"`
		} `json:"list"`
	}
	if err := json.Unmarshal(listJSON, &resp); err != nil {
		log.Printf("[WARN] 解析文件列表失败: %v", err)
		return
	}
	if resp.Errno != 0 {
		// 截取前 200 字节便于调试，不记录完整响应（可能包含 Cookie 等敏感信息）
		preview := listJSON
		if len(preview) > 200 {
			preview = preview[:200]
		}
		log.Printf("[WARN] 百度API返回错误 errno=%d, 响应头: %s", resp.Errno, preview)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.filesByPath == nil {
		c.filesByPath = make(map[string]fileMeta)
	}
	for _, f := range resp.List {
		if f.Path != "" && f.FsID != 0 {
			c.filesByPath[f.Path] = fileMeta{
				FsID:          f.FsID,
				MD5:           f.MD5,
				DLink:         f.DLink,
				DLinkCachedAt: time.Now(),
			}
		}
	}
	c.updatedAt = time.Now()
	log.Printf("[缓存] 更新完成：共 %d 个文件，当前总共已缓存 %d 个元数据", len(resp.List), len(c.filesByPath))
}

// getFileMeta 根据路径获取文件元数据
func (c *fileListCache) getFileMeta(path string) (fileMeta, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	meta, ok := c.filesByPath[path]
	return meta, ok
}

func (c *fileListCache) getCachedDLink(filePath string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	meta, ok := c.filesByPath[filePath]
	if !ok || meta.DLink == "" || time.Since(meta.DLinkCachedAt) >= cachedDLinkTTL {
		return "", false
	}
	return meta.DLink, true
}

func (c *fileListCache) setDLink(filePath, dlink string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.filesByPath[filePath]
	if !ok {
		return
	}
	meta.DLink = dlink
	meta.DLinkCachedAt = time.Now()
	c.filesByPath[filePath] = meta
}

// ---------- 服务器 ----------

// Server 应用服务器
type Server struct {
	cfg          *Config
	limiter      *RateLimiter
	mux          *http.ServeMux
	baiduBaseURL string
	httpClient   *http.Client
	shortLinks   *shortLinkStore
	cache        *fileListCache // 文件列表缓存，dlink 存在服务端，不暴露给前端
	uk           int64          // 百度用户uk
	sk           string         // 百度授权sk
	sessionMu    sync.Mutex     // 保护 uk/sk 刷新
}

const (
	shortLinkTTL        = time.Hour
	csrfTokenCookieName = "csrf_token"
)

var shortLinkTokenRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

type shortLink struct {
	filePath  string
	expiresAt time.Time
}

type shortLinkStore struct {
	mu    sync.Mutex
	links map[string]shortLink
	ttl   time.Duration
}

func newShortLinkStore(ttl time.Duration) *shortLinkStore {
	if ttl <= 0 {
		ttl = shortLinkTTL
	}
	return &shortLinkStore{
		links: make(map[string]shortLink),
		ttl:   ttl,
	}
}

func (s *shortLinkStore) create(filePath string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, link := range s.links {
		if !now.Before(link.expiresAt) {
			delete(s.links, token)
		}
	}

	for i := 0; i < 3; i++ {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("生成短链接令牌失败: %w", err)
		}
		token := hex.EncodeToString(buf)
		if _, exists := s.links[token]; exists {
			continue
		}
		s.links[token] = shortLink{filePath: filePath, expiresAt: now.Add(s.ttl)}
		return token, nil
	}
	return "", fmt.Errorf("生成短链接令牌失败: 随机令牌冲突")
}

func (s *shortLinkStore) resolve(token string) (string, bool) {
	if !shortLinkTokenRe.MatchString(token) {
		return "", false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.links[token]
	if !ok {
		return "", false
	}
	if !time.Now().Before(link.expiresAt) {
		delete(s.links, token)
		return "", false
	}
	return link.filePath, true
}

func newServer(cfg *Config) *Server {
	s := &Server{
		cfg:          cfg,
		limiter:      newRateLimiter(cfg.RateLimitPerSecond),
		mux:          http.NewServeMux(),
		baiduBaseURL: "https://pan.baidu.com",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		shortLinks:   newShortLinkStore(shortLinkTTL),
		cache:        newFileListCache(),
	}
	s.mux.HandleFunc("/api/files", s.withSecurity(s.handleFiles))
	s.mux.HandleFunc("/api/download", s.withSecurity(s.handleDownload))
	s.mux.HandleFunc("/d/", s.withSecurity(s.handleShortDownload))
	// 使用 FileServer 提供 static/ 目录下的所有静态资源（index.html, app.js 等）
	staticFS := http.FileServer(http.Dir("static"))
	s.mux.HandleFunc("/", s.withSecurity(func(w http.ResponseWriter, r *http.Request) {
		// 只允许访问 / 和已知静态文件，防止目录枚举
		allowed := map[string]bool{"/": true, "/index.html": true, "/app.js": true}
		if !allowed[r.URL.Path] {
			http.NotFound(w, r)
			return
		}
		staticFS.ServeHTTP(w, r)
	}))
	return s
}

// withSecurity 统一安全中间件：添加安全响应头 + 频率限制
func (s *Server) withSecurity(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 查询接口允许 GET；文件列表和短链接申请允许带 CSRF 令牌的 POST。
		isProtectedPost := r.Method == http.MethodPost &&
			(r.URL.Path == "/api/download" || r.URL.Path == "/api/files")
		if r.Method != http.MethodGet && !isProtectedPost {
			http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}

		// 设置安全响应头
		// TODO(security): CSP nonce 未实现，当前使用严格的 default-src 'self'
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; object-src 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet {
			if _, err := r.Cookie(csrfTokenCookieName); err != nil {
				token, tokenErr := newCSRFToken()
				if tokenErr != nil {
					http.Error(w, "生成安全令牌失败", http.StatusInternalServerError)
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     csrfTokenCookieName,
					Value:    token,
					Path:     "/",
					MaxAge:   3600,
					SameSite: http.SameSiteLaxMode,
					Secure:   r.TLS != nil,
				})
			}
		}
		if isProtectedPost && !hasValidCSRFToken(r) {
			http.Error(w, "CSRF 令牌无效", http.StatusForbidden)
			return
		}

		// TODO(security): 当前仅限本地访问，如需对外开放需添加认证（OAuth/JWT）
		// RemoteAddr 包含端口；只使用主机部分，否则同一客户端可通过更换
		// 临时端口绕过限流。未配置可信代理时不信任 X-Forwarded-For。
		ip := clientIP(r.RemoteAddr)

		isStatic := r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/app.js"
		if !isStatic {
			if !s.limiter.Allow(ip) {
				http.Error(w, "请求过于频繁，请稍后重试", http.StatusTooManyRequests)
				return
			}
		}

		next(w, r)
	}
}

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

// ---------- API处理 ----------

// fetchFileList 拉取百度网盘文件列表。下载直链只在服务端按需生成。
func (s *Server) fetchFileList(dir string) ([]byte, error) {
	if dir == "" {
		dir = "/"
	}
	apiURL := fmt.Sprintf(
		s.baiduBaseURL+"/youth/api/list?clienttype=0&app_id=%s&web=1&order=time&desc=1&num=100&page=1&dlink=1&dir=%s",
		url.QueryEscape(s.cfg.BaiduAppID),
		url.QueryEscape(dir),
	)
	return s.baiduGet(apiURL, "")
}

// handleFiles 获取文件列表，代理百度网盘 list API
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	var dir string
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
		var request struct {
			Dir string `json:"dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "请求体格式错误", http.StatusBadRequest)
			return
		}
		dir = request.Dir
	} else {
		dir = r.URL.Query().Get("dir")
	}
	if dir == "" {
		dir = "/"
	}
	if !isValidBaiduPath(dir) {
		log.Printf("[WARN] 获取文件列表的非法路径被拒绝: %q", dir)
		http.Error(w, "非法路径", http.StatusBadRequest)
		return
	}

	body, err := s.fetchFileList(dir)
	if err != nil {
		log.Printf("[ERROR] 获取文件列表失败: %v", err)
		http.Error(w, "获取文件列表失败", http.StatusBadGateway)
		return
	}

	// 直链只缓存于服务端，返回给前端前必须移除 dlink 字段。
	s.cache.update(body)
	publicBody, err := stripDownloadLinks(body)
	if err != nil {
		log.Printf("[ERROR] 清理文件列表直链失败: %v", err)
		http.Error(w, "处理文件列表失败", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(publicBody)
}

func stripDownloadLinks(body []byte) ([]byte, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	listRaw, ok := response["list"]
	if !ok {
		return body, nil
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(listRaw, &list); err != nil {
		return nil, err
	}
	for _, item := range list {
		delete(item, "dlink")
	}
	cleanList, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	response["list"] = cleanList
	return json.Marshal(response)
}

// fetchUserSession 从百度接口获取 uk 与 sk
func (s *Server) fetchUserSession() (int64, string, error) {
	// 1. 尝试从 /youth/api/user/getinfo 获取 uk/sk
	apiURL := fmt.Sprintf(
		s.baiduBaseURL+"/youth/api/user/getinfo?app_id=%s&clienttype=0&web=1&need_selfinfo=1",
		url.QueryEscape(s.cfg.BaiduAppID),
	)
	body, err := s.baiduGet(apiURL, "")
	var uk int64
	var sk string
	if err == nil {
		var resp struct {
			Errno   int `json:"errno"`
			Records []struct {
				Uk int64  `json:"uk"`
				Sk string `json:"sk"`
			} `json:"records"`
		}
		if json.Unmarshal(body, &resp) == nil && len(resp.Records) > 0 {
			uk = resp.Records[0].Uk
			sk = resp.Records[0].Sk
		}
	}

	// 2. 如果不完整，从 /api/gettemplatevariable 获取
	if uk == 0 || sk == "" {
		fallbackURL := s.baiduBaseURL + `/api/gettemplatevariable?fields=["bdstoken","uk","sk"]`
		body2, err2 := s.baiduGet(fallbackURL, "")
		if err2 == nil {
			var resp2 struct {
				Result struct {
					Uk int64  `json:"uk"`
					Sk string `json:"sk"`
				} `json:"result"`
			}
			if json.Unmarshal(body2, &resp2) == nil {
				if uk == 0 {
					uk = resp2.Result.Uk
				}
				if sk == "" {
					sk = resp2.Result.Sk
				}
			}
		}
	}

	// 3. 如果还是没有 sk，从 /youth/api/report/user 获取
	if sk == "" && uk != 0 {
		skURL := fmt.Sprintf(
			s.baiduBaseURL+"/youth/api/report/user?app_id=%s&clienttype=0&web=1&action=sapi_auth&timestamp=%d",
			url.QueryEscape(s.cfg.BaiduAppID),
			time.Now().UnixMilli(),
		)
		bodySK, errSK := s.baiduGet(skURL, "")
		if errSK == nil {
			var respSK struct {
				Uinfo string `json:"uinfo"`
			}
			if json.Unmarshal(bodySK, &respSK) == nil && respSK.Uinfo != "" {
				sk = respSK.Uinfo
			}
		}
	}

	if uk == 0 || sk == "" {
		return 0, "", fmt.Errorf("无法从百度网盘获取完整的 uk (%d) 或 sk (%q)", uk, sk)
	}

	return uk, sk, nil
}

// getSession 线程安全地获取或刷新 uk/sk
func (s *Server) getSession() (int64, string, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.uk != 0 && s.sk != "" {
		return s.uk, s.sk, nil
	}
	uk, sk, err := s.fetchUserSession()
	if err != nil {
		return 0, "", err
	}
	s.uk = uk
	s.sk = sk
	log.Printf("[Session] 获取成功, uk: %d", uk)
	return uk, sk, nil
}

// locatedownloadRand 使用 SHA-1 计算位于下载的 rand 参数
func locatedownloadRand(uk int64, sk string, nowMilli int64) string {
	data := fmt.Sprintf("%d%s%d0", uk, sk, nowMilli)
	h := sha1.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// locatedownloadSign 使用 MD5 计算位于下载的 sign 参数
func locatedownloadSign(fileMD5 string, fileID string, uk int64, nowMilli int64) string {
	data := fmt.Sprintf("%s_%d_%s_%d", fileMD5, uk, fileID, nowMilli)
	h := md5.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// getBaiduDLink 计算签名并向百度 locatedownload 接口获取直链（包含 sk 过期自动重试）
func (s *Server) getBaiduDLink(filePath string, ua string) (string, error) {
	// 1. 获取文件元数据
	meta, ok := s.cache.getFileMeta(filePath)
	if !ok {
		parentDir := path.Dir(filePath)
		log.Printf("[缓存] 未命中 %q，重新拉取父目录 %q 的文件列表", filePath, parentDir)
		listBody, err := s.fetchFileList(parentDir)
		if err != nil {
			return "", fmt.Errorf("重新拉取文件列表失败: %w", err)
		}
		s.cache.update(listBody)
		meta, ok = s.cache.getFileMeta(filePath)
	}

	if !ok {
		// 调试：列出当前缓存中所有路径
		s.cache.mu.RLock()
		for p := range s.cache.filesByPath {
			log.Printf("[调试] 缓存中有路径: %q", p)
		}
		s.cache.mu.RUnlock()
		return "", fmt.Errorf("在缓存中找不到路径: %s", filePath)
	}
	if cachedDLink, ok := s.cache.getCachedDLink(filePath); ok {
		return withPrivateCacheControl(cachedDLink), nil
	}

	// 2. 获取或刷新 uk/sk
	uk, sk, err := s.getSession()
	if err != nil {
		return "", fmt.Errorf("获取百度Session失败: %w", err)
	}

	// 3. 计算签名并调用 locatedownload。百度有时会用 HTTP 200 返回
	// errno 错误，因此响应解析失败也必须进入一次 Session 刷新重试。
	body, err := s.requestDLink(filePath, meta, uk, sk, ua)
	dlink, parseErr := parseDLinkResponse(body)
	if err != nil || parseErr != nil {
		if err != nil {
			log.Printf("[WARN] 首次 locatedownload 失败: %v，尝试清除 sk 并重试", err)
		} else {
			log.Printf("[WARN] 首次 locatedownload 返回错误: %v，尝试清除 sk 并重试", parseErr)
		}
		s.sessionMu.Lock()
		s.sk = ""
		s.sessionMu.Unlock()

		uk, sk, err = s.getSession()
		if err != nil {
			return "", fmt.Errorf("重新获取Session失败: %w", err)
		}
		body, err = s.requestDLink(filePath, meta, uk, sk, ua)
		if err != nil {
			return "", fmt.Errorf("重试 locatedownload 失败: %w", err)
		}
		dlink, err = parseDLinkResponse(body)
		if err != nil {
			return "", fmt.Errorf("重试 locatedownload 失败: %w", err)
		}
	}

	dlink = withPrivateCacheControl(dlink)
	s.cache.setDLink(filePath, dlink)

	return dlink, nil
}

func withPrivateCacheControl(dlink string) string {
	if strings.Contains(dlink, "response-cache-control=") {
		return dlink
	}
	sep := "&"
	if !strings.Contains(dlink, "?") {
		sep = "?"
	}
	return dlink + sep + "response-cache-control=private"
}

func (s *Server) requestDLink(filePath string, meta fileMeta, uk int64, sk, ua string) ([]byte, error) {
	nowMilli := time.Now().UnixMilli()
	randVal := locatedownloadRand(uk, sk, nowMilli)
	signVal := locatedownloadSign(meta.MD5, strconv.FormatInt(meta.FsID, 10), uk, nowMilli)
	locateURL := fmt.Sprintf(
		s.baiduBaseURL+"/youth/api/locatedownload?app_id=%s&clienttype=0&web=1&devuid=0&dp-logid=%d&path=%s&rand=%s&sign=%s&time=%d",
		url.QueryEscape(s.cfg.BaiduAppID),
		time.Now().UnixNano(),
		url.QueryEscape(filePath),
		randVal,
		signVal,
		nowMilli,
	)
	return s.baiduGet(locateURL, ua)
}

func parseDLinkResponse(body []byte) (string, error) {
	var resp struct {
		Errno   int    `json:"errno"`
		ShowMsg string `json:"show_msg"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析 locatedownload 响应失败: %w", err)
	}
	if resp.Errno != 0 || resp.URL == "" {
		return "", fmt.Errorf("百度 locatedownload 返回错误 errno=%d, msg=%q", resp.Errno, resp.ShowMsg)
	}
	return resp.URL, nil
}

// handleDownload 获取百度网盘文件直链并返回
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	var filePath string
	format := r.URL.Query().Get("format")
	if r.Method == http.MethodPost {
		// POST 请求把路径放在请求体中，避免出现在 URL、历史记录和代理访问日志里。
		r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
		var request struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "请求体格式错误", http.StatusBadRequest)
			return
		}
		filePath = request.Path
		format = "json"
	} else {
		filePath = r.URL.Query().Get("path")
	}
	if filePath == "" {
		http.Error(w, "缺少 path 参数", http.StatusBadRequest)
		return
	}

	if !isValidBaiduPath(filePath) {
		log.Printf("[WARN] 非法路径请求被拒绝: %q", filePath)
		http.Error(w, "非法路径", http.StatusBadRequest)
		return
	}

	if format == "json" {
		// 只返回不包含真实路径的短链接，真实直链在短链接请求时生成。
		token, err := s.shortLinks.create(filePath)
		if err != nil {
			log.Printf("[ERROR] 创建短链接失败: %v", err)
			http.Error(w, "创建短链接失败", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		respJSON, err := downloadJSONResponse("/d/" + token)
		if err != nil {
			http.Error(w, "生成直链响应失败", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(respJSON)
		return
	}

	s.redirectToBaiduDownload(w, r, filePath)
}

func (s *Server) handleShortDownload(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/d/")
	filePath, ok := s.shortLinks.resolve(token)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.redirectToBaiduDownload(w, r, filePath)
}

func (s *Server) redirectToBaiduDownload(w http.ResponseWriter, r *http.Request, filePath string) {
	dlink, err := s.getBaiduDLink(filePath, r.Header.Get("User-Agent"))
	if err != nil {
		log.Printf("[ERROR] 获取百度直链失败: %v", err)
		http.Error(w, "获取直链失败", http.StatusBadGateway)
		return
	}
	// 浏览器直接请求时，重定向到真实直链以触发下载。
	http.Redirect(w, r, dlink, http.StatusFound)
}

func downloadJSONResponse(dlink string) ([]byte, error) {
	return json.Marshal(struct {
		URLs []struct {
			URL string `json:"url"`
		} `json:"urls"`
	}{URLs: []struct {
		URL string `json:"url"`
	}{{URL: dlink}}})
}

// ---------- 百度网盘请求 ----------

// baiduGet 向百度网盘API发起GET请求，Cookie由服务端注入
func (s *Server) baiduGet(apiURL string, ua string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// Cookie只在服务端注入，绝不返回给客户端
	req.Header.Set("Cookie", s.cfg.BaiduCookie)
	if ua == "" {
		ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Referer", "https://pan.baidu.com/")

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求百度网盘API失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("百度网盘API返回非200状态: %d", resp.StatusCode)
	}

	// 限制读取大小，防止响应过大
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	return body, nil
}

// ---------- 主函数 ----------

func main() {
	cfg, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	if cfg.BaiduCookie == "" {
		log.Fatal("配置文件中 baidu_cookie 不能为空")
	}

	srv := newServer(cfg)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("临时盘启动，访问地址: http://localhost%s", addr)
	server := &http.Server{
		Addr:              addr,
		Handler:           srv.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}
