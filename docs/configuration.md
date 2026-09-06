# 配置说明

本地、VPS、Docker **只维护一份配置**：`config.json`。

```bash
cp config.example.json config.json
```

- 环境变量可覆盖同名字段（优先级更高）
- 配置文件可不存在：纯环境变量模式也能启动（适合编排注入密钥）

## 最小必填

| 项 | 说明 |
| --- | --- |
| `baidu_cookie` | 百度网盘青春版 Cookie |
| `access_token` | 公网/VPS **强烈建议必填**；非空即启用登录鉴权 |

生成令牌：

```bash
openssl rand -hex 24
```

## 字段与环境变量

| 字段 | 环境变量 | 默认 | 说明 |
| --- | --- | --- | --- |
| `port` | `PORT` | `4172` | 监听端口 |
| `bind_address` | `BIND_ADDRESS` | `127.0.0.1` | 绑定地址；容器内请用 `0.0.0.0` |
| `baidu_cookie` | `BAIDU_COOKIE` | （必填） | 百度 Cookie（敏感） |
| `access_token` | `ACCESS_TOKEN` | 空 | 访问令牌；非空启用鉴权（敏感） |
| `session_secret` | `SESSION_SECRET` | 空 | 会话签名密钥；留空则由 `access_token` 派生。改此值可一键强制所有人重新登录（敏感） |
| `auth_session_ttl_seconds` | `AUTH_SESSION_TTL_SECONDS` | `604800` | 登录会话有效期（秒），默认 7 天 |
| `force_secure_cookie` | `FORCE_SECURE_COOKIE` | `false` | 强制 Cookie `Secure`（HTTPS 反代时建议 true） |
| `rate_limit_per_second` | `RATE_LIMIT_PER_SECOND` | `10` | 每 IP 每秒限流 |
| `baidu_app_id` | `BAIDU_APP_ID` | `250528` | 百度 App ID |
| `short_link_ttl_seconds` | `SHORT_LINK_TTL_SECONDS` | `3600` | 短链有效期（秒） |
| `short_link_max_uses` | `SHORT_LINK_MAX_USES` | `0` | 短链最大使用次数；`0` 不限制 |
| `session_ttl_seconds` | `SESSION_TTL_SECONDS` | `3600` | 百度 uk/sk 缓存（秒） |
| `trusted_proxy_ips` | `TRUSTED_PROXY_IPS` | 空 | 允许读取 `X-Forwarded-For` 的反代 IP 或 CIDR；仅填写实际反代来源 |
| `audit_log_path` | `AUDIT_LOG_PATH` | 空 | JSONL 下载审计日志路径；为空时关闭持久化，父目录不存在时会自动创建 |
| `allowed_paths` | `ALLOWED_PATHS` | 空 | 共享目录白名单；配置后仅允许这些目录及其子目录访问，多个环境变量路径用英文逗号分隔 |
| `show_readme` | `SHOW_README` | `true` | 是否在目录顶部展示网盘中的 README |
| `show_readme_overview` | `SHOW_README_OVERVIEW` | `true` | 是否在 README 右侧展示目录概览 |
| `list_max_pages` | `LIST_MAX_PAGES` | `15` | 目录列表自动翻页上限（每页 100 项，15 页即 1500 项） |
| `preview_max_bytes` | `PREVIEW_MAX_BYTES` | `16777216` | 图片预览大小上限（字节），默认 16 MB |
| `readme_max_bytes` | `README_MAX_BYTES` | `524288` | README 内容大小上限（字节），默认 512 KB |
| `file_cache_ttl_seconds` | `FILE_CACHE_TTL_SECONDS` | `900` | 文件元数据缓存有效期（秒） |
| `dlink_cache_ttl_seconds` | `DLINK_CACHE_TTL_SECONDS` | `300` | 下载直链缓存有效期（秒） |
| — | `CONFIG_PATH` | `config.json` / 容器内 `/data/config.json` | 配置文件路径 |
| — | `TZ` | — | 时区（Docker 默认 `Asia/Shanghai`） |

布尔环境变量支持：`true` / `1` / `yes`（大小写不敏感）。

## 示例

### 本机开发

```json
{
  "port": 4172,
  "bind_address": "127.0.0.1",
  "baidu_cookie": "你的 Cookie",
  "access_token": "",
  "force_secure_cookie": false,
  "rate_limit_per_second": 10,
  "baidu_app_id": "250528",
  "short_link_ttl_seconds": 3600,
  "short_link_max_uses": 0,
  "session_ttl_seconds": 3600
}
```

### VPS（经 HTTPS 反代）

同一份 `config.json`，建议：

```json
{
  "port": 4172,
  "bind_address": "127.0.0.1",
  "baidu_cookie": "你的 Cookie",
  "access_token": "openssl rand -hex 24 的结果",
  "force_secure_cookie": true
}
```

### Docker

- 仍用 `config.json` 挂载进容器；**不必**单独维护 docker 专用配置文件
- `docker-compose.yml` 会用 `BIND_ADDRESS=0.0.0.0` 覆盖监听地址
- 也可用 `.env` / 环境变量注入 `BAIDU_COOKIE`、`ACCESS_TOKEN`（见 [deployment.md](deployment.md)）
