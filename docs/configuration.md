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
| `force_secure_cookie` | `FORCE_SECURE_COOKIE` | `false` | 强制 Cookie `Secure`（HTTPS 反代时建议 true） |
| `rate_limit_per_second` | `RATE_LIMIT_PER_SECOND` | `10` | 每 IP 每秒限流 |
| `baidu_app_id` | `BAIDU_APP_ID` | `250528` | 百度 App ID |
| `short_link_ttl_seconds` | `SHORT_LINK_TTL_SECONDS` | `3600` | 短链有效期（秒） |
| `short_link_max_uses` | `SHORT_LINK_MAX_USES` | `0` | 短链最大使用次数；`0` 不限制 |
| `session_ttl_seconds` | `SESSION_TTL_SECONDS` | `3600` | 百度 uk/sk 缓存（秒） |
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