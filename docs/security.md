# 安全建议

## 部署红线

- 默认绑定 `127.0.0.1`；公网入口走 HTTPS 反代
- VPS/公网务必设置足够长的 `access_token`
- `baidu_cookie` 仅服务端持有，不返回给浏览器
- **不要**把 `config.json` / `.env` 提交到仓库或打进镜像
- 若历史中曾泄露 Cookie/Token，立即轮换

## 应用层能力

- 登录态：**HMAC 签名会话令牌**写入 HttpOnly Cookie（`jkqp_session`）；Cookie 不含 `access_token` 原文，泄露也拿不到管理员令牌
  - 会话内嵌过期时间并参与签名，客户端无法篡改延长；默认 7 天（`auth_session_ttl_seconds`）
  - 轮换 `session_secret`（或修改 `access_token`）即服务端强制下线全部会话
  - 编程式客户端仍可用 `Authorization: Bearer` / `X-Access-Token` 请求头携带原始令牌
- 登录防爆破：`/api/login` 按 IP 指数退避锁定（前 5 次容错，之后失败翻倍锁定，封顶 5 分钟，锁定期返回 `429` + `Retry-After`）
- 令牌比较：对 SHA-256 摘要做常量时间比较，不泄露令牌长度侧信道
- CSRF：双提交 Cookie，保护 POST
- 下载短链：32 位随机 hex，可配 TTL / 次数上限；直链解析失败不消耗使用次数
- 路径校验：拒绝 `..`、异常字符、规范化后语义变化的路径
- 直链域名白名单：仅允许百度相关 HTTPS 域名
- dlink 缓存绑定 User-Agent，降低跨浏览器复用风险
- 图片预览单独走受限接口，仅允许图片 MIME 类型且限制为 16 MB（可配 `preview_max_bytes`），流式转发不整块驻留内存；预览数据经过 VPS 并以 `inline` 返回
- 文本预览（`/api/text`）仅接受扩展名白名单（与前端同步），内容以 JSON 字符串回传由前端安全渲染，非 UTF-8 内容直接拒绝
- 媒体播放（`<video>/<audio>`）复用下载短链 302 到百度直链，CSP `media-src` 与直链白名单保持一致，媒体流量不经 VPS 中转
- 按 IP 限流；`X-Forwarded-For` 只在来源命中 `trusted_proxy_ips` 白名单时才被采信（从右向左跳过可信代理取第一个不可信 IP）
- 元数据缓存 / 短链 / 限流客户端均有上限（超限采样淘汰）
- 审计日志超 10 MB 自动轮转为 `*.old`，磁盘占用有界
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

- 未配置 `trusted_proxy_ips` 时，反代后所有用户会被视为同一来源，限流/锁定变成共享桶（见 [deployment.md](deployment.md)）
- 单进程内存短链/缓存：重启丢失；多实例不共享
- 依赖百度青春版接口可用性与 Cookie 有效性
