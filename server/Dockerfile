# syntax=docker/dockerfile:1
# Build stage
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

ENV GOPROXY=https://goproxy.io,https://goproxy.cn,direct
ENV GONOSUMDB=*

WORKDIR /build

COPY shared/ ./shared/
COPY server/ ./server/
WORKDIR /build/server

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    rm -f go.sum && GONOSUMDB=* go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o tunnel-server \
    ./cmd/server/main.go

# Final stage
FROM alpine:latest

RUN apk add --no-cache \
    ca-certificates \
    netcat-openbsd \
    curl \
    && rm -rf /var/cache/apk/*

WORKDIR /app

COPY --from=builder /build/server/tunnel-server .
COPY server/configs ./configs

RUN mkdir -p /app/data /app/logs

EXPOSE 443 8443

ENTRYPOINT ["/app/tunnel-server"]
CMD ["--config", "/app/configs/server.yml"]
