#!/bin/bash
# Tunnel Proxy — полная автоматическая установка
# Запускать от root: sudo bash install.sh
#
# Что делает скрипт:
#   1. Устанавливает git и скачивает проект из репозитория
#   2. Устанавливает Docker (если нет)
#   3. Собирает и запускает клиентский контейнер
#   4. Настраивает IP forwarding и DNS (Linux/gateway)
#   5. Устанавливает gost + dnsmasq (прозрачный прокси)
#   6. Настраивает nftables + GeoIP (российские IP напрямую)

set -eo pipefail

export DEBIAN_FRONTEND=noninteractive

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

SOCKS_PORT=1080
REDSOCKS_PORT=12345
WEBUI_PORT=8080

REPO_URL="https://github.com/petelinskiy/tunnel-project.git"
INSTALL_DIR="/opt/tunnel-project"

STEP=0
TOTAL_STEPS=7
STEP_START=0
SCRIPT_START=$(date +%s)

# ─── Утилиты ──────────────────────────────────────────────────────────────────

log()    { echo -e "  ${GREEN}✓ $*${NC}"; }
warn()   { echo -e "  ${YELLOW}⚠ $*${NC}"; }
info()   { echo -e "  ${BLUE}→ $*${NC}"; }
err()    { echo -e "  ${RED}✗ $*${NC}"; exit 1; }

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

# ─── Деинсталляция ────────────────────────────────────────────────────────────

do_uninstall() {
    echo ""
    echo -e "${RED}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║          Tunnel Proxy — Удаление                              ║${NC}"
    echo -e "${RED}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    if [[ $EUID -ne 0 ]]; then
        echo -e "  ${RED}✗ Запустите скрипт от root:${NC}"
        echo -e "  ${YELLOW}  sudo bash install.sh --uninstall${NC}"
        exit 1
    fi

    # Определяем compose
    if docker compose version &>/dev/null 2>&1; then
        COMPOSE="docker compose"
    elif command -v docker-compose &>/dev/null; then
        COMPOSE="docker-compose"
    fi

    echo -e "${CYAN}┌─ Остановка и удаление Docker-контейнера${NC}"
    if [[ -f "$INSTALL_DIR/client/docker-compose.yml" ]] && [[ -n "${COMPOSE:-}" ]]; then
        $COMPOSE -f "$INSTALL_DIR/client/docker-compose.yml" down --rmi all --volumes 2>/dev/null && \
            log "Контейнер и образ удалены" || warn "Контейнер уже остановлен или не найден"
    else
        docker stop tunnel-client 2>/dev/null && docker rm tunnel-client 2>/dev/null && \
            log "Контейнер удалён" || warn "Контейнер не найден"
        docker rmi client-tunnel-client 2>/dev/null && log "Образ удалён" || true
    fi
    echo -e "${CYAN}└─ ${GREEN}готово${NC}"

    echo -e "${CYAN}┌─ Удаление nftables-правил (tunnel-proxy)${NC}"
    nft delete table ip tunnel-proxy 2>/dev/null && log "nftables-таблица tunnel-proxy удалена" || \
        warn "Таблица tunnel-proxy не найдена"
    # Очищаем сохранённый конфиг
    if [[ -f /etc/nftables.conf ]]; then
        sed -i '/table ip tunnel-proxy/,/^}/d' /etc/nftables.conf 2>/dev/null || true
        log "Правила удалены из /etc/nftables.conf"
    fi
    echo -e "${CYAN}└─ ${GREEN}готово${NC}"

    echo -e "${CYAN}┌─ Остановка и удаление сервисов${NC}"
    for svc in gost-redirect update-geoip-ru.timer update-geoip-ru; do
        systemctl stop "$svc" 2>/dev/null || true
        systemctl disable "$svc" 2>/dev/null || true
    done
    rm -f /etc/systemd/system/gost-redirect.service
    rm -f /etc/systemd/system/update-geoip-ru.service
    rm -f /etc/systemd/system/update-geoip-ru.timer
    systemctl daemon-reload
    log "Systemd-сервисы удалены"

    # dnsmasq — только наш конфиг, сам dnsmasq не трогаем
    if [[ -f /etc/dnsmasq.d/tunnel-gateway.conf ]]; then
        rm -f /etc/dnsmasq.d/tunnel-gateway.conf
        systemctl restart dnsmasq 2>/dev/null || true
        log "Конфиг dnsmasq (tunnel-gateway.conf) удалён"
    fi
    echo -e "${CYAN}└─ ${GREEN}готово${NC}"

    echo -e "${CYAN}┌─ Удаление бинарников и скриптов${NC}"
    rm -f /usr/local/bin/gost
    rm -f /usr/local/bin/update-geoip-ru.sh
    log "Удалены: gost, update-geoip-ru.sh"
    echo -e "${CYAN}└─ ${GREEN}готово${NC}"

    echo -e "${CYAN}┌─ Удаление директории проекта ($INSTALL_DIR)${NC}"
    if [[ -d "$INSTALL_DIR" ]]; then
        rm -rf "$INSTALL_DIR"
        log "$INSTALL_DIR удалён"
    else
        warn "$INSTALL_DIR не найден"
    fi
    echo -e "${CYAN}└─ ${GREEN}готово${NC}"

    echo -e "${CYAN}┌─ Восстановление DNS${NC}"
    if [[ -f /etc/resolv.conf ]]; then
        current_dns=$(grep 'nameserver' /etc/resolv.conf | head -1)
        if [[ "$current_dns" == "nameserver 127.0.0.1" ]]; then
            echo "nameserver 8.8.8.8" > /etc/resolv.conf
            log "DNS восстановлен: 8.8.8.8"
        else
            info "DNS уже настроен: $current_dns"
        fi
    fi
    echo -e "${CYAN}└─ ${GREEN}готово${NC}"

    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║   ✓ Tunnel Proxy успешно удалён                               ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "  Для повторной установки:"
    echo -e "  ${YELLOW}sudo bash install.sh --install${NC}"
    echo ""
    exit 0
}

# ─── Разбор аргументов ────────────────────────────────────────────────────────

MODE=""
for arg in "$@"; do
    case "$arg" in
        --install)   MODE="install" ;;
        --uninstall) MODE="uninstall" ;;
        *)
            echo -e "  ${RED}✗ Неизвестный аргумент: $arg${NC}"
            echo -e "  Использование:"
            echo -e "    ${YELLOW}sudo bash install.sh --install${NC}"
            echo -e "    ${YELLOW}sudo bash install.sh --uninstall${NC}"
            exit 1
            ;;
    esac
done

if [[ -z "$MODE" ]]; then
    echo -e "  ${RED}✗ Укажите режим запуска:${NC}"
    echo -e "    ${YELLOW}sudo bash install.sh --install${NC}"
    echo -e "    ${YELLOW}sudo bash install.sh --uninstall${NC}"
    exit 1
fi

[[ "$MODE" == "uninstall" ]] && do_uninstall

# ─── Шапка ────────────────────────────────────────────────────────────────────

echo ""
echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║          Tunnel Proxy — Автоматическая установка              ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
echo ""

if [[ $EUID -ne 0 ]]; then
    echo -e "  ${RED}✗ Запустите скрипт от root:${NC}"
    echo -e "  ${YELLOW}  sudo bash install.sh --install${NC}"
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
    warn "Не удалось определить пакетный менеджер."
    PKG_INSTALL=""
fi

# ─── Шаг 1: Git + клонирование репозитория ────────────────────────────────────

step "Загрузка проекта из репозитория"

# Устанавливаем git если нет
if ! command -v git &>/dev/null; then
    if [[ -n "$PKG_INSTALL" ]]; then
        info "Устанавливаем git..."
        $PKG_UPDATE >/dev/null 2>&1
        $PKG_INSTALL git >/dev/null 2>&1
        log "git $(git --version | awk '{print $3}')"
    else
        err "git не найден и не удалось установить. Установите вручную: apt install git"
    fi
else
    log "git $(git --version | awk '{print $3}')"
fi

if [[ -d "$INSTALL_DIR/.git" ]]; then
    info "Директория $INSTALL_DIR уже существует — обновляем..."
    git -C "$INSTALL_DIR" pull --ff-only 2>&1 | while read line; do info "$line"; done
    log "Репозиторий обновлён"
else
    if [[ -d "$INSTALL_DIR" ]]; then
        warn "$INSTALL_DIR существует но не является git-репозиторием — удаляем..."
        rm -rf "$INSTALL_DIR"
    fi
    info "Клонируем $REPO_URL → $INSTALL_DIR ..."
    git clone --depth=1 "$REPO_URL" "$INSTALL_DIR" 2>&1 | while read line; do info "$line"; done
    log "Репозиторий загружен"
fi

cd "$INSTALL_DIR"
log "Рабочая директория: $INSTALL_DIR"

step_done

# ─── Шаг 2: Docker ────────────────────────────────────────────────────────────

step "Установка и проверка Docker"

if ! command -v docker &>/dev/null; then
    if [[ -n "$PKG_INSTALL" ]]; then
        info "Docker не найден, устанавливаем..."
        curl -fsSL https://get.docker.com | sh
        systemctl enable docker
        systemctl start docker
        log "Docker установлен"
    else
        err "Docker не найден. Установите: https://docs.docker.com/get-docker/"
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

# IPv4-only DNS для Docker daemon — BuildKit IPv6 может быть недоступен
if [[ ! -f /etc/docker/daemon.json ]] || ! grep -q '"dns"' /etc/docker/daemon.json 2>/dev/null; then
    info "Настраиваем Docker daemon: IPv4-only DNS..."
    echo '{"dns": ["8.8.8.8", "1.1.1.1"]}' > /etc/docker/daemon.json
    systemctl restart docker >/dev/null 2>&1 || true
    sleep 2
    log "Docker daemon: IPv4 DNS (8.8.8.8 / 1.1.1.1)"
fi

# sudo и hostname
HOSTNAME_VAL=$(hostname 2>/dev/null || true)
if [[ -n "$HOSTNAME_VAL" ]] && ! grep -q "$HOSTNAME_VAL" /etc/hosts 2>/dev/null; then
    echo "127.0.0.1 $HOSTNAME_VAL" >> /etc/hosts
fi

mkdir -p client/data client/logs client/configs
mkdir -p server/data server/logs
log "Рабочие директории созданы"

step_done

# ─── Шаг 3: Сборка Docker-образа ──────────────────────────────────────────────

step "Сборка Docker-образа"

info "Сборка client-образа (несколько минут при первом запуске)..."
$COMPOSE -f "$INSTALL_DIR/client/docker-compose.yml" build
log "Образ собран"

step_done

# ─── Шаг 4: Запуск контейнера ─────────────────────────────────────────────────

step "Запуск контейнера и проверка готовности"

$COMPOSE -f "$INSTALL_DIR/client/docker-compose.yml" up -d
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
    echo -e "  ${RED}✗ Web UI не отвечает после 60 секунд. Логи:${NC}"
    $COMPOSE -f "$INSTALL_DIR/client/docker-compose.yml" logs --tail=30
    exit 1
fi

log "Web UI запущен и отвечает на порту $WEBUI_PORT"
step_done

# ─── Gateway-режим (только Linux) ─────────────────────────────────────────────

if [[ "$GATEWAY_MODE" == true ]]; then

    LAN_IFACE=$(ip route | awk '/^default/{print $5; exit}')
    LAN_IP=$(ip -4 addr show "$LAN_IFACE" | awk '/inet /{print $2}' | cut -d/ -f1 | head -1)

    ALL_LAN_IPS=$(ip -4 addr show | awk '/inet / && !/127\.0\.0\.1/{print $2}' | cut -d/ -f1 \
        | while read ip; do
            iface=$(ip -4 addr | grep -B2 "inet $ip" | awk '/^[0-9]+:/{gsub(":",""); print $2}' | head -1)
            [[ "$iface" =~ ^(lo|docker|br-|veth) ]] || echo "$ip"
          done | tr '\n' ',')
    ALL_LAN_IPS="${ALL_LAN_IPS%,}"

    if [[ -z "$LAN_IFACE" ]]; then
        warn "Не удалось определить сетевой интерфейс. Gateway-режим пропущен."
        GATEWAY_MODE=false
        TOTAL_STEPS=4
    fi
fi

if [[ "$GATEWAY_MODE" == true ]]; then

    # ─── Шаг 5: IP forwarding + DNS ───────────────────────────────────────────

    step "IP forwarding и подготовка DNS"

    info "Интерфейс: $LAN_IFACE  |  IP: $LAN_IP"

    echo 1 > /proc/sys/net/ipv4/ip_forward
    if ! grep -q "net.ipv4.ip_forward" /etc/sysctl.conf 2>/dev/null; then
        echo "net.ipv4.ip_forward = 1" >> /etc/sysctl.conf
    else
        sed -i 's/.*net.ipv4.ip_forward.*/net.ipv4.ip_forward = 1/' /etc/sysctl.conf
    fi
    log "IP forwarding включён (постоянно)"

    if systemctl is-active --quiet systemd-resolved 2>/dev/null; then
        info "Останавливаем systemd-resolved (освобождаем порт 53)..."
        systemctl stop systemd-resolved
        systemctl disable systemd-resolved >/dev/null 2>&1 || true
        rm -f /etc/resolv.conf
        echo "nameserver 8.8.8.8" > /etc/resolv.conf
        log "systemd-resolved остановлен, временный DNS: 8.8.8.8"
    fi

    step_done

    # ─── Шаг 6: gost + dnsmasq ────────────────────────────────────────────────

    step "Установка и настройка gost + dnsmasq"

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
            warn "gost не удалось скачать. Gateway-режим недоступен."
            GATEWAY_MODE=false
        fi
    fi

    if [[ "$GATEWAY_MODE" == true ]]; then
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
        log "gost запущен → перехват TCP :$REDSOCKS_PORT → SOCKS5 :$SOCKS_PORT (лимит: 65536 соед.)"

        if ! command -v dnsmasq &>/dev/null; then
            info "Устанавливаем dnsmasq..."
            $PKG_UPDATE >/dev/null 2>&1
            $PKG_INSTALL dnsmasq >/dev/null 2>&1 || warn "dnsmasq не удалось установить"
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
            echo "nameserver 127.0.0.1" > /etc/resolv.conf
            log "dnsmasq запущен → DNS → 8.8.8.8 / 1.1.1.1 (ISP DNS не видит запросы)"
            DNS_READY=true
        else
            DNS_READY=false
        fi
    fi

    step_done

    # ─── Шаг 7: nftables + GeoIP ──────────────────────────────────────────────

    step "nftables: правила маршрутизации + GeoIP (Россия напрямую)"

    WAN_IFACE=$(ip route | awk '/^default/{print $5; exit}')
    GEOIP_READY=false

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

    systemctl stop ipset-russia 2>/dev/null || true
    systemctl disable ipset-russia 2>/dev/null || true
    rm -f /etc/systemd/system/ipset-russia.service

    nft delete table ip tunnel-proxy 2>/dev/null || true

    nft -f - << NFTEOF
table ip tunnel-proxy {

    set russia {
        type ipv4_addr
        flags interval
        auto-merge
    }

    chain PREROUTING {
        type nat hook prerouting priority dstnat; policy accept;

        # Docker-сети идут напрямую
        ip saddr 172.16.0.0/12 return

        # DNS клиентов → dnsmasq
        udp dport 53 redirect to :53
        tcp dport 53 redirect to :53

        # RFC-1918 и специальные адреса → напрямую
        ip daddr { 0.0.0.0/8, 10.0.0.0/8, 127.0.0.0/8,
                   169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16,
                   224.0.0.0/4, 240.0.0.0/4 } return

        # Российские IP → напрямую (GeoIP)
        ip protocol tcp ip daddr @russia return

        # Остальной TCP → gost → SOCKS5 → туннель
        ip protocol tcp redirect to :${REDSOCKS_PORT}
    }

    chain POSTROUTING {
        type nat hook postrouting priority srcnat; policy accept;
        oifname "${WAN_IFACE}" masquerade
    }
}
NFTEOF

    log "nftables-таблица tunnel-proxy применена (WAN: ${WAN_IFACE})"

    cat > /usr/local/bin/update-geoip-ru.sh << 'UPDATEEOF'
#!/bin/bash
set -euo pipefail

TABLE="tunnel-proxy"
SET="russia"
URL="https://www.ipdeny.com/ipblocks/data/countries/ru.zone"

if ! nft list set ip "$TABLE" "$SET" &>/dev/null; then
    echo "GeoIP: nftables-set '$SET' в таблице '$TABLE' не найден — запустите install.sh" >&2
    exit 1
fi

TMPFILE=$(mktemp)
NFT_SCRIPT=$(mktemp)
trap 'rm -f "$TMPFILE" "$NFT_SCRIPT"' EXIT

if ! curl -fsSL --max-time 60 -o "$TMPFILE" "$URL"; then
    echo "GeoIP: не удалось скачать список RU" >&2
    exit 1
fi

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
nft list ruleset > /etc/nftables.conf

echo "GeoIP RU: обновлено $count префиксов"
UPDATEEOF
    chmod +x /usr/local/bin/update-geoip-ru.sh

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

    info "Загружаем список IP-диапазонов России..."
    if /usr/local/bin/update-geoip-ru.sh 2>&1 | tail -1 | grep -q "обновлено"; then
        log "GeoIP: российские IP-диапазоны загружены в nftables-set"
        GEOIP_READY=true
    else
        warn "Не удалось загрузить GeoIP — весь трафик пойдёт через туннель"
    fi

    nft list ruleset > /etc/nftables.conf
    systemctl enable nftables >/dev/null 2>&1 || true
    log "Правила сохранены в /etc/nftables.conf — переживут перезагрузку"

    step_done

fi

# ─── Итоговый отчёт ───────────────────────────────────────────────────────────

TOTAL_ELAPSED=$(( $(date +%s) - SCRIPT_START ))

# Читаем сгенерированный токен из конфига (если уже записан контейнером)
AUTH_TOKEN=""
TOKEN_FILE="$INSTALL_DIR/client/configs/client.yml"
if [[ -f "$TOKEN_FILE" ]]; then
    AUTH_TOKEN=$(grep 'auth_token:' "$TOKEN_FILE" 2>/dev/null | awk '{print $2}' | tr -d '"' || true)
fi

DISPLAY_IP=$( [[ "$GATEWAY_MODE" == true ]] && echo "${LAN_IP:-localhost}" || echo "localhost" )

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
printf  "${GREEN}║   ✓ Установка завершена успешно за %-4ss                      ${GREEN}║${NC}\n" "$TOTAL_ELAPSED"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
echo ""

echo -e "${BOLD}${BLUE}Доступ к системе:${NC}"
echo -e "  Web UI   → ${GREEN}http://${DISPLAY_IP}:${WEBUI_PORT}${NC}"
echo -e "  SOCKS5   → ${GREEN}${DISPLAY_IP}:${SOCKS_PORT}${NC}"
echo -e "  Проект   → ${GREEN}${INSTALL_DIR}${NC}"
if [[ -n "$AUTH_TOKEN" ]]; then
    echo -e "  Auth     → ${YELLOW}${AUTH_TOKEN}${NC}"
else
    echo -e "  Auth     → ${YELLOW}генерируется при первом запуске, см. ${TOKEN_FILE}${NC}"
fi
echo ""

if [[ "$GATEWAY_MODE" == true ]]; then
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}${YELLOW}Настройка устройств в локальной сети:${NC}"
    echo ""
    echo -e "  Default Gateway → ${GREEN}${LAN_IP}${NC}"
    if [[ "${DNS_READY:-false}" == true ]]; then
        echo -e "  DNS Server      → ${GREEN}${LAN_IP}${NC}  (ISP-провайдер не видит DNS-запросы)"
    else
        echo -e "  DNS Server      → ${GREEN}8.8.8.8${NC}  (вручную, dnsmasq не установлен)"
    fi
    echo ""
    if [[ "$GEOIP_READY" == true ]]; then
        echo -e "  ${GREEN}●${NC} Российские IP-адреса → напрямую (GeoIP, ежедневное обновление)"
        echo -e "  ${GREEN}●${NC} Зарубежный трафик    → туннель"
    else
        echo -e "  ${YELLOW}●${NC} Весь TCP-трафик устройств → туннель (GeoIP не загружен)"
    fi
    echo -e "  ${GREEN}●${NC} LAN-трафик (10.x / 192.168.x / 172.16.x) → напрямую"
    echo -e "  ${GREEN}●${NC} Firewall: nftables (таблица tunnel-proxy)"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
fi

echo -e "${BOLD}${BLUE}Следующие шаги:${NC}"
echo ""
echo -e "  ${BOLD}1.${NC} Откройте Web UI:"
echo -e "     ${GREEN}http://${DISPLAY_IP}:${WEBUI_PORT}${NC}"
echo ""
echo -e "  ${BOLD}2.${NC} Добавьте VPS-сервер (кнопка «+ Add Server»):"
echo -e "     • Введите IP-адрес VPS"
echo -e "     • SSH-логин и пароль"
echo -e "     • Нажмите «Deploy & Connect» — сервер установится автоматически"
echo ""
echo -e "  ${BOLD}3.${NC} Проверьте туннель:"
echo -e "     ${YELLOW}curl --proxy socks5://127.0.0.1:${SOCKS_PORT} https://api.ipify.org${NC}"
echo -e "     Должен вернуть IP вашего VPS."
echo ""
if [[ "$GATEWAY_MODE" == true ]]; then
    echo -e "  ${BOLD}4.${NC} На клиентских устройствах укажите шлюз по умолчанию: ${GREEN}${LAN_IP}${NC}"
    echo ""
fi

echo -e "${BLUE}Полезные команды:${NC}"
echo -e "  Логи контейнера:   ${YELLOW}${COMPOSE} -f ${INSTALL_DIR}/client/docker-compose.yml logs -f${NC}"
echo -e "  Перезапуск:        ${YELLOW}${COMPOSE} -f ${INSTALL_DIR}/client/docker-compose.yml restart${NC}"
echo -e "  Остановить:        ${YELLOW}${COMPOSE} -f ${INSTALL_DIR}/client/docker-compose.yml down${NC}"
echo -e "  Статус:            ${YELLOW}docker ps | grep tunnel${NC}"
echo -e "  Обновить GeoIP:    ${YELLOW}sudo /usr/local/bin/update-geoip-ru.sh${NC}"
echo -e "  Переустановить:    ${YELLOW}sudo bash ${INSTALL_DIR}/install.sh --install${NC}"
echo -e "  Удалить:           ${YELLOW}sudo bash ${INSTALL_DIR}/install.sh --uninstall${NC}"
echo ""
