# 安全建议

## 部署红线

- 默认绑定 `127.0.0.1`；公网入口走 HTTPS 反代
- VPS/公网务必设置足够长的 `access_token`
- `baidu_cookie` 仅服务端持有，不返回给浏览器
- **不要**把 `config.json` / `.env` 提交到仓库或打进镜像
- 若历史中曾泄露 Cookie/Token，立即轮换

## 应用层能力

- 登录态：HttpOnly Cookie + 可选 Bearer / `X-Access-Token`
- CSRF：双提交 Cookie，保护 POST
- 下载短链：32 位随机 hex，可配 TTL / 次数上限
- 路径校验：拒绝 `..`、异常字符、规范化后语义变化的路径
- 直链域名白名单：仅允许百度相关 HTTPS 域名
- dlink 缓存绑定 User-Agent，降低跨浏览器复用风险
- 按 IP 限流；不信任未配置的 `X-Forwarded-For`
- 元数据缓存 / 短链 / 限流客户端均有上限
- 安全响应头：`X-Content-Type-Options`、`X-Frame-Options`、CSP、`Referrer-Policy` 等
- HTTP Server 读写超时与优雅关闭

## Cookie 与 HTTPS

TLS 在反代终结时，请开启：

```json
"force_secure_cookie": true
```

或：

```bash
FORCE_SECURE_COOKIE=true
```

否则浏览器可能在 HTTPS 页面拒绝写入 Secure 相关策略不一致的 Cookie。

## 短链分享注意

开启鉴权后，复制的 `/d/{token}` 链接**仍然需要接收方已登录**（共享同一 `access_token`）。  
短链隐藏的是文件路径，不是访问控制本身。

## 已知局限

- 限流基于 `RemoteAddr`，反代后可能变成共享桶（见 [deployment.md](deployment.md)）
- 单进程内存短链/缓存：重启丢失；多实例不共享
- 依赖百度青春版接口可用性与 Cookie 有效性
