# 即刻轻盘

基于**百度网盘青春版**的临时中转网盘。

- 后端：Go
- 前端：静态 HTML / JS
- Cookie 仅保存在服务端，不暴露给浏览器

## 功能

- 浏览网盘目录（支持文件夹）
- 申请不透明短链后下载
- 复制短链分享（有效期内可重复访问）
- 服务端限流、CSRF 保护、直链域名白名单

## 截图

![即刻轻盘界面截图](https://img11.360buyimg.com/ddimg/jfs/t1/443186/21/10502/30047/6a1fa645F590f15a3/001536a2fd6f69b0.jpg)

## 快速开始

### 1. 准备配置

在项目根目录自行创建 `config.json`（**不要提交到 Git**）：

```json
{
  "port": 8080,
  "bind_address": "127.0.0.1",
  "baidu_cookie": "你的百度网盘青春版 Cookie",
  "rate_limit_per_second": 10,
  "baidu_app_id": "250528"
}
```

### 2. 构建

```bash
go build -o cmd/bin/main main.go
```

或使用脚本：

```bash
bash build.sh
```

### 3. 运行

```bash
./cmd/bin/main
```

浏览器访问：

```text
http://127.0.0.1:8080
```

> 默认只监听本机。若需远程访问，请自行配置反向代理、认证与 HTTPS，并明确设置 `bind_address`。

## 配置说明

| 字段 | 说明 |
| --- | --- |
| `port` | 监听端口，默认 `8080` |
| `bind_address` | 绑定地址，默认 `127.0.0.1` |
| `baidu_cookie` | 百度网盘青春版 Cookie（敏感信息） |
| `rate_limit_per_second` | API 限流（每 IP 每秒） |
| `baidu_app_id` | 百度 App ID，默认 `250528` |

## 下载流程

1. 前端 `POST /api/download`（需 CSRF）申请短链
2. 服务端返回不透明路径：`/d/{token}`
3. 浏览器访问短链，服务端解析百度直链并 `302` 跳转

真实文件路径不会出现在下载 URL 中。

## HTTP API

### 获取文件列表

```http
POST /api/files
Content-Type: application/json
X-CSRF-Token: <csrf_token>
```

```json
{
  "dir": "/"
}
```

说明：

- 也兼容 `GET /api/files?dir=/`
- 响应中的 `dlink` 会在返回前剥离，仅缓存于服务端
- 目录列表会自动翻页合并

### 申请下载短链

```http
POST /api/download
Content-Type: application/json
X-CSRF-Token: <csrf_token>
```

```json
{
  "path": "/示例目录/文件.zip"
}
```

响应示例：

```json
{
  "urls": [
    {
      "url": "/d/0123456789abcdef0123456789abcdef"
    }
  ]
}
```

> `GET /api/download` 已禁用，避免绕过 CSRF 并在查询串中暴露路径。

### 通过短链下载

```http
GET /d/{token}
```

服务端校验 token 后重定向到百度直链。短链默认有效期 1 小时。

## 安全说明

- 默认绑定 `127.0.0.1`，降低误暴露风险
- `baidu_cookie` 仅服务端使用
- 状态变更接口需要 CSRF（Cookie + `X-CSRF-Token` 双提交）
- 下载短链为 32 位随机 hex，不包含真实路径
- 百度直链仅允许 `https` 与百度相关域名白名单
- 限流基于客户端 IP（不信任未配置的 `X-Forwarded-For`）

**请勿将 `config.json` 或 Cookie 提交到仓库。** 若历史中曾泄露 Cookie，请立即在百度侧轮换。

## 参考

定位下载签名逻辑参考了 [AList](https://github.com/AlistGo/alist) 的 BaiduYouth 驱动实现思路。

## 相关链接

- 开源仓库：<https://github.com/malaohu/jikeqingpan>
- 作者博客：<https://51.ruyo.net>
