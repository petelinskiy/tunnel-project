# 🚀 Tunnel Proxy - Финальная сборка

Это рабочий прототип DPI-устойчивого туннельного прокси с:
- ✅ TLS 1.3 + HTTP/2 транспорт
- ✅ Обфускация с помощью uTLS и AES-GCM
- ✅ Мультисерверная балансировка
- ✅ Web UI для управления
- ✅ SOCKS5 прокси
- ✅ Docker контейнеризация
- ✅ Автоматический деплой серверов

## 📦 Что внутри

```
tunnel-project/
├── client/          # Клиент с Web UI и SOCKS5
├── server/          # Туннельный сервер
├── shared/          # Общий код (протокол, криптография)
├── deploy/          # Скрипты автодеплоя
├── docs/            # Документация
└── .github/         # CI/CD (GitHub Actions)
```

## 🎯 Быстрый старт

### Вариант 1: Автоматический (рекомендуется)

```bash
# Распаковать архив
unzip tunnel-project.zip
cd tunnel-project

# Запустить установку
chmod +x start.sh
./start.sh

# Откроется Web UI на http://localhost:8080
```

### Вариант 2: Makefile

```bash
cd tunnel-project

# Инициализация
make init

# Сборка
make build-all

# Запуск
make run-client

# Помощь
make help
```

### Вариант 3: Docker Compose (вручную)

```bash
cd tunnel-project/client
docker-compose up -d

# Логи
docker-compose logs -f
```

## 🌐 Развертывание сервера

### Через Web UI (самое простое)

1. Открыть http://localhost:8080
2. Нажать "Add Server"
3. Ввести SSH credentials вашего VPS
4. Нажать "Deploy & Connect"

### Через скрипт

```bash
cd deploy/scripts
./deploy-server.sh YOUR_VPS_IP root YOUR_PASSWORD
```

### Вручную на VPS

```bash
# На вашем VPS выполнить:
curl -fsSL https://get.docker.com | sh

docker run -d \
  --name tunnel-server \
  --restart unless-stopped \
  --network host \
  --cap-add NET_ADMIN \
  -v /opt/tunnel/data:/app/data \
  ghcr.io/yourusername/tunnel-server:latest
```

## ⚙️ Настройка браузера

### Firefox
Настройки → Прокси → SOCKS v5 → localhost:1080

### Chrome
```bash
google-chrome --proxy-server="socks5://localhost:1080"
```

### Системный прокси (Linux)
```bash
gsettings set org.gnome.system.proxy.socks host 'localhost'
gsettings set org.gnome.system.proxy.socks port 1080
gsettings set org.gnome.system.proxy mode 'manual'
```

## 🧪 Проверка работы

```bash
# Должен показать IP вашего VPS
curl --socks5 localhost:1080 https://ifconfig.me
```

## 📚 Документация

- [Быстрый старт](docs/quickstart.md) - подробное руководство
- [README.md](README.md) - полная документация
- [CONTRIBUTING.md](CONTRIBUTING.md) - для разработчиков

## 🔧 Важные команды

```bash
# Логи клиента
docker-compose -f client/docker-compose.yml logs -f

# Остановка
docker-compose -f client/docker-compose.yml down

# Перезапуск
docker-compose -f client/docker-compose.yml restart

# Статус
docker ps | grep tunnel

# Обновление
cd client && docker-compose pull && docker-compose up -d
```

## 🐛 Troubleshooting

### Клиент не запускается
```bash
# Проверить логи
docker-compose -f client/docker-compose.yml logs

# Проверить порты
netstat -tulpn | grep -E "8080|1080"
```

### Сервер не подключается
```bash
# Проверить доступность
telnet YOUR_VPS_IP 443

# Проверить firewall на сервере
ssh root@YOUR_VPS_IP
ufw status
ufw allow 443/tcp
```

### Web UI недоступен
```bash
# Убедиться что контейнер запущен
docker ps | grep tunnel-client

# Проверить health
curl http://localhost:8080/health
```

## 📝 TODO / Улучшения

Базовый прототип готов. Для продакшена нужно доработать:

**Безопасность:**
- [ ] Генерация настоящих TLS сертификатов (Let's Encrypt)
- [ ] Аутентификация в Web UI (пароль)
- [ ] Ротация обфускационных ключей
- [ ] Rate limiting

**Функциональность:**
- [ ] Полноценный protocol.Decode с буферизованным reader
- [ ] Graceful shutdown с закрытием всех сессий
- [ ] Сохранение серверов в SQLite
- [ ] Metrics экспорт (Prometheus)
- [ ] Auto-reconnect при обрывах

**UI/UX:**
- [ ] Графики трафика в real-time
- [ ] Push-уведомления о событиях
- [ ] Экспорт/импорт конфигураций
- [ ] Темная тема

**DevOps:**
- [ ] Helm charts для Kubernetes
- [ ] Terraform модули
- [ ] Automated tests (unit, integration)
- [ ] Benchmark тесты производительности

## 🤝 Вклад в проект

Pull requests приветствуются! См. [CONTRIBUTING.md](CONTRIBUTING.md)

## 📄 Лицензия

MIT License - см. [LICENSE](LICENSE)

## ⚠️ Disclaimer

Используйте ответственно и в соответствии с законами вашей страны.

---

Сделано с ❤️ для свободного интернета
