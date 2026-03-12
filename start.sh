#!/bin/bash
# Tunnel Proxy — первый запуск и настройка gateway
# Запускать от root: sudo ./start.sh

set -eo pipefail

export DEBIAN_FRONTEND=noninteractive

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

SOCKS_PORT=1080
REDSOCKS_PORT=12345
WEBUI_PORT=8080

STEP=0
TOTAL_STEPS=6
STEP_START=0
SCRIPT_START=$(date +%s)

# ─── Утилиты ──────────────────────────────────────────────────────────────────

log()    { echo -e "  ${GREEN}✓ $*${NC}"; }
warn()   { echo -e "  ${YELLOW}⚠ $*${NC}"; }
info()   { echo -e "  ${BLUE}→ $*${NC}"; }
err()    { echo -e "  ${RED}✗ $*${NC}"; }

step() {
    STEP=$((STEP + 1))
    STEP_START=$(date +%s)
    echo ""
    echo -e "${CYAN}┌─ [${STEP}/${TOTAL_STEPS}] $*${NC}"
}

step_done() {
    local elapsed=$(( $(date +%s) - STEP_START ))
    echo -e "${CYAN}└─ ${GREEN}готово${CYAN} (${elapsed}s)${NC}"
}

# ─── Шапка ────────────────────────────────────────────────────────────────────

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
    TOTAL_STEPS=3
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

# ─── Шаг 1: Docker ────────────────────────────────────────────────────────────

step "Проверка Docker"

if ! command -v docker &>/dev/null; then
    if [[ -n "$PKG_INSTALL" ]]; then
        info "Docker не найден, устанавливаем..."
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

# Прописываем IPv4-only DNS для Docker daemon.
# По умолчанию BuildKit использует IPv6 DNS (2001:4860:4860::8888), который
# может быть недоступен — тогда go mod tidy внутри build зависает/падает.
if [[ ! -f /etc/docker/daemon.json ]] || ! grep -q '"dns"' /etc/docker/daemon.json 2>/dev/null; then
    info "Настраиваем Docker daemon: IPv4-only DNS..."
    echo '{"dns": ["8.8.8.8", "1.1.1.1"]}' > /etc/docker/daemon.json
    systemctl restart docker >/dev/null 2>&1 || true
    sleep 2
    log "Docker daemon перезапущен с IPv4 DNS"
fi

# Чтобы sudo не ругался на hostname — добавляем в /etc/hosts если нет
HOSTNAME_VAL=$(hostname 2>/dev/null || true)
if [[ -n "$HOSTNAME_VAL" ]] && ! grep -q "$HOSTNAME_VAL" /etc/hosts 2>/dev/null; then
    echo "127.0.0.1 $HOSTNAME_VAL" >> /etc/hosts
fi

mkdir -p client/data client/logs client/configs
mkdir -p server/data server/logs
log "Рабочие директории созданы"

step_done

# ─── Шаг 2: Сборка образа ─────────────────────────────────────────────────────

step "Сборка Docker-образа"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

info "Сборка client-образа (может занять несколько минут при первом запуске)..."
$COMPOSE -f "$SCRIPT_DIR/client/docker-compose.yml" build
log "Образ собран"

step_done

# ─── Шаг 3: Запуск контейнера ─────────────────────────────────────────────────

step "Запуск контейнера и проверка готовности"

$COMPOSE -f "$SCRIPT_DIR/client/docker-compose.yml" up -d
log "Контейнер запущен, ожидаем Web UI..."

READY=false
for i in $(seq 1 60); do
    if curl -sf "http://127.0.0.1:$WEBUI_PORT/health" >/dev/null 2>&1; then
        echo ""
        READY=true
        break
    fi
    printf "\r  ${BLUE}→ Ожидание Web UI [%2ds / 60s]...${NC}" "$i"
    sleep 1
done

if [[ "$READY" == false ]]; then
    echo ""
    err "Web UI не отвечает после 60 секунд. Логи контейнера:"
    $COMPOSE -f "$SCRIPT_DIR/client/docker-compose.yml" logs --tail=30
    exit 1
fi

log "Web UI доступен на порту $WEBUI_PORT"
step_done

# ─── Gateway-режим (только Linux) ─────────────────────────────────────────────

if [[ "$GATEWAY_MODE" == true ]]; then

    # Определяем основной LAN-интерфейс и IP машины
    LAN_IFACE=$(ip route | awk '/^default/{print $5; exit}')
    LAN_IP=$(ip -4 addr show "$LAN_IFACE" | awk '/inet /{print $2}' | cut -d/ -f1 | head -1)

    # Собираем IP всех физических интерфейсов (исключаем lo, docker, br-)
    ALL_LAN_IPS=$(ip -4 addr show | awk '/inet / && !/127\.0\.0\.1/{print $2}' | cut -d/ -f1 \
        | while read ip; do
            iface=$(ip -4 addr | grep -B2 "inet $ip" | awk '/^[0-9]+:/{gsub(":",""); print $2}' | head -1)
            [[ "$iface" =~ ^(lo|docker|br-|veth) ]] || echo "$ip"
          done | tr '\n' ',')
    ALL_LAN_IPS="${ALL_LAN_IPS%,}"  # убираем trailing запятую

    if [[ -z "$LAN_IFACE" ]]; then
        err "Не удалось определить сетевой интерфейс. Gateway не настроен."
        GATEWAY_MODE=false
        TOTAL_STEPS=3
    fi
fi

if [[ "$GATEWAY_MODE" == true ]]; then

    # ─── Шаг 4: IP forwarding + systemd-resolved ──────────────────────────────

    step "IP forwarding и подготовка DNS"

    info "Интерфейс: $LAN_IFACE  |  IP: $LAN_IP"

    echo 1 > /proc/sys/net/ipv4/ip_forward
    if ! grep -q "net.ipv4.ip_forward" /etc/sysctl.conf 2>/dev/null; then
        echo "net.ipv4.ip_forward = 1" >> /etc/sysctl.conf
    else
        sed -i 's/.*net.ipv4.ip_forward.*/net.ipv4.ip_forward = 1/' /etc/sysctl.conf
    fi
    log "IP forwarding включён (постоянно)"

    # Останавливаем systemd-resolved ДО установки dnsmasq — иначе порт 53 занят
    if systemctl is-active --quiet systemd-resolved 2>/dev/null; then
        info "Останавливаем systemd-resolved (освобождаем порт 53)..."
        systemctl stop systemd-resolved
        systemctl disable systemd-resolved >/dev/null 2>&1 || true
        # Временный DNS чтобы apt работал до запуска dnsmasq
        rm -f /etc/resolv.conf
        echo "nameserver 8.8.8.8" > /etc/resolv.conf
        log "systemd-resolved остановлен, временный DNS: 8.8.8.8"
    fi

    step_done

    # ─── Шаг 5: gost (transparent proxy) + dnsmasq ───────────────────────────

    step "Установка и настройка gost + dnsmasq"

    # gost — transparent TCP proxy без ограничения в 128 соединений (в отличие от redsocks 0.5)
    if ! command -v gost &>/dev/null; then
        info "Скачиваем gost..."
        GOST_VER="3.0.0-rc10"
        GOST_URL="https://github.com/go-gost/gost/releases/download/v${GOST_VER}/gost_${GOST_VER}_linux_amd64.tar.gz"
        if curl -fsSL --max-time 30 -o /tmp/gost.tar.gz "$GOST_URL" 2>/dev/null; then
            tar -xzf /tmp/gost.tar.gz -C /tmp/
            mv /tmp/gost /usr/local/bin/gost
            chmod +x /usr/local/bin/gost
            rm -f /tmp/gost.tar.gz
            log "gost установлен: $(/usr/local/bin/gost -V 2>&1 | head -1)"
        else
            err "gost не удалось скачать. Gateway-режим недоступен."
            GATEWAY_MODE=false
        fi
    fi

    if [[ "$GATEWAY_MODE" == true ]]; then
        # Останавливаем redsocks если он был установлен ранее
        systemctl stop redsocks 2>/dev/null || true
        systemctl disable redsocks 2>/dev/null || true

        cat > /etc/systemd/system/gost-redirect.service << EOF
[Unit]
Description=GOST transparent TCP redirector to SOCKS5
After=network.target

[Service]
ExecStart=/usr/local/bin/gost -L "red://:${REDSOCKS_PORT}" -F "socks5://127.0.0.1:${SOCKS_PORT}"
Restart=always
RestartSec=2
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
        systemctl daemon-reload
        systemctl enable --now gost-redirect >/dev/null 2>&1
        log "gost запущен → перехват TCP на :$REDSOCKS_PORT → SOCKS5 :$SOCKS_PORT (лимит: 65536 соед.)"

        if ! command -v dnsmasq &>/dev/null; then
            info "Устанавливаем dnsmasq..."
            $PKG_UPDATE >/dev/null 2>&1
            $PKG_INSTALL dnsmasq >/dev/null 2>&1 || warn "dnsmasq не удалось установить — DNS клиентов идёт напрямую"
        fi

        if command -v dnsmasq &>/dev/null; then
            cat > /etc/dnsmasq.d/tunnel-gateway.conf << EOF
listen-address=127.0.0.1,${ALL_LAN_IPS}
bind-interfaces
no-resolv
server=8.8.8.8
server=8.8.4.4
server=1.1.1.1
cache-size=1000
domain-needed
bogus-priv
filter-AAAA
EOF
            systemctl enable dnsmasq >/dev/null 2>&1
            systemctl restart dnsmasq
            # Переключаем resolv.conf на локальный dnsmasq
            echo "nameserver 127.0.0.1" > /etc/resolv.conf
            log "dnsmasq запущен → DNS клиентов → 8.8.8.8 / 1.1.1.1 (минуя ISP) [слушает: 127.0.0.1,${ALL_LAN_IPS}]"
            DNS_READY=true
        else
            DNS_READY=false
        fi
    fi

    step_done

    # ─── Шаг 6: nftables + GeoIP ─────────────────────────────────────────────

    step "nftables: правила маршрутизации + GeoIP (Россия напрямую)"

    WAN_IFACE=$(ip route | awk '/^default/{print $5; exit}')
    GEOIP_READY=false
    GEOIP_COUNT=0

    # Устанавливаем nftables
    if [[ -n "$PKG_INSTALL" ]]; then
        $PKG_INSTALL nftables >/dev/null 2>&1 && log "nftables установлен" || warn "nftables не удалось установить"
    fi

    # Удаляем устаревшие iptables-правила нашей цепочки REDSOCKS (если были)
    iptables -t nat -F REDSOCKS 2>/dev/null || true
    iptables -t nat -D PREROUTING -p tcp -j REDSOCKS 2>/dev/null || true
    iptables -t nat -D PREROUTING -s 172.16.0.0/12 -p udp --dport 53 -j RETURN 2>/dev/null || true
    iptables -t nat -D PREROUTING -s 172.16.0.0/12 -p tcp --dport 53 -j RETURN 2>/dev/null || true
    iptables -t nat -D PREROUTING -p udp --dport 53 -j REDIRECT --to-ports 53 2>/dev/null || true
    iptables -t nat -D PREROUTING -p tcp --dport 53 -j REDIRECT --to-ports 53 2>/dev/null || true
    iptables -t nat -X REDSOCKS 2>/dev/null || true
    log "Старые iptables-правила очищены"

    # Удаляем устаревший ipset-сервис если он был
    systemctl stop ipset-russia 2>/dev/null || true
    systemctl disable ipset-russia 2>/dev/null || true
    rm -f /etc/systemd/system/ipset-russia.service

    # Удаляем нашу старую nftables-таблицу (идемпотентно при повторном запуске)
    nft delete table ip tunnel-proxy 2>/dev/null || true

    # Применяем nftables-правила — одна таблица "tunnel-proxy", не трогаем Docker/nat
    # Используем отдельную таблицу чтобы не конфликтовать с правилами Docker
    nft -f - << NFTEOF
table ip tunnel-proxy {

    # Нативный nftables-set для GeoIP — заменяет ipset (быстрее, без модуля ядра)
    set russia {
        type ipv4_addr
        flags interval
        auto-merge
    }

    chain PREROUTING {
        type nat hook prerouting priority dstnat; policy accept;

        # Docker-сети: DNS и TCP идут напрямую — BuildKit должен резолвить имена
        ip saddr 172.16.0.0/12 return

        # DNS клиентов → dnsmasq
        udp dport 53 redirect to :53
        tcp dport 53 redirect to :53

        # TCP → обработка в цепочке REDSOCKS
        tcp goto REDSOCKS
    }

    chain REDSOCKS {
        # Локальные и RFC-1918 адреса назначения → напрямую
        ip daddr { 0.0.0.0/8, 10.0.0.0/8, 127.0.0.0/8,
                   169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16,
                   224.0.0.0/4, 240.0.0.0/4 } return

        # Российские IP → напрямую (GeoIP, заполняется ниже)
        ip daddr @russia return

        # Всё остальное → gost (transparent proxy → SOCKS5)
        redirect to :${REDSOCKS_PORT}
    }

    chain POSTROUTING {
        type nat hook postrouting priority srcnat; policy accept;
        oifname "${WAN_IFACE}" masquerade
    }

    chain FORWARD {
        type filter hook forward priority filter; policy accept;
    }
}
NFTEOF

    log "nftables-таблица tunnel-proxy применена (WAN: ${WAN_IFACE})"

    # ── GeoIP: загружаем российские IP в nftables-set ─────────────────────────

    # Скрипт обновления GeoIP — использует nftables set вместо ipset
    cat > /usr/local/bin/update-geoip-ru.sh << 'UPDATEEOF'
#!/bin/bash
set -euo pipefail

TABLE="tunnel-proxy"
SET="russia"
URL="https://www.ipdeny.com/ipblocks/data/countries/ru.zone"

if ! nft list set ip "$TABLE" "$SET" &>/dev/null; then
    echo "GeoIP: nftables-set '$SET' в таблице '$TABLE' не найден — запустите start.sh" >&2
    exit 1
fi

TMPFILE=$(mktemp)
NFT_SCRIPT=$(mktemp)
trap 'rm -f "$TMPFILE" "$NFT_SCRIPT"' EXIT

if ! curl -fsSL --max-time 60 -o "$TMPFILE" "$URL"; then
    echo "GeoIP: не удалось скачать список RU" >&2
    exit 1
fi

# Атомарная замена содержимого set: flush + add в одной транзакции nft -f
count=0
{
    echo "flush set ip $TABLE $SET"
    printf "add element ip %s %s { " "$TABLE" "$SET"
    while IFS= read -r prefix; do
        [[ -z "$prefix" || "$prefix" == \#* ]] && continue
        printf "%s, " "$prefix"
        count=$((count + 1))
    done < "$TMPFILE"
    printf "}\n"
} > "$NFT_SCRIPT"

nft -f "$NFT_SCRIPT"

# Сохраняем весь ruleset (включая содержимое set) для восстановления после reboot
nft list ruleset > /etc/nftables.conf

echo "GeoIP RU: обновлено $count префиксов"
UPDATEEOF
    chmod +x /usr/local/bin/update-geoip-ru.sh

    # Таймер ежедневного обновления GeoIP
    cat > /etc/systemd/system/update-geoip-ru.service << 'EOF'
[Unit]
Description=Update Russia GeoIP nftables set
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/update-geoip-ru.sh
EOF

    cat > /etc/systemd/system/update-geoip-ru.timer << 'EOF'
[Unit]
Description=Daily Russia GeoIP update

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=3600

[Install]
WantedBy=timers.target
EOF

    systemctl daemon-reload
    systemctl enable update-geoip-ru.timer >/dev/null 2>&1

    # Первая загрузка GeoIP
    info "Загружаем список IP-диапазонов России..."
    if /usr/local/bin/update-geoip-ru.sh 2>&1 | tail -1 | grep -q "обновлено"; then
        GEOIP_COUNT=$(nft list set ip tunnel-proxy russia 2>/dev/null \
            | grep -c '\.' || echo 0)
        log "GeoIP: загружено российских IP-диапазонов в nftables-set"
        GEOIP_READY=true
    else
        warn "Не удалось загрузить GeoIP — весь трафик пойдёт через туннель"
    fi

    # Сохраняем финальный ruleset и включаем nftables для автозапуска
    nft list ruleset > /etc/nftables.conf
    systemctl enable nftables >/dev/null 2>&1 || true
    log "Правила сохранены в /etc/nftables.conf — переживут перезагрузку"

    step_done

fi

# ─── Итог ─────────────────────────────────────────────────────────────────────

TOTAL_ELAPSED=$(( $(date +%s) - SCRIPT_START ))

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║          Установка завершена за ${TOTAL_ELAPSED}s!                      ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}Web UI:${NC}  http://$( [[ "$GATEWAY_MODE" == true ]] && echo "$LAN_IP" || echo "localhost" ):$WEBUI_PORT"
echo -e "${BLUE}SOCKS5:${NC}  $( [[ "$GATEWAY_MODE" == true ]] && echo "$LAN_IP" || echo "localhost" ):$SOCKS_PORT"
echo ""

if [[ "$GATEWAY_MODE" == true ]]; then
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${YELLOW}Настройка клиентских устройств в сети:${NC}"
    echo ""
    echo -e "  Default Gateway → ${GREEN}$LAN_IP${NC}"
    if [[ "${DNS_READY:-false}" == true ]]; then
        echo -e "  DNS             → ${GREEN}$LAN_IP${NC}  (dnsmasq — ISP DNS не видит запросы)"
    else
        echo -e "  DNS             → ${GREEN}8.8.8.8${NC}  (вручную, dnsmasq не установлен)"
    fi
    echo ""
    if [[ "$GEOIP_READY" == true ]]; then
        echo -e "  Российские IP → ${GREEN}напрямую${NC}  (nftables-set, обновляется ежедневно)"
        echo -e "  Зарубежный трафик → ${GREEN}туннель${NC}"
    else
        echo -e "  Весь TCP-трафик устройств пойдёт через туннель."
    fi
    echo -e "  Локальный LAN-трафик (10.x / 192.168.x / 172.16.x) идёт напрямую."
    echo -e "  Firewall: ${GREEN}nftables${NC} (таблица tunnel-proxy)"
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
