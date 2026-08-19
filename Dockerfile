# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/check ./cmd/check
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app -u 10001 app && mkdir /data && chown app:app /data
COPY --from=builder /out/worker /usr/local/bin/worker
COPY --from=builder /out/migrate /usr/local/bin/migrate
COPY --from=builder /out/check /usr/local/bin/check
USER app
WORKDIR /data
# 安全加固：只读根文件系统，需要 /data 作为唯一可写目录
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/worker"]
