# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.25-alpine AS builder
WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/

# 纯标准库为主（SQLite 持久化为纯 Go 驱动）；不写死 GOARCH，便于 buildx 多架构（amd64/arm64）
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/jikeqingpan ./cmd/jikeqingpan

# ---- runtime ----
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
  && adduser -D -H -u 10001 appuser

WORKDIR /app

COPY --from=builder /out/jikeqingpan /usr/local/bin/jikeqingpan

# 配置通过卷挂载 /data/config.json，或用环境变量 BAIDU_COOKIE / ACCESS_TOKEN 注入。
# 密钥不要打进镜像。
ENV CONFIG_PATH=/data/config.json \
    BIND_ADDRESS=0.0.0.0 \
    PORT=4172 \
    TZ=Asia/Shanghai

EXPOSE 4172

USER appuser

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT:-4172}/healthz" >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/jikeqingpan"]
CMD ["-config", "/data/config.json"]
