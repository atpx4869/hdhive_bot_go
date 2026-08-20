# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS builder
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/worker ./cmd/worker && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app -u 10001 app && mkdir /data && chown app:app /data
COPY --from=builder /out/worker /usr/local/bin/worker
COPY --from=builder /out/migrate /usr/local/bin/migrate
USER app
WORKDIR /data

# 日志配置
ENV LOG_LEVEL=info
# 设置时区用于日志时间戳
ENV TZ=Asia/Shanghai

ENTRYPOINT ["/usr/local/bin/worker"]
