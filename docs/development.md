# 开发与测试

## 项目结构

```text
.
├── main.go              # 入口、HTTP Server、优雅关闭
├── config.go            # 配置加载 / 环境变量覆盖 / 校验
├── server.go            # 路由与安全中间件
├── handlers.go          # HTTP 处理器
├── baidu.go             # 百度青春版 API、签名与直链
├── cache.go             # 文件元数据 / dlink 缓存
├── shortlink.go         # 下载短链存储
├── pathsec.go           # 路径与下载域名校验
├── ratelimit.go         # 按 IP 限流
├── httputil.go          # Cookie / CSRF / 鉴权辅助
├── security_test.go     # 安全相关单测
├── static/              # 前端（go:embed）
├── docs/                # 文档
├── config.example.json  # 配置模板 → 复制为 config.json
├── Dockerfile
├── docker-compose.yml
├── .env.example
└── build.sh
```

## 本地开发

```bash
cp config.example.json config.json
# 填入 baidu_cookie 等

go run . -config config.json
```

Windows：

```powershell
go run . -config config.json
```

修改 `static/` 后需要重新编译/重启（资源通过 `//go:embed static/*` 嵌入）。

## 测试与检查

```bash
go test ./...
go vet ./...
go build -trimpath -ldflags="-s -w" -o cmd/bin/main .
```

或：

```bash
bash build.sh
```

## 常用环境变量（开发）

```bash
export CONFIG_PATH=config.json
export PORT=4172
export BIND_ADDRESS=127.0.0.1
# export BAIDU_COOKIE='...'
# export ACCESS_TOKEN='...'
```

## 杀软误报

部分杀软可能对 Go 网络程序产生 `not-a-virus:NetTool` 启发式误报。开发时可对项目目录与 Go 安装目录加排除。
