# Tunnel Project — Memory

## Что это
DPI-устойчивый туннельный прокси. Клиент → TLS+smux → Сервер → интернет.

## Стек
- **TLS**: uTLS `HelloChrome_Auto` (Chrome fingerprint, маскировка под браузер)
- **Мультиплексинг**: smux поверх TLS (один коннект, много потоков)
- **SNI**: случайный из списка CDN-доменов (google, cloudflare, etc.) на каждый коннект
- **Jitter**: случайная задержка 0–500ms перед каждым подключением
- **SOCKS5**: клиент слушает :1080
- **Web UI**: клиент слушает :8080

## Структура проекта
```
tunnel-project/
├── server/                  # Tunnel server (деплоится на VPS)
│   ├── cmd/server/main.go
│   ├── internal/tunnel/server.go   # TLS listen + smux + handleStream
│   ├── configs/server.yml
│   ├── Dockerfile           # BuildKit cache mount, no go mod download all
│   └── docker-compose.yml
├── client/                  # Tunnel client (деплоится в локальной сети)
│   ├── cmd/client/main.go
│   ├── internal/tunnel/manager.go  # uTLS + smux + SNI/jitter
│   ├── internal/tunnel/balancer.go
│   ├── internal/webui/server.go    # Web UI + API
│   ├── internal/deploy/deployer.go # SSH-деплой сервера
│   ├── configs/client.yml
│   ├── web/dist/index.html
│   ├── Dockerfile           # 3 stages: server-builder, client-builder, final
│   └── docker-compose.yml
├── shared/models/models.go  # ServerConfig, ClientConfig, ServerInfo, ServerMetrics
└── start.sh                 # Автоустановка: Docker + redsocks + dnsmasq + iptables
```

## Ключевые решения

### Почему smux, а не HTTP/2
Пробовали http2.Transport поверх uTLS — utls v1.6.0 не заполняет
`ConnectionState().NegotiatedProtocol` после хендшейка → `http2.Transport`
видит ALPN="" → "unexpected EOF". Откат на смux. DPI-резистентность
достигается через Chrome TLS fingerprint, SNI-рандомизацию и jitter.

### Gateway-режим (start.sh)
На Linux автоматически:
1. IP forwarding (`net.ipv4.ip_forward=1`)
2. redsocks: перехват TCP → SOCKS5:1080
3. dnsmasq: перехват DNS клиентов → 8.8.8.8/1.1.1.1 (минуя ISP DNS)
4. iptables PREROUTING: весь TCP → redsocks, UDP/TCP 53 → dnsmasq
Клиентским устройствам: gateway=<IP этой машины>, DNS=<IP этой машины>

### Dockerfile оптимизация
Убран `go mod download all` (качает всё включая тест-зависимости, зависает).
Заменён на `--mount=type=cache,target=/go/pkg/mod` — BuildKit кэширует модули.

### SSH-деплой серверов
client/Dockerfile содержит server-builder stage → `/app/tunnel-server`.
При клике "Add Server" в Web UI: SSH на VPS → копирует бинарник → запускает.

## Конфиг клиента (client/configs/client.yml)
```yaml
tunnel:
  obfuscation_key: "b29406e1cf214e2ebdb8ac0e6a54b11a"
  jitter_max_ms: 500
  sni_list:
    - "www.google.com"
    - "www.youtube.com"
    - "www.cloudflare.com"
    - "cdn.jsdelivr.net"
    - "ajax.googleapis.com"
    - "fonts.googleapis.com"
    - "www.gstatic.com"
    - "storage.googleapis.com"
balancing:
  strategy: latency
  health_check_interval: 30s
```

## Модели (shared/models/models.go)
ServerConfig, ClientConfig содержат SNIList []string и JitterMaxMs int.

## Сборка и запуск
```bash
docker compose -f server/docker-compose.yml build && docker compose -f server/docker-compose.yml up -d
docker compose -f client/docker-compose.yml build && docker compose -f client/docker-compose.yml up -d
```

## Известные ограничения
- UDP-трафик (кроме DNS) не туннелируется — redsocks работает только с TCP
- Против GFW active probing слабее Xray+Reality (нет fallback handler)
- SNI не совпадает с реальным IP сервера — видно при глубоком анализе
