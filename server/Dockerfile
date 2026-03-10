# Build stage
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

ENV GOPROXY=https://goproxy.io,https://goproxy.cn,direct
ENV GONOSUMDB=*

WORKDIR /build

# Copy shared module
COPY shared/ ./shared/

# Copy server go.mod (без go.sum — пересоздадим через tidy)
COPY server/go.mod ./server/go.mod
WORKDIR /build/server
RUN go mod download all

# Copy server source
COPY server/ .

# Tidy + build (tidy обновит go.sum под актуальные зависимости)
RUN rm -f go.sum && GONOSUMDB=* go mod tidy && \
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
