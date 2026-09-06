package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 服务配置
type Config struct {
	Port               int    `json:"port"`
	BindAddress        string `json:"bind_address"`
	BaiduCookie        string `json:"baidu_cookie"`
	RateLimitPerSecond int    `json:"rate_limit_per_second"`
	BaiduAppID         string `json:"baidu_app_id"`

	// AccessToken 非空时启用全站访问控制（API 与下载短链均需鉴权）。
	// 适合 VPS 给少数用户使用：共享一个长随机令牌即可。
	AccessToken string `json:"access_token"`

	// SessionSecret 用于给登录会话签名的密钥。为空时由 AccessToken 派生，
	// 因此零配置即可用；显式修改该值可一键吊销所有已登录会话。
	SessionSecret string `json:"session_secret"`

	// AuthSessionTTLSeconds 登录会话有效期，默认 7 天。会话令牌内嵌过期时间，
	// 客户端无法延长；配合 SessionSecret 轮换实现服务端强制下线。
	AuthSessionTTLSeconds int `json:"auth_session_ttl_seconds"`

	// ForceSecureCookie 在 TLS 终结于反向代理时强制 Secure Cookie。
	ForceSecureCookie bool `json:"force_secure_cookie"`

	// ShortLinkTTLSeconds 短链有效期，默认 3600。
	ShortLinkTTLSeconds int `json:"short_link_ttl_seconds"`
	// ShortLinkMaxUses 短链最大使用次数；0 表示不限制。
	ShortLinkMaxUses int `json:"short_link_max_uses"`

	// DirLinkTTLSeconds 目录短链有效期，默认 7 天。目录短链用于地址栏
	// 分享/书签（?d=令牌），重启后失效（内存模型）。
	DirLinkTTLSeconds int `json:"dir_link_ttl_seconds"`

	// SessionTTLSeconds 百度 uk/sk 会话缓存有效期，默认 3600。
	SessionTTLSeconds  int      `json:"session_ttl_seconds"`
	TrustedProxyIPs    []string `json:"trusted_proxy_ips"`
	AuditLogPath       string   `json:"audit_log_path"`
	AllowedPaths       []string `json:"allowed_paths"`
	ShowReadme         *bool    `json:"show_readme"`
	ShowReadmeOverview *bool    `json:"show_readme_overview"`

	// ListMaxPages 目录列表自动翻页上限；0 表示默认 15 页（每页 100 项）。
	ListMaxPages int `json:"list_max_pages"`
	// PreviewMaxBytes 图片预览大小上限；0 表示默认 16 MB。
	PreviewMaxBytes int `json:"preview_max_bytes"`
	// ReadmeMaxBytes README 内容大小上限；0 表示默认 512 KB。
	ReadmeMaxBytes int `json:"readme_max_bytes"`
	// FileCacheTTLSeconds 文件元数据缓存有效期；0 表示默认 15 分钟。
	FileCacheTTLSeconds int `json:"file_cache_ttl_seconds"`
	// DlinkCacheTTLSeconds 下载直链缓存有效期；0 表示默认 5 分钟。
	DlinkCacheTTLSeconds int `json:"dlink_cache_ttl_seconds"`
}

func (c *Config) showReadme() bool {
	return c.ShowReadme == nil || *c.ShowReadme
}

func (c *Config) showReadmeOverview() bool {
	return c.ShowReadmeOverview == nil || *c.ShowReadmeOverview
}

func (c *Config) shortLinkTTL() time.Duration {
	if c.ShortLinkTTLSeconds <= 0 {
		return time.Hour
	}
	return time.Duration(c.ShortLinkTTLSeconds) * time.Second
}

// dirLinkTTL 目录短链有效期，默认 7 天。
func (c *Config) dirLinkTTL() time.Duration {
	if c.DirLinkTTLSeconds <= 0 {
		return 7 * 24 * time.Hour
	}
	return time.Duration(c.DirLinkTTLSeconds) * time.Second
}

func (c *Config) sessionTTL() time.Duration {
	if c.SessionTTLSeconds <= 0 {
		return time.Hour
	}
	return time.Duration(c.SessionTTLSeconds) * time.Second
}

// authSessionTTL 登录会话有效期，默认 7 天。
func (c *Config) authSessionTTL() time.Duration {
	if c.AuthSessionTTLSeconds <= 0 {
		return 7 * 24 * time.Hour
	}
	return time.Duration(c.AuthSessionTTLSeconds) * time.Second
}

// listMaxPages 目录列表自动翻页上限，默认 15 页（每页 100，即 1500 项）。
func (c *Config) listMaxPages() int {
	if c.ListMaxPages > 0 {
		return c.ListMaxPages
	}
	return 15
}

// previewMaxBytes 图片预览大小上限，默认 16 MB。
func (c *Config) previewMaxBytes() int {
	if c.PreviewMaxBytes > 0 {
		return c.PreviewMaxBytes
	}
	return 16 * 1024 * 1024
}

// readmeMaxBytes README 内容大小上限，默认 512 KB。
func (c *Config) readmeMaxBytes() int {
	if c.ReadmeMaxBytes > 0 {
		return c.ReadmeMaxBytes
	}
	return 512 * 1024
}

// fileCacheTTL 文件元数据缓存有效期，默认 15 分钟。
func (c *Config) fileCacheTTL() time.Duration {
	if c.FileCacheTTLSeconds > 0 {
		return time.Duration(c.FileCacheTTLSeconds) * time.Second
	}
	return 15 * time.Minute
}

// dlinkCacheTTL 下载直链缓存有效期，默认 5 分钟。
func (c *Config) dlinkCacheTTL() time.Duration {
	if c.DlinkCacheTTLSeconds > 0 {
		return time.Duration(c.DlinkCacheTTLSeconds) * time.Second
	}
	return 5 * time.Minute
}

// sessionSigningKey 派生会话签名密钥：优先用显式 SessionSecret，
// 否则由 AccessToken 派生（轮换 AccessToken 会自动失效旧会话）。
func (c *Config) sessionSigningKey() []byte {
	base := strings.TrimSpace(c.SessionSecret)
	if base == "" {
		base = c.AccessToken
	}
	mac := hmac.New(sha256.New, []byte(base))
	mac.Write([]byte("jikeqingpan/session/v1"))
	return mac.Sum(nil)
}

func loadConfig(path string) (*Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
		// 文件不存在时允许纯环境变量启动（Docker/编排友好）。
	} else {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件失败: %w", err)
		}
	}
	applyEnvOverrides(&cfg)
	if err := cfg.normalizeAndValidate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyEnvOverrides 允许 Docker/编排用环境变量覆盖配置中的敏感项与监听参数。
// 环境变量优先于 config.json 中的同名字段。
func applyEnvOverrides(c *Config) {
	if n, ok := envInt("PORT"); ok {
		c.Port = n
	}
	if v := strings.TrimSpace(os.Getenv("BIND_ADDRESS")); v != "" {
		c.BindAddress = v
	}
	if v := strings.TrimSpace(os.Getenv("BAIDU_COOKIE")); v != "" {
		c.BaiduCookie = v
	}
	if v := strings.TrimSpace(os.Getenv("ACCESS_TOKEN")); v != "" {
		c.AccessToken = v
	}
	if v := strings.TrimSpace(os.Getenv("SESSION_SECRET")); v != "" {
		c.SessionSecret = v
	}
	if n, ok := envInt("AUTH_SESSION_TTL_SECONDS"); ok {
		c.AuthSessionTTLSeconds = n
	}
	if v := strings.TrimSpace(os.Getenv("BAIDU_APP_ID")); v != "" {
		c.BaiduAppID = v
	}
	if v := strings.TrimSpace(os.Getenv("FORCE_SECURE_COOKIE")); v != "" {
		c.ForceSecureCookie = parseBoolEnv(v)
	}
	if n, ok := envInt("RATE_LIMIT_PER_SECOND"); ok {
		c.RateLimitPerSecond = n
	}
	if n, ok := envInt("SHORT_LINK_TTL_SECONDS"); ok {
		c.ShortLinkTTLSeconds = n
	}
	if n, ok := envInt("SHORT_LINK_MAX_USES"); ok {
		c.ShortLinkMaxUses = n
	}
	if n, ok := envInt("DIR_LINK_TTL_SECONDS"); ok {
		c.DirLinkTTLSeconds = n
	}
	if n, ok := envInt("SESSION_TTL_SECONDS"); ok {
		c.SessionTTLSeconds = n
	}
	if v := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_IPS")); v != "" {
		c.TrustedProxyIPs = strings.Split(v, ",")
	}
	if v := strings.TrimSpace(os.Getenv("AUDIT_LOG_PATH")); v != "" {
		c.AuditLogPath = v
	}
	if v := strings.TrimSpace(os.Getenv("ALLOWED_PATHS")); v != "" {
		c.AllowedPaths = strings.Split(v, ",")
	}
	if v := strings.TrimSpace(os.Getenv("SHOW_README")); v != "" {
		value := parseBoolEnv(v)
		c.ShowReadme = &value
	}
	if v := strings.TrimSpace(os.Getenv("SHOW_README_OVERVIEW")); v != "" {
		value := parseBoolEnv(v)
		c.ShowReadmeOverview = &value
	}
	if n, ok := envInt("LIST_MAX_PAGES"); ok {
		c.ListMaxPages = n
	}
	if n, ok := envInt("PREVIEW_MAX_BYTES"); ok {
		c.PreviewMaxBytes = n
	}
	if n, ok := envInt("README_MAX_BYTES"); ok {
		c.ReadmeMaxBytes = n
	}
	if n, ok := envInt("FILE_CACHE_TTL_SECONDS"); ok {
		c.FileCacheTTLSeconds = n
	}
	if n, ok := envInt("DLINK_CACHE_TTL_SECONDS"); ok {
		c.DlinkCacheTTLSeconds = n
	}
}

func envInt(key string) (int, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("[WARN] 环境变量 %s 不是有效整数: %q，已忽略", key, v)
		return 0, false
	}
	return n, true
}

func parseBoolEnv(v string) bool {
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func (c *Config) normalizeAndValidate() error {
	if c.Port == 0 {
		c.Port = 4172
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port 必须在 1 到 65535 之间")
	}
	if strings.TrimSpace(c.BindAddress) == "" {
		c.BindAddress = "127.0.0.1"
	}
	if c.RateLimitPerSecond == 0 {
		c.RateLimitPerSecond = 10
	}
	if c.RateLimitPerSecond < 1 {
		return fmt.Errorf("rate_limit_per_second 必须大于 0")
	}
	if c.BaiduAppID == "" {
		c.BaiduAppID = "250528"
	}
	if strings.TrimSpace(c.BaiduCookie) == "" {
		return fmt.Errorf("baidu_cookie 不能为空（可写在配置文件，或通过环境变量 BAIDU_COOKIE 注入）")
	}
	if c.ShortLinkTTLSeconds < 0 {
		return fmt.Errorf("short_link_ttl_seconds 不能为负数")
	}
	if c.DirLinkTTLSeconds < 0 {
		return fmt.Errorf("dir_link_ttl_seconds 不能为负数")
	}
	if c.ShortLinkMaxUses < 0 {
		return fmt.Errorf("short_link_max_uses 不能为负数")
	}
	if c.SessionTTLSeconds < 0 {
		return fmt.Errorf("session_ttl_seconds 不能为负数")
	}
	if c.AuthSessionTTLSeconds < 0 {
		return fmt.Errorf("auth_session_ttl_seconds 不能为负数")
	}
	if c.ListMaxPages != 0 && (c.ListMaxPages < 1 || c.ListMaxPages > 100) {
		return fmt.Errorf("list_max_pages 必须在 1 到 100 之间")
	}
	if c.PreviewMaxBytes < 0 {
		return fmt.Errorf("preview_max_bytes 不能为负数")
	}
	if c.ReadmeMaxBytes < 0 {
		return fmt.Errorf("readme_max_bytes 不能为负数")
	}
	if c.FileCacheTTLSeconds < 0 {
		return fmt.Errorf("file_cache_ttl_seconds 不能为负数")
	}
	if c.DlinkCacheTTLSeconds < 0 {
		return fmt.Errorf("dlink_cache_ttl_seconds 不能为负数")
	}
	c.AccessToken = strings.TrimSpace(c.AccessToken)
	c.SessionSecret = strings.TrimSpace(c.SessionSecret)
	for i := range c.TrustedProxyIPs {
		c.TrustedProxyIPs[i] = strings.TrimSpace(c.TrustedProxyIPs[i])
	}
	seenAllowed := make(map[string]struct{}, len(c.AllowedPaths))
	normalizedAllowed := make([]string, 0, len(c.AllowedPaths))
	for _, raw := range c.AllowedPaths {
		allowed := strings.TrimSpace(raw)
		if allowed == "" || !isValidBaiduPath(allowed) {
			return fmt.Errorf("allowed_paths contains an invalid path: %q", raw)
		}
		if _, exists := seenAllowed[allowed]; exists {
			continue
		}
		seenAllowed[allowed] = struct{}{}
		normalizedAllowed = append(normalizedAllowed, allowed)
	}
	c.AllowedPaths = normalizedAllowed
	return nil
}

func (c *Config) authEnabled() bool {
	return c.AccessToken != ""
}
