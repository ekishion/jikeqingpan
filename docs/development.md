# 开发与测试

本项目采用标准 Go 项目工程布局（Standard Go Project Layout），遵循清晰的模块边界与分层架构。

## 项目结构

```text
.
├── cmd/
│   └── jikeqingpan/          # 应用可执行文件入口
│       └── main.go           # CLI 参数解析、配置装载、优雅停机与服务启动
├── internal/
│   └── app/                  # 核心业务逻辑实现（Go 私有包，禁止外部导入）
│       ├── baidu.go          # 百度青春版 API 代理、直链解析与参数签名
│       ├── cache.go          # 目录与文件元数据多级缓存（支持内存与落盘持久化）
│       ├── cache_test.go     # 缓存读写、TTL 与主动失效测试
│       ├── config.go         # 服务配置结构体、环境变量覆盖与校验
│       ├── dirlink.go        # 目录短链（?d=令牌）管理与过期清理
│       ├── evict.go          # 采样淘汰算法
│       ├── handlers.go       # HTTP API 路由处理器
│       ├── handlers_test.go  # 接口鉴权、目录缓存与响应头测试
│       ├── httputil.go       # HTTP / Cookie / CSRF / IP 工具库
│       ├── loginguard.go     # 登录失败 IP 锁定与指数退避防护
│       ├── pathsec.go        # 路径遍历防护与百度直链域名白名单
│       ├── persist.go        # SQLite / JSON 状态持久化后端驱动
│       ├── persist_test.go   # SQLite / JSON 状态落盘与恢复测试
│       ├── ratelimit.go      # 基于 IP 的令牌桶限流器
│       ├── server.go         # HTTP Server 封装、安全中间件与路由注册
│       ├── session.go        # HMAC-SHA256 会话签名与无状态校验
│       └── shortlink.go      # 下载短链存储与解析
├── web/                      # 前端静态资源与 Go 嵌入包
│   ├── embed.go              # //go:embed static/* 导出前端静态资源 FS
│   └── static/               # 前端静态文件（HTML / CSS / JS）
│       ├── index.html        # 单页应用 HTML 骨架
│       ├── app.css           # 纯白极简 UI / 深色模式适配样式表
│       ├── app.js            # 前端核心交互逻辑（列表、排序、搜索、短链）
│       ├── preview.js        # 多媒体灯箱预览（图片/代码/音视频播放与移动端全屏）
│       ├── icons.js          # 内联 Lucide SVG 图标模块
│       └── markdown.js       # README 纯 DOM 安全 Markdown 渲染器
├── docs/                     # 完整开发、配置与部署文档
├── .github/
│   └── workflows/
│       ├── ci.yml            # 代码规范、前端语法检查与跨平台编译矩阵
│       └── docker.yml        # 多架构 Docker 镜像自动化发布
├── Dockerfile                # 多阶段极简 Alpine 无根镜像构建
├── docker-compose.yml        # Docker Compose 服务编排模板
├── build.sh                  # 自动化编译发布脚本
├── config.example.json       # 服务配置示例文件
├── .env.example              # 环境变量配置示例
├── go.mod                    # Go 依赖与版本定义
└── go.sum
```

## 本地开发调试

### 1. 启动服务

```bash
cp config.example.json config.json
# 编辑 config.json 填入 baidu_cookie 等必要参数

go run ./cmd/jikeqingpan -config config.json
```

Windows PowerShell：

```powershell
go run ./cmd/jikeqingpan -config config.json
```

修改 `web/static/` 下的前端资源后，重新运行 `go run` 即可自动重新嵌入生效。

## 自动化测试与检查

```bash
# 代码格式检查
gofmt -l .

# 静态代码分析
go vet ./...

# 前端 JS 语法校验
for f in web/static/*.js; do node --check "$f"; done

# 编译验证
go build -trimpath -ldflags="-s -w" -o cmd/bin/main ./cmd/jikeqingpan

# 运行单元测试
go test ./...
```

或直接执行自动化脚本：

```bash
bash build.sh
```

## 常用开发环境变量

```bash
export CONFIG_PATH=config.json
export PORT=4172
export BIND_ADDRESS=127.0.0.1
# export BAIDU_COOKIE='...'
# export ACCESS_TOKEN='...'
```

## 杀毒软件提示

部分杀毒软件可能对 Go 编写的网络工具产生启发式误报。在本地开发时，建议将项目工作区与 Go SDK 目录加入排除项列表。
