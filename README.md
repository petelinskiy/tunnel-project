# Secure Tunnel Proxy

DPI-устойчивый туннельный прокси с TLS обфускацией, мультисерверной балансировкой и Web UI для управления.

## 🎯 Особенности

- ✅ **DPI-устойчивость**: TLS 1.3 + HTTP/2 с обфускацией fingerprint
- ✅ **Мультисервер**: автоматическая балансировка нагрузки между серверами
- ✅ **Web UI**: удобное управление серверами через браузер
- ✅ **Автодеплой**: развертывание серверов в один клик через SSH
- ✅ **Мониторинг**: real-time метрики (latency, CPU, RAM, bandwidth)
- ✅ **Docker**: полная контейнеризация клиента и сервера
- ✅ **SOCKS5**: поддержка всех приложений через SOCKS прокси

## 📋 Требования

### Клиент
- Docker и Docker Compose
- Linux с поддержкой iptables (для NAT)
- 512 MB RAM минимум

### Сервер
- VPS с публичным IP
- Linux (Ubuntu 20.04+, Debian 11+)
- SSH доступ
- 256 MB RAM минимум

## 🚀 Быстрый старт

### 1. Запуск клиента

```bash
# Клонируем репозиторий
git clone https://github.com/yourusername/tunnel-project.git
cd tunnel-project/client

# Запускаем через Docker Compose
docker-compose up -d

# Проверяем статус
docker-compose logs -f tunnel-client
```

Откройте Web UI: **http://localhost:8080**

### 2. Добавление сервера

**Вариант А: Автоматический деплой через Web UI**

1. Откройте http://localhost:8080
2. Перейдите в "Add Server"
3. Введите SSH credentials вашего VPS
4. Нажмите "Deploy & Connect"
5. Ждите ~2-3 минуты

**Вариант Б: Ручной деплой сервера**

```bash
# На вашем VPS
cd /opt
git clone https://github.com/yourusername/tunnel-project.git
cd tunnel-project/server

# Запускаем сервер
docker run -d \
  --name tunnel-server \
  --restart unless-stopped \
  --network host \
  --cap-add NET_ADMIN \
  -v /opt/tunnel/data:/app/data \
  ghcr.io/yourusername/tunnel-server:latest
```

Затем добавьте сервер в клиенте вручную через Web UI.

### 3. Настройка устройств

**SOCKS5 прокси доступен на:**
- Host: IP клиента (или localhost если на той же машине)
- Port: 1080
- Без аутентификации (по умолчанию)

**Примеры настройки:**

```bash
# Firefox
Настройки → Прокси → SOCKS5: localhost:1080

# Chrome/Chromium (Linux)
google-chrome --proxy-server="socks5://localhost:1080"

# curl
curl --socks5 localhost:1080 https://ifconfig.me

# Системный прокси (GNOME)
gsettings set org.gnome.system.proxy mode 'manual'
gsettings set org.gnome.system.proxy.socks host 'localhost'
gsettings set org.gnome.system.proxy.socks port 1080
```

**NAT для всех устройств в сети (опционально):**

Если клиент запущен на роутере/шлюзе:

```bash
# На клиенте выполнить
docker exec tunnel-client /app/scripts/setup-nat.sh

# Теперь все устройства в сети автоматически используют туннель
```

## 📊 Web UI функции

### Dashboard
- Статус всех серверов
- Real-time метрики (latency, load, traffic)
- Графики использования

### Server Management
- Добавление/удаление серверов
- Автоматический деплой через SSH
- Ручная настройка

### Load Balancing
- Round Robin
- Latency-based (автовыбор самого быстрого)
- Least Loaded (наименее загруженный)
- Auto failover при падении

### Monitoring
- CPU, RAM, Network usage
- Active connections
- Bandwidth statistics
- Health checks

## 🏗️ Архитектура

```
┌─────────────┐
│  Устройства │
└──────┬──────┘
       │ SOCKS5 (port 1080)
       ▼
┌─────────────────────┐
│   Tunnel Client     │ ◄── Web UI (port 8080)
│   (Docker)          │
└──────┬──────────────┘
       │ TLS 1.3 + HTTP/2 + Obfuscation
       ▼
┌──────────────────┐    ┌──────────────────┐    ┌──────────────────┐
│ Tunnel Server 1  │    │ Tunnel Server 2  │    │ Tunnel Server N  │
│   (Docker)       │    │   (Docker)       │    │   (Docker)       │
└────────┬─────────┘    └────────┬─────────┘    └────────┬─────────┘
         │                       │                       │
         └───────────────────────┴───────────────────────┘
                                 │
                                 ▼
                            Internet
```

## 🔒 Безопасность

- **TLS 1.3**: современное шифрование
- **uTLS**: маскировка под популярные браузеры (Chrome, Firefox, Safari)
- **Обфускация**: дополнительный слой XOR/AES поверх TLS
- **Traffic padding**: скрытие паттернов размера пакетов
- **Ephemeral keys**: уникальные ключи для каждой сессии
- **No logs**: сервер не хранит логи трафика

## 🛠️ Разработка

### Сборка из исходников

```bash
# Клонируем
git clone https://github.com/yourusername/tunnel-project.git
cd tunnel-project

# Сборка клиента
cd client
docker build -t tunnel-client:dev .

# Сборка сервера
cd ../server
docker build -t tunnel-server:dev .
```

### Локальная разработка

```bash
# Клиент
cd client
go run cmd/client/main.go --config configs/client.yml

# Сервер
cd server
go run cmd/server/main.go --config configs/server.yml
```

### Тестирование

```bash
# Unit tests
go test ./...

# Integration tests
cd tests
./run-integration-tests.sh
```

## 📝 Конфигурация

### Client Config

```yaml
# client/configs/client.yml
client:
  web_ui_port: 8080
  socks5_port: 1080
  data_dir: /app/data

balancing:
  strategy: latency  # round-robin, latency, least-loaded
  health_check_interval: 30s
  failover_enabled: true

servers:
  - host: server1.example.com
    port: 443
    enabled: true
  - host: server2.example.com
    port: 443
    enabled: true
```

### Server Config

```yaml
# server/configs/server.yml
server:
  listen_port: 443
  metrics_port: 8443

tunnel:
  obfuscation_enabled: true
  obfuscation_key: "your-secret-key-change-me"
  
tls:
  cert_path: /app/data/cert.pem
  key_path: /app/data/key.pem
  auto_cert: true  # Let's Encrypt

monitoring:
  enabled: true
  metrics_endpoint: /metrics
```

## 🐛 Troubleshooting

### Клиент не подключается к серверу

```bash
# Проверка логов
docker-compose logs tunnel-client

# Проверка доступности сервера
curl -v https://your-server.com:443

# Проверка firewall на сервере
# На сервере:
sudo ufw status
sudo ufw allow 443/tcp
```

### Web UI недоступен

```bash
# Проверка портов
docker-compose ps
netstat -tulpn | grep 8080

# Проверка healthcheck
curl http://localhost:8080/health
```

### Низкая скорость

1. Проверьте latency в Web UI
2. Переключите балансировку на latency-based
3. Добавьте серверы ближе географически
4. Проверьте загрузку CPU на сервере

### SOCKS5 не работает

```bash
# Тест SOCKS5
curl --socks5 localhost:1080 https://ifconfig.me

# Проверка, что порт слушается
netstat -tulpn | grep 1080

# Логи клиента
docker-compose logs -f tunnel-client
```

## 📦 Обновление

### Клиент

```bash
cd client
docker-compose pull
docker-compose up -d
```

### Сервер

```bash
# Через Web UI: "Update Server" кнопка

# Или вручную на сервере:
docker pull ghcr.io/yourusername/tunnel-server:latest
docker stop tunnel-server
docker rm tunnel-server
docker run -d --name tunnel-server ... # (та же команда что при установке)
```

## 🤝 Contributing

1. Fork проект
2. Создайте feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit изменения (`git commit -m 'Add some AmazingFeature'`)
4. Push в branch (`git push origin feature/AmazingFeature`)
5. Откройте Pull Request

## 📄 License

MIT License - см. [LICENSE](LICENSE) файл

## ⚠️ Disclaimer

Этот проект предназначен для легитимного использования в соответствии с законодательством вашей страны. Авторы не несут ответственности за неправомерное использование.

## 🔗 Полезные ссылки

- [Документация](docs/)
- [API Reference](docs/api.md)
- [FAQ](docs/faq.md)
- [Примеры использования](examples/)

---

Сделано с ❤️ для обхода цензуры
