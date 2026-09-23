# 即刻轻盘

基于**百度网盘青春版**的临时中转网盘。

- **后端**：Go 标准库为主；可选启用状态持久化时使用纯 Go SQLite 驱动（modernc.org/sqlite，无 CGO）
- **前端**：极简响应式 HTML / CSS / JS（`go:embed` 打包进单二进制，零外部 CDN 依赖）
- **安全**：Cookie 仅服务端托管，HMAC 会话签名防篡改，支持 IP 爆破退避锁定与 CSRF 防护
- **权限**：支持 `access_token` 全局访问保护、共享目录白名单与透明目录短链（`/?d=令牌`）

## 功能特性

- **目录浏览**：支持文件夹层级深度浏览、自动多页合并拉取、按名称/大小/时间排序及前进后退
- **目录短链**：地址栏自动呈现无真实路径的目录短链（`/?d=令牌`），复制即分享
- **安全下载**：生成不透明单次/多次/限时下载短链（`/d/{token}`），直链只由服务端按 UA 签名解析并 302 重定向
- **目录说明**：自动识别目录内 README 文件并在顶部优雅渲染 Markdown
- **多媒体在线预览**：
  - 图片灯箱：支持点击放大、缩放位置自适应、上一张/下一张滑动切换
  - 文本/代码：内联安全渲染与行号高亮
  - 视频/音频：自绘流媒体播放器，支持进度快进快退、静音、全屏（兼容移动端/iOS Safari）
- **主题与体验**：深色模式自动跟随系统或手动切换，`/` 快捷键聚焦筛选
- **健康检查**：提供 `/healthz`（存活探针）与 `/readyz`（百度会话就绪探针）

## 截图展示

![即刻轻盘界面截图](./docs/images/index.png)

## 快速开始

### 1. 准备配置

```bash
cp config.example.json config.json
```

编辑 `config.json`：至少填写 `baidu_cookie`；公网/VPS 务必设置 `access_token`。

```bash
# 生成高熵访问令牌示例
openssl rand -hex 24
```

> Docker 容器部署时通过环境变量 `BIND_ADDRESS=0.0.0.0` 覆盖监听地址即可。

完整配置项说明见 [docs/configuration.md](docs/configuration.md)。

### 2. 本地运行与构建

```bash
# 本地开发运行
go run ./cmd/jikeqingpan -config config.json

# 一键编译发布二进制
bash build.sh
./cmd/bin/main -config config.json
```

Windows PowerShell：

```powershell
# 本地开发运行
go run ./cmd/jikeqingpan -config config.json

# 编译为 Windows 可执行文件
go build -o cmd/bin/main.exe ./cmd/jikeqingpan
.\cmd\bin\main.exe -config config.json
```

浏览器访问：`http://127.0.0.1:4172`

### 3. Docker 部署（推荐）

```bash
cp config.example.json config.json   # 填好 cookie / token
docker compose up -d --build
curl -sS http://127.0.0.1:4172/healthz
```

详细部署指南与反向代理配置见 [docs/deployment.md](docs/deployment.md)。

## 文档索引

| 文档 | 内容说明 |
| --- | --- |
| [docs/configuration.md](docs/configuration.md) | 配置字段、环境变量与默认值 |
| [docs/deployment.md](docs/deployment.md) | Docker Compose / VPS / Nginx / Caddy 反向代理 |
| [docs/api.md](docs/api.md) | HTTP API 规范与下载/短链流程 |
| [docs/security.md](docs/security.md) | 访问控制、CSRF、Cookie 安全与审计日志 |
| [docs/development.md](docs/development.md) | 标准 Go 项目结构、本地调试与测试 |

## 参考与致谢

- 百度青春版签名算法参考了 [AList](https://github.com/AlistGo/alist) 的 BaiduYouth 驱动实现思路。

## 开源协议

- 仓库地址：<https://github.com/ekishion/jikeqingpan>
