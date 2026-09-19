// 临时盘 / 即刻轻盘 - Go 后端
// 代理百度网盘青春版 API，将 Cookie 保存在服务端，不暴露给前端。
// VPS 少数用户场景：配置 access_token 启用访问控制。
package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// version 由构建时注入（-ldflags "-X main.version=..."），默认 dev。
var version = "dev"

//go:embed static/*
var embeddedStatic embed.FS

func main() {
	defaultConfig := "config.json"
	if v := strings.TrimSpace(os.Getenv("CONFIG_PATH")); v != "" {
		defaultConfig = v
	}
	configPath := flag.String("config", defaultConfig, "配置文件路径（也可用环境变量 CONFIG_PATH）")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	warnRiskyConfig(cfg)

	srv := newServer(cfg, embeddedStatic)

	addr := net.JoinHostPort(cfg.BindAddress, strconv.Itoa(cfg.Port))
	server := &http.Server{
		Addr:              addr,
		Handler:           srv.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// 列表翻页与下载冷启动会串行请求百度（每页按 16s 估算最坏耗时），
		// 预览为流式转发；WriteTimeout 随 list_max_pages 推导并保底下限。
		WriteTimeout: writeTimeoutFor(cfg),
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		authMode := "关闭（仅适合本机/受信网络）"
		if cfg.authEnabled() {
			authMode = "已启用 access_token"
		}
		log.Printf("即刻轻盘启动: http://%s  版本: %s  鉴权: %s", addr, version, authMode)
		if cfg.BindAddress == "0.0.0.0" || cfg.BindAddress == "::" {
			if !cfg.authEnabled() {
				log.Printf("[WARN] 正在监听所有网卡且未设置 access_token，存在未授权访问风险")
			}
		}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务异常退出: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Printf("正在优雅关闭…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("关闭服务失败: %v", err)
	}
	srv.closePersist()
	srv.closeAudit()
}

// writeTimeoutFor 按 list_max_pages 推导 WriteTimeout：最坏路径是
// maxPages 次串行百度请求（每次 15s 客户端超时 + 1s 余量），外加
// 60s 预览/README 流式转发余量；下限 120s 兜底常规场景。
func writeTimeoutFor(cfg *Config) time.Duration {
	derived := 60*time.Second + time.Duration(cfg.listMaxPages())*16*time.Second
	if derived < 120*time.Second {
		return 120 * time.Second
	}
	return derived
}

// warnRiskyConfig 启动时提示有风险但不阻断的配置组合。
func warnRiskyConfig(cfg *Config) {
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
