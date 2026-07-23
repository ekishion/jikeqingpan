package main

import (
	"encoding/json"
	"fmt"
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

	// ForceSecureCookie 在 TLS 终结于反向代理时强制 Secure Cookie。
	ForceSecureCookie bool `json:"force_secure_cookie"`

	// ShortLinkTTLSeconds 短链有效期，默认 3600。
	ShortLinkTTLSeconds int `json:"short_link_ttl_seconds"`
	// ShortLinkMaxUses 短链最大使用次数；0 表示不限制。
	ShortLinkMaxUses int `json:"short_link_max_uses"`

	// SessionTTLSeconds 百度 uk/sk 会话缓存有效期，默认 3600。
	SessionTTLSeconds int `json:"session_ttl_seconds"`
}

func (c *Config) shortLinkTTL() time.Duration {
	if c.ShortLinkTTLSeconds <= 0 {
		return time.Hour
	}
	return time.Duration(c.ShortLinkTTLSeconds) * time.Second
}

func (c *Config) sessionTTL() time.Duration {
	if c.SessionTTLSeconds <= 0 {
		return time.Hour
	}
	return time.Duration(c.SessionTTLSeconds) * time.Second
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
	if n, ok := envInt("SESSION_TTL_SECONDS"); ok {
		c.SessionTTLSeconds = n
	}
}

func envInt(key string) (int, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
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
	if c.ShortLinkMaxUses < 0 {
		return fmt.Errorf("short_link_max_uses 不能为负数")
	}
	if c.SessionTTLSeconds < 0 {
		return fmt.Errorf("session_ttl_seconds 不能为负数")
	}
	c.AccessToken = strings.TrimSpace(c.AccessToken)
	return nil
}

func (c *Config) authEnabled() bool {
	return c.AccessToken != ""
}
