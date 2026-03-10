# syntax=docker/dockerfile:1
# ─── Stage 1: Build server binary (for SSH deploy) ───────────────────────────
FROM golang:1.21-alpine AS server-builder

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

# ─── Stage 2: Build client binary ─────────────────────────────────────────────
FROM golang:1.21-alpine AS client-builder

RUN apk add --no-cache git gcc musl-dev

ENV GOPROXY=https://goproxy.io,https://goproxy.cn,direct
ENV GONOSUMDB=*

WORKDIR /build
COPY shared/ ./shared/
COPY client/ ./client/
WORKDIR /build/client

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GONOSUMDB=* go mod tidy && \
    CGO_ENABLED=1 GOOS=linux go build \
      -ldflags="-w -s" \
      -o tunnel-client \
      ./cmd/client/main.go

# ─── Stage 3: Final image ─────────────────────────────────────────────────────
FROM alpine:latest

RUN apk add --no-cache \
    ca-certificates \
    iptables \
    ip6tables \
    curl \
    bash \
    openssh-client \
    openssl \
    && rm -rf /var/cache/apk/*

WORKDIR /app

COPY --from=client-builder /build/client/tunnel-client .
COPY --from=server-builder /build/server/tunnel-server ./tunnel-server
COPY client/web/dist ./web/dist
COPY client/configs ./configs

RUN mkdir -p /app/data /app/logs

EXPOSE 8080 1080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/tunnel-client"]
CMD ["--config", "/app/configs/client.yml"]
