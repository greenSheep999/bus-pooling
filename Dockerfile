# bus-pooling · 多阶段 Dockerfile
#
# 三阶段构建：
#   1. web-build · Vite 打前端 → /w/dist（静态资源 · 单独 nginx 或直接 caddy serve）
#   2. go-build  · 编 Go 二进制（modernc.org/sqlite 纯 Go · 无 CGO · 静态链接）
#   3. runtime   · alpine + ca-certs · 跑 /app/bus-pooling + serve web/dist
#
# 关键：后端 Go 二进制**embed 前端 dist**（见 internal/web/embed.go）· 单容器一体化。

# ────────────────────────── web-build ──────────────────────────
FROM node:20-alpine AS web-build
WORKDIR /w
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ────────────────────────── go-build ───────────────────────────
FROM golang:1.23-alpine AS go-build
WORKDIR /src

# 依赖缓存层
COPY go.mod go.sum ./
RUN go mod download

# 源码 + 迁移 sql
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# 把前端 dist 拷进 internal/web/dist · 让 Go embed 打进二进制
COPY --from=web-build /w/dist ./internal/web/dist

# 静态链接 · 无 CGO · 版本注入
ARG BUILD_SHA=dev
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.buildSHA=${BUILD_SHA} -X main.buildTime=${BUILD_TIME}" \
    -o /out/bus-pooling ./cmd/bus-pooling

# ────────────────────────── runtime ────────────────────────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -g 65532 app \
    && adduser -D -u 65532 -G app -s /sbin/nologin app

WORKDIR /app
COPY --from=go-build /out/bus-pooling /app/bus-pooling
COPY config.example.yaml /app/config.example.yaml

RUN mkdir -p /app/data && chown -R app:app /app
USER app

EXPOSE 8080
ENV BP_ADDR=":8080" \
    BP_DB_PATH="/app/data/bus-pooling.db" \
    BP_CONFIG="/app/config.yaml"

# health probe · 后端 /healthz 已实现（不是 /api/health）
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/bus-pooling"]
CMD ["serve"]
