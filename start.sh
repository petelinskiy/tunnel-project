#!/bin/bash
# Tunnel Proxy — первый запуск и настройка gateway
# Запускать от root: sudo ./start.sh

set -eo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SOCKS_PORT=1080
REDSOCKS_PORT=12345
WEBUI_PORT=8080

# ─── Утилиты ──────────────────────────────────────────────────────────────────

log()    { echo -e "${GREEN}✓ $*${NC}"; }
warn()   { echo -e "${YELLOW}⚠ $*${NC}"; }
info()   { echo -e "${BLUE}→ $*${NC}"; }
err()    { echo -e "${RED}✗ $*${NC}"; }
header() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  $*${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# ─── Проверки ─────────────────────────────────────────────────────────────────

echo ""
echo -e "${BLUE}╔═══════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║           Tunnel Proxy — Quick Start Setup                ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════════════════════════╝${NC}"
echo ""

# Root обязателен для gateway-режима (iptables, ip_forward)
if [[ $EUID -ne 0 ]]; then
    err "Запустите скрипт от root: sudo ./start.sh"
    exit 1
fi

OS="$(uname -s)"
if [[ "$OS" != "Linux" ]]; then
    warn "Gateway-режим работает только на Linux. На $OS доступен только SOCKS5-прокси."
    GATEWAY_MODE=false
else
    GATEWAY_MODE=true
fi

# Определяем пакетный менеджер
if command -v apt-get &>/dev/null; then
    PKG_INSTALL="apt-get install -y -q"
    PKG_UPDATE="apt-get update -q"
elif command -v dnf &>/dev/null; then
    PKG_INSTALL="dnf install -y -q"
    PKG_UPDATE="dnf check-update -q || true"
elif command -v yum &>/dev/null; then
    PKG_INSTALL="yum install -y -q"
    PKG_UPDATE="yum check-update -q || true"
else
    warn "Не удалось определить пакетный менеджер. Установите зависимости вручную."
    PKG_INSTALL=""
fi

# ─── Docker ───────────────────────────────────────────────────────────────────

header "Проверка Docker"

if ! command -v docker &>/dev/null; then
    if [[ -n "$PKG_INSTALL" ]]; then
        info "Устанавливаем Docker..."
        curl -fsSL https://get.docker.com | sh
        systemctl enable docker
        systemctl start docker
        log "Docker установлен"
    else
        err "Docker не найден. Установите: https://docs.docker.com/get-docker/"
        exit 1
    fi
else
    log "Docker $(docker --version | awk '{print $3}' | tr -d ',')"
fi

# Поддержка docker compose v2 (плагин) и docker-compose v1
if docker compose version &>/dev/null 2>&1; then
    COMPOSE="docker compose"
elif command -v docker-compose &>/dev/null; then
    COMPOSE="docker-compose"
else
    info "Устанавливаем docker-compose..."
    curl -SL "https://github.com/docker/compose/releases/latest/download/docker-compose-linux-$(uname -m)" \
        -o /usr/local/bin/docker-compose
    chmod +x /usr/local/bin/docker-compose
    COMPOSE="docker-compose"
    log "docker-compose установлен"
fi

log "Compose: $($COMPOSE version --short 2>/dev/null || $COMPOSE version | head -1)"

# ─── Директории ───────────────────────────────────────────────────────────────

header "Создание директорий"

mkdir -p client/data client/logs client/configs
mkdir -p server/data server/logs
log "Директории готовы"

# ─── Сборка и запуск контейнера ───────────────────────────────────────────────

header "Сборка Docker-образа"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

info "Сборка client-образа (может занять несколько минут)..."
$COMPOSE -f "$SCRIPT_DIR/client/docker-compose.yml" build
log "Образ собран"

header "Запуск контейнера"

$COMPOSE -f "$SCRIPT_DIR/client/docker-compose.yml" up -d
log "Контейнер запущен"

info "Ждём готовности Web UI..."
for i in $(seq 1 30); do
    if curl -sf "http://127.0.0.1:$WEBUI_PORT/health" >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

if ! curl -sf "http://127.0.0.1:$WEBUI_PORT/health" >/dev/null 2>&1; then
    err "Web UI не отвечает. Логи:"
    $COMPOSE -f "$SCRIPT_DIR/client/docker-compose.yml" logs --tail=30
    exit 1
fi
log "Web UI доступен на порту $WEBUI_PORT"

# ─── Gateway-режим (только Linux) ────────────────────────────────────────────

if [[ "$GATEWAY_MODE" == true ]]; then

    header "Настройка IP Gateway"

    # Определяем LAN-интерфейс и IP машины
    LAN_IFACE=$(ip route | awk '/^default/{print $5; exit}')
    LAN_IP=$(ip -4 addr show "$LAN_IFACE" | awk '/inet /{print $2}' | cut -d/ -f1 | head -1)

    if [[ -z "$LAN_IFACE" ]]; then
        err "Не удалось определить сетевой интерфейс. Gateway не настроен."
        GATEWAY_MODE=false
    else
        info "Интерфейс: $LAN_IFACE  |  IP: $LAN_IP"
    fi
fi

if [[ "$GATEWAY_MODE" == true ]]; then

    # ── IP forwarding ──────────────────────────────────────────────────────────
    echo 1 > /proc/sys/net/ipv4/ip_forward
    if ! grep -q "net.ipv4.ip_forward" /etc/sysctl.conf 2>/dev/null; then
        echo "net.ipv4.ip_forward = 1" >> /etc/sysctl.conf
    else
        sed -i 's/.*net.ipv4.ip_forward.*/net.ipv4.ip_forward = 1/' /etc/sysctl.conf
    fi
    log "IP forwarding включён"

    # ── Установка redsocks ─────────────────────────────────────────────────────
    if ! command -v redsocks &>/dev/null; then
        info "Устанавливаем redsocks..."
        $PKG_UPDATE >/dev/null 2>&1
        if ! $PKG_INSTALL redsocks >/dev/null 2>&1; then
            err "redsocks не удалось установить. На этом дистрибутиве gateway-режим недоступен."
            err "Установите вручную: https://github.com/darkk/redsocks"
            GATEWAY_MODE=false
        fi
    fi
    [[ "$GATEWAY_MODE" == true ]] && log "redsocks установлен"
fi

if [[ "$GATEWAY_MODE" == true ]]; then

    # ── Конфиг redsocks ────────────────────────────────────────────────────────
    cat > /etc/redsocks.conf << EOF
base {
    log_debug = off;
    log_info = on;
    log = "file:/var/log/redsocks.log";
    daemon = on;
    redirector = iptables;
}

redsocks {
    local_ip   = 127.0.0.1;
    local_port = $REDSOCKS_PORT;
    ip         = 127.0.0.1;
    port       = $SOCKS_PORT;
    type       = socks5;
}
EOF

    systemctl enable redsocks >/dev/null 2>&1
    systemctl restart redsocks
    log "redsocks запущен (слушает $REDSOCKS_PORT → SOCKS5 :$SOCKS_PORT)"

    # ── DNS: установка и настройка dnsmasq ────────────────────────────────────
    # redsocks работает только с TCP — UDP-DNS клиентов туннелировать нельзя.
    # dnsmasq перехватывает DNS от клиентов и форвардит на 8.8.8.8/1.1.1.1
    # напрямую (минуя ISP DNS). Это предотвращает утечку DNS к провайдеру.
    if ! command -v dnsmasq &>/dev/null; then
        info "Устанавливаем dnsmasq..."
        $PKG_UPDATE >/dev/null 2>&1
        $PKG_INSTALL dnsmasq >/dev/null 2>&1 || warn "dnsmasq не удалось установить — DNS клиентов будет идти напрямую"
    fi

    if command -v dnsmasq &>/dev/null; then
        # Останавливаем systemd-resolved если занимает порт 53
        if systemctl is-active --quiet systemd-resolved 2>/dev/null; then
            systemctl stop systemd-resolved >/dev/null 2>&1 || true
            systemctl disable systemd-resolved >/dev/null 2>&1 || true
            # Убираем symlink и ставим нормальный resolv.conf
            rm -f /etc/resolv.conf
            echo "nameserver 127.0.0.1" > /etc/resolv.conf
        fi

        cat > /etc/dnsmasq.d/tunnel-gateway.conf << EOF
# Слушаем на LAN-интерфейсе и localhost
listen-address=127.0.0.1,$LAN_IP
bind-interfaces

# Не читаем /etc/resolv.conf — используем только заданные upstream
no-resolv

# Upstream DNS: Google + Cloudflare (минуя ISP)
server=8.8.8.8
server=8.8.4.4
server=1.1.1.1

# Кэш
cache-size=1000

# Не пересылать запросы без точки (локальные имена)
domain-needed
bogus-priv
EOF

        systemctl enable dnsmasq >/dev/null 2>&1
        systemctl restart dnsmasq
        log "dnsmasq запущен (DNS клиентов → 8.8.8.8 / 1.1.1.1, минуя ISP)"
        DNS_READY=true
    else
        DNS_READY=false
    fi

    # ── iptables ───────────────────────────────────────────────────────────────
    # Очищаем старые правила если есть
    iptables -t nat -F REDSOCKS 2>/dev/null || true
    iptables -t nat -D PREROUTING -p tcp -j REDSOCKS 2>/dev/null || true
    iptables -t nat -X REDSOCKS 2>/dev/null || true

    # Создаём цепочку
    iptables -t nat -N REDSOCKS

    # Пропускаем локальный трафик без изменений
    iptables -t nat -A REDSOCKS -d 0.0.0.0/8      -j RETURN
    iptables -t nat -A REDSOCKS -d 10.0.0.0/8     -j RETURN
    iptables -t nat -A REDSOCKS -d 127.0.0.0/8    -j RETURN
    iptables -t nat -A REDSOCKS -d 169.254.0.0/16 -j RETURN
    iptables -t nat -A REDSOCKS -d 172.16.0.0/12  -j RETURN
    iptables -t nat -A REDSOCKS -d 192.168.0.0/16 -j RETURN
    iptables -t nat -A REDSOCKS -d 224.0.0.0/4    -j RETURN
    iptables -t nat -A REDSOCKS -d 240.0.0.0/4    -j RETURN

    # Весь остальной TCP → redsocks
    iptables -t nat -A REDSOCKS -p tcp -j REDIRECT --to-ports $REDSOCKS_PORT

    # Применяем к входящему трафику (от клиентов в сети)
    iptables -t nat -A PREROUTING -p tcp -j REDSOCKS

    # DNS клиентов (UDP + TCP 53) → dnsmasq на этой машине
    if [[ "$DNS_READY" == true ]]; then
        iptables -t nat -D PREROUTING -p udp --dport 53 -j REDIRECT --to-ports 53 2>/dev/null || true
        iptables -t nat -D PREROUTING -p tcp --dport 53 -j REDIRECT --to-ports 53 2>/dev/null || true
        iptables -t nat -A PREROUTING -p udp --dport 53 -j REDIRECT --to-ports 53
        iptables -t nat -A PREROUTING -p tcp --dport 53 -j REDIRECT --to-ports 53
        log "DNS клиентов перехвачен → dnsmasq"
    fi

    # Разрешаем форвардинг
    iptables -P FORWARD ACCEPT

    log "iptables настроены"

    # ── Сохранение правил iptables ────────────────────────────────────────────
    if command -v iptables-save &>/dev/null; then
        if command -v apt-get &>/dev/null; then
            $PKG_INSTALL iptables-persistent >/dev/null 2>&1 || true
            iptables-save > /etc/iptables/rules.v4 2>/dev/null || true
        elif command -v dnf &>/dev/null || command -v yum &>/dev/null; then
            $PKG_INSTALL iptables-services >/dev/null 2>&1 || true
            iptables-save > /etc/sysconfig/iptables 2>/dev/null || true
            systemctl enable iptables >/dev/null 2>&1 || true
        fi
    fi
    log "Правила iptables сохранены (переживут перезагрузку)"

fi

# ─── Итог ─────────────────────────────────────────────────────────────────────

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║                 Установка завершена!                      ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}Web UI:${NC}      http://$( [[ "$GATEWAY_MODE" == true ]] && echo "$LAN_IP" || echo "localhost" ):$WEBUI_PORT"
echo -e "${BLUE}SOCKS5:${NC}      $( [[ "$GATEWAY_MODE" == true ]] && echo "$LAN_IP" || echo "localhost" ):$SOCKS_PORT"
echo ""

if [[ "$GATEWAY_MODE" == true ]]; then
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${YELLOW}Настройка клиентских устройств в сети:${NC}"
    echo ""
    echo -e "  Default Gateway → ${GREEN}$LAN_IP${NC}"
    if [[ "$DNS_READY" == true ]]; then
        echo -e "  DNS             → ${GREEN}$LAN_IP${NC}  (dnsmasq на этой машине — ISP DNS не видит запросы)"
    else
        echo -e "  DNS             → ${GREEN}8.8.8.8${NC}  (вручную, dnsmasq не установлен)"
    fi
    echo ""
    echo -e "  Весь TCP-трафик устройств пойдёт через туннель."
    echo -e "  DNS-запросы перехвачены локально (dnsmasq → 8.8.8.8/1.1.1.1)."
    echo -e "  Локальный LAN-трафик (10.x / 192.168.x / 172.16.x) идёт напрямую."
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
fi

echo -e "${BLUE}Следующий шаг:${NC}"
echo -e "  Откройте Web UI, нажмите «Add Server» и введите IP VPS + SSH-логин."
echo -e "  Клиент автоматически установит сервер и подключится."
echo ""
echo -e "${BLUE}Полезные команды:${NC}"
echo -e "  Логи:      ${YELLOW}$COMPOSE -f client/docker-compose.yml logs -f${NC}"
echo -e "  Остановить:${YELLOW}$COMPOSE -f client/docker-compose.yml down${NC}"
echo -e "  Статус:    ${YELLOW}docker ps | grep tunnel${NC}"
echo ""
