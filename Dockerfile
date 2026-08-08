# 用 modernc.org/sqlite（纯 Go）所以不需要 CGO —— 镜像能做到静态 + distroless
FROM golang:1.26-alpine AS build

WORKDIR /src

# 先只拷依赖清单，让 go mod download 能命中缓存层
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0：静态链接，运行镜像里不用带 libc
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/bus-pooling ./cmd/bus-pooling

# ── 运行镜像 ────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/bus-pooling /app/bus-pooling
COPY config.example.yaml /app/config.example.yaml

# SQLite 数据要挂 volume —— 容器重建不能丢乘客数据和台账
VOLUME ["/app/data"]

EXPOSE 8080

# distroless nonroot 的 uid
USER 65532:65532

ENTRYPOINT ["/app/bus-pooling"]
CMD ["serve"]
