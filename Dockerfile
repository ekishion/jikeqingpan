# syntax=docker/dockerfile:1

# 使用 --platform=$BUILDPLATFORM 让编译阶段始终运行在宿主机原生 CPU 架构上（极速构建，避免 QEMU 软仿真开销）
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/

# BuildKit 自动注入目标系统的 OS 和架构（如 linux/amd64 或 linux/arm64）
# 纯 Go（CGO_ENABLED=0）原生支持极速交叉编译
ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/jikeqingpan ./cmd/jikeqingpan

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
