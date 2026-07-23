# 部署指南

## 推荐拓扑（VPS 少数人使用）

```text
Internet → Nginx/Caddy (HTTPS) → 127.0.0.1:4172 → jikeqingpan
```

要点：

1. 服务只监听本机 `127.0.0.1`（或 Docker 端口只绑 `127.0.0.1`）
2. 反代终结 TLS
3. 开启 `force_secure_cookie` / `FORCE_SECURE_COOKIE=true`
4. 设置足够长的 `access_token`
5. 不要把 `0.0.0.0` 直接暴露到公网且不设 token

配置只使用 `config.example.json` → `config.json`。详见 [configuration.md](configuration.md)。

## 二进制部署

```bash
cp config.example.json config.json
# 编辑 config.json

go build -trimpath -ldflags="-s -w" -o cmd/bin/main .
# 或
bash build.sh

./cmd/bin/main -config config.json
```

可用 systemd / supervisor 托管进程；进程前建议仅本机监听。

## Docker Compose（推荐）

```bash
cp config.example.json config.json
# 填入 baidu_cookie、access_token

docker compose up -d --build
docker compose ps
docker compose logs -f
curl -sS http://127.0.0.1:4172/healthz
```

默认映射：`127.0.0.1:4172 -> 容器 4172`。

镜像特点：

- 多阶段构建，Alpine 运行时
- 非 root（`appuser`）
- `read_only` + `cap_drop: ALL` + `no-new-privileges`
- 密钥通过卷挂载或环境变量注入，**不打进镜像**

### 使用环境变量

```bash
cp .env.example .env
# 编辑 BAIDU_COOKIE / ACCESS_TOKEN
```

在 `docker-compose.yml` 中取消 `env_file` 注释；若完全用环境变量，可注释 `config.json` 卷挂载。

HTTPS 反代时建议：

```yaml
environment:
  FORCE_SECURE_COOKIE: "true"
```

### 直接 docker run

```bash
docker build -t jikeqingpan:latest .

docker run -d --name jikeqingpan \
  --restart unless-stopped \
  -p 127.0.0.1:4172:4172 \
  -e BIND_ADDRESS=0.0.0.0 \
  -e PORT=4172 \
  -e TZ=Asia/Shanghai \
  -v "$PWD/config.json:/data/config.json:ro" \
  jikeqingpan:latest
```

纯环境变量（配置文件可不存在）：

```bash
docker run -d --name jikeqingpan \
  --restart unless-stopped \
  -p 127.0.0.1:4172:4172 \
  -e BIND_ADDRESS=0.0.0.0 \
  -e PORT=4172 \
  -e BAIDU_COOKIE='你的Cookie' \
  -e ACCESS_TOKEN='你的长随机令牌' \
  -e FORCE_SECURE_COOKIE=true \
  jikeqingpan:latest
```

### 更新

```bash
git pull
docker compose up -d --build
```

修改 `static/` 后需重新 build（资源已 embed）。

## Nginx 反代示例

```nginx
server {
    listen 443 ssl http2;
    server_name pan.example.com;

    # ssl_certificate / ssl_certificate_key ...

    location / {
        proxy_pass http://127.0.0.1:4172;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 120s;
    }
}
```

## 限流与反代注意

当前限流基于直连 `RemoteAddr`。若反代后所有人变成 `127.0.0.1`，限流会变成“全站共享桶”。

建议：

- 服务只绑本机，外网入口放在反代
- 主要限流策略放到反代层
- 或后续增加可信代理 IP 解析（当前未实现）

## 健康检查

- `GET /healthz`：进程存活（公开）
- `GET /readyz`：百度 Session 是否可用（需鉴权）
