# Quick Start Guide

## 5-минутный запуск

### Шаг 1: Клонирование и запуск клиента

```bash
# Клонируем репозиторий
git clone https://github.com/yourusername/tunnel-project.git
cd tunnel-project/client

# Запускаем клиент через Docker Compose
docker-compose up -d

# Проверяем логи
docker-compose logs -f
```

Откройте браузер: **http://localhost:8080**

### Шаг 2: Подготовка VPS сервера

Вам нужен VPS с:
- Ubuntu 20.04+ или Debian 11+
- Публичный IP адрес
- SSH доступ (root или sudo)
- Минимум 512 MB RAM

Рекомендуемые провайдеры:
- DigitalOcean ($6/месяц)
- Vultr ($5/месяц)
- Hetzner (€4/месяц)
- Linode ($5/месяц)

### Шаг 3: Развертывание сервера

**Вариант A: Автоматический деплой (рекомендуется)**

1. Откройте Web UI: http://localhost:8080
2. Нажмите "Add Server"
3. Заполните форму:
   - Host: IP вашего VPS
   - Port: 443
   - SSH Username: root (или ваш username)
   - SSH Password: ваш пароль
4. Нажмите "Deploy & Connect"
5. Ждите 2-3 минуты

**Вариант B: Ручной деплой через скрипт**

```bash
cd ../deploy/scripts
./deploy-server.sh YOUR_VPS_IP root YOUR_PASSWORD
```

**Вариант C: Полностью вручную**

На вашем VPS выполните:

```bash
# Установка Docker
curl -fsSL https://get.docker.com | sh

# Создание директорий
mkdir -p /opt/tunnel/data /opt/tunnel/configs

# Генерация TLS сертификатов
cd /opt/tunnel/data
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem \
    -days 365 -nodes -subj "/CN=tunnel-server"

# Создание конфига
cat > /opt/tunnel/configs/server.yml << 'EOF'
server:
  listen_port: 443
  metrics_port: 8443

tunnel:
  obfuscation_enabled: true
  obfuscation_key: "change-this-to-random-32-chars"

tls:
  cert_path: /app/data/cert.pem
  key_path: /app/data/key.pem
  auto_cert: false

monitoring:
  enabled: true
  metrics_endpoint: /metrics
EOF

# Настройка firewall
ufw allow 443/tcp
ufw allow 8443/tcp

# Запуск сервера
docker run -d \
  --name tunnel-server \
  --restart unless-stopped \
  --network host \
  --cap-add NET_ADMIN \
  -v /opt/tunnel/data:/app/data \
  -v /opt/tunnel/configs:/app/configs \
  ghcr.io/yourusername/tunnel-server:latest
```

### Шаг 4: Добавление сервера в клиент (если деплоили вручную)

Отредактируйте `client/configs/client.yml`:

```yaml
servers:
  - id: my-first-server
    host: YOUR_VPS_IP
    port: 443
    enabled: true
```

Перезапустите клиент:

```bash
docker-compose restart
```

### Шаг 5: Настройка устройств

#### Firefox

1. Настройки → Прокси
2. Выберите "Ручная настройка прокси"
3. SOCKS Host: `localhost`
4. Port: `1080`
5. SOCKS v5: ✓
6. ОК

#### Chrome/Edge (командная строка)

```bash
# Linux
google-chrome --proxy-server="socks5://localhost:1080"

# macOS
open -a "Google Chrome" --args --proxy-server="socks5://localhost:1080"

# Windows
"C:\Program Files\Google\Chrome\Application\chrome.exe" --proxy-server="socks5://localhost:1080"
```

#### Системный прокси (Linux)

```bash
# GNOME
gsettings set org.gnome.system.proxy mode 'manual'
gsettings set org.gnome.system.proxy.socks host 'localhost'
gsettings set org.gnome.system.proxy.socks port 1080

# KDE
# Настройки системы → Сеть → Прокси → SOCKS
```

#### Тестирование

```bash
# Проверка что трафик идет через туннель
curl --socks5 localhost:1080 https://ifconfig.me

# Должен показать IP вашего VPS, а не ваш реальный IP
```

### Шаг 6: Проверка работы

1. Откройте Web UI: http://localhost:8080
2. Проверьте что сервер в статусе "Active"
3. Latency должна быть < 200ms (зависит от расстояния до сервера)
4. Попробуйте открыть сайт через браузер с SOCKS5

## Следующие шаги

### Добавление нескольких серверов

Для лучшей производительности и отказоустойчивости добавьте 2-3 сервера в разных локациях:

```yaml
servers:
  - id: eu-server
    host: eu.example.com
    port: 443
    enabled: true
    
  - id: us-server
    host: us.example.com
    port: 443
    enabled: true
    
  - id: asia-server
    host: asia.example.com
    port: 443
    enabled: true
```

### Настройка балансировки

В `client.yml`:

```yaml
balancing:
  strategy: latency  # Автовыбор самого быстрого
  health_check_interval: 30s
  failover_enabled: true  # Автопереключение при падении
```

### NAT для всех устройств в сети

Если вы запустили клиент на роутере/шлюзе:

```bash
docker exec tunnel-client iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
docker exec tunnel-client iptables -A FORWARD -i eth0 -o tun0 -j ACCEPT
```

Теперь все устройства в вашей сети автоматически используют туннель!

## Troubleshooting

### Клиент не подключается

```bash
# Проверка логов
docker-compose logs tunnel-client

# Проверка доступности сервера
telnet YOUR_VPS_IP 443

# Проверка firewall на сервере
ssh root@YOUR_VPS_IP
ufw status
```

### Web UI недоступен

```bash
# Проверка что контейнер запущен
docker ps | grep tunnel-client

# Проверка портов
netstat -tulpn | grep 8080
```

### Низкая скорость

1. Добавьте серверы ближе к вам географически
2. Переключите стратегию на `latency`
3. Проверьте загрузку CPU на сервере в Web UI

## Дополнительная помощь

- [Полная документация](../README.md)
- [FAQ](./faq.md)
- [GitHub Issues](https://github.com/yourusername/tunnel-project/issues)
