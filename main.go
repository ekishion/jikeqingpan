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

	srv := newServer(cfg, embeddedStatic)

	addr := net.JoinHostPort(cfg.BindAddress, strconv.Itoa(cfg.Port))
	server := &http.Server{
		Addr:              addr,
		Handler:           srv.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// 列表翻页与下载冷启动会串行请求百度；需覆盖最坏路径（约 15 页 × 客户端超时）。
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		authMode := "关闭（仅适合本机/受信网络）"
		if cfg.authEnabled() {
			authMode = "已启用 access_token"
		}
		log.Printf("即刻轻盘启动: http://%s  鉴权: %s", addr, authMode)
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
}
