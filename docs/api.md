# HTTP API 与下载流程

除特别说明外，启用 `access_token` 后，API 与短链下载均需已登录。

鉴权方式（任选其一）：

- 浏览器：登录后的 HttpOnly 会话 Cookie `jkqp_session`（HMAC 签名、内嵌过期，非 token 原文）
- 编程式客户端：请求头 `Authorization: Bearer <token>` 或 `X-Access-Token: <token>`

> 不接受 query 传 token，避免进访问日志。Cookie 中也不再存放 token 原文。

状态变更类 `POST` 需要 CSRF：

- Cookie：`csrf_token`（前端可读）
- 请求头：`X-CSRF-Token` 与 Cookie 值一致

## 下载流程

1. （可选）登录
2. 前端 `POST /api/download` 申请短链
3. 服务端校验文件后返回 `/d/{token}`
4. 浏览器打开短链；服务端按当前 User-Agent 解析/缓存百度直链并 `302`

真实文件路径不会出现在下载 URL 中。

## 鉴权

### 状态

```http
GET /api/auth/status
```

响应还会返回前端展示配置：`show_readme` 和 `show_readme_overview`，分别控制目录 README 和右侧目录概览。

### 登录

```http
POST /api/login
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"token":"<access_token>"}
```

成功返回 `{"ok":true}` 并下发签名会话 Cookie `jkqp_session`。
令牌错误返回 `401 invalid_token`；同一 IP 连续失败会触发指数退避锁定，返回 `429 login_locked` 并带 `Retry-After`（秒）。

### 退出

```http
POST /api/logout
X-CSRF-Token: <csrf_token>
```

## 文件列表

```http
POST /api/files
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"dir": "/"}
```

说明：

- 兼容 `GET /api/files?dir=/`
- 也可用目录短链定位：`{"token": "<32位hex>"}`（与 `dir` 二选一），见 [目录短链](#目录短链)
- 响应中的 `dlink` 会在返回前剥离
- 响应附带 `resolved_dir` 字段（当前列表对应的真实目录，token 请求时前端据此还原路径）
- 自动翻页合并（每页 100，最多约 15 页，可用 `list_max_pages` 调整）
- 若截断会带 `truncated: true`、`list_pages` 等字段

## 目录短链

浏览目录时前端自动把地址栏换为短链形式（`/?d=令牌`），隐藏真实路径；复制地址栏即得分享链接。也可按需创建：

```http
POST /api/dir-link
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"dir": "/Music/Asmr"}
```

响应：

```json
{
  "token": "9f86d081884c7d659a2feaa0c55ad015",
  "url": "/?d=9f86d081884c7d659a2feaa0c55ad015"
}
```

- 令牌 32 位 hex，有效期默认 7 天（`dir_link_ttl_seconds`），重启后失效
- **隐藏的是路径，不是权限**：打开短链仍需登录，目录权限在使用时按 `allowed_paths` 重新校验
- 文件列表接口接受 `{"token": "..."}`，响应中的 `resolved_dir` 为解析出的真实目录
- 常见错误：`dirlink_failed`、`dirlink_not_found`（令牌过期或服务重启后失效）

### 目录 README

前端会识别当前目录中的 `README.md`、`README.markdown`、`README.txt` 或 `README`，并在文件列表上方安全渲染。接口由前端按需调用：

```http
POST /api/readme
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"path": "/示例目录/README.md"}
```

- 优先级为 `README.md`、`README.markdown`、`README.txt`、`README`
- 接口只接受上述四种 README 文件名；内容上限为 512 KB，超出后不展示
- 支持标题、段落、列表、引用、代码、链接等常用 Markdown；原始 HTML 和脚本不会执行

## 下载短链

### 申请

```http
POST /api/download
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"path": "/示例目录/文件.zip"}
```

响应示例：

```json
{
  "urls": [
    { "url": "/d/0123456789abcdef0123456789abcdef" }
  ]
}
```

### 访问短链

```http
GET /d/{token}
```

- token 为 32 位 hex
- 可配置 TTL 与最大使用次数
- 启用鉴权时，访问短链同样需要已登录

### 图片预览

图片文件由前端点击“预览”后请求，服务端读取图片并以 `inline` 响应，供站内灯箱显示：

```http
POST /api/preview
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"path": "/图片目录/example.jpg"}
```

- 仅允许图片 MIME 类型；服务端同时结合文件内容和扩展名识别类型
- 单张图片默认最大 16 MB（`preview_max_bytes` 可调）
- 预览内容会经过 VPS，因此会产生 VPS 出站流量；普通下载仍使用百度直链重定向
- 常见错误：`preview_link_failed`、`preview_fetch_failed`、`preview_not_image`、`preview_too_large`

### 文本预览

文本/代码文件在灯箱中查看：

```http
POST /api/text
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"path": "/代码目录/main.go"}
```

- 仅接受扩展名白名单：`txt` `md` `markdown` `json` `xml` `yaml` `yml` `csv` `log` `ini` `conf` `sh` `bat` `ps1` `py` `js` `ts` `go` `c` `h` `cpp` `hpp` `java` `rs` `sql` `toml` `html` `css`
- 内容上限 512 KB；超出后按 UTF-8 字符边界截断并返回 `truncated: true`
- 非 UTF-8（二进制）内容返回 `415 text_not_text`
- 响应示例：

```json
{
  "found": true,
  "name": "main.go",
  "content": "package main\n...",
  "truncated": false
}
```

- 常见错误：`text_not_allowed`、`text_not_text`、`text_link_failed`、`text_fetch_failed`

### 音视频播放

`<video>` / `<audio>` 直接复用下载短链：前端先 `POST /api/download` 拿到 `/d/{token}`，再把短链作为媒体 `src`。服务端 302 到百度直链，浏览器自动透传 `Range` 头，支持拖动进度条。

- 媒体流量**不经过 VPS 中转**（浏览器直连百度直链；CSP `media-src` 已放行相应域名）
- 注意：若配置了 `short_link_max_uses`，拖动进度条产生的多次请求可能重复消耗短链使用次数；需要媒体播放时建议保持默认（不限次数）

## 健康检查

| 路径 | 鉴权 | 说明 |
| --- | --- | --- |
| `GET /healthz` | 否 | 进程存活（同时支持 `HEAD`） |
| `GET /readyz` | 是（若启用 access_token） | 探测百度 Session |

## 错误响应格式

```json
{
  "error": {
    "code": "unauthorized",
    "message": "需要访问令牌"
  }
}
```

常见 code：`unauthorized`、`csrf_invalid`、`invalid_path`、`path_not_allowed`、`rate_limited`、`baidu_list_failed`、`dlink_failed`、`shortlink_not_found`、`dirlink_not_found`、`readme_not_found`、`readme_link_failed`、`readme_too_large`、`readme_not_text`、`preview_not_image`、`preview_too_large`、`text_not_allowed`、`text_not_text`、`text_link_failed`、`text_fetch_failed` 等。
