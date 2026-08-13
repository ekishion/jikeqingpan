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
- 响应中的 `dlink` 会在返回前剥离
- 自动翻页合并（每页 100，最多约 15 页）
- 若截断会带 `truncated: true`、`list_pages` 等字段

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

## 健康检查

| 路径 | 鉴权 | 说明 |
| --- | --- | --- |
| `GET /healthz` | 否 | 进程存活 |
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

常见 code：`unauthorized`、`csrf_invalid`、`invalid_path`、`path_not_allowed`、`rate_limited`、`baidu_list_failed`、`dlink_failed`、`shortlink_not_found` 等。
