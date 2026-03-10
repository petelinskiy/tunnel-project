#!/bin/bash

# Скрипт автоматического развертывания сервера
# Использование: ./deploy-server.sh <host> <user> <password>

set -e

HOST=$1
USER=$2
PASSWORD=$3

if [ -z "$HOST" ] || [ -z "$USER" ] || [ -z "$PASSWORD" ]; then
    echo "Usage: $0 <host> <user> <password>"
    echo "Example: $0 192.168.1.100 root mypassword"
    exit 1
fi

echo "🚀 Deploying tunnel server to $HOST..."

# Используем sshpass для автоматической аутентификации
if ! command -v sshpass &> /dev/null; then
    echo "Installing sshpass..."
    apt-get update && apt-get install -y sshpass
fi

# Функция для выполнения команд по SSH
run_ssh() {
    sshpass -p "$PASSWORD" ssh -o StrictHostKeyChecking=no "$USER@$HOST" "$@"
}

# Функция для копирования файлов
copy_file() {
    sshpass -p "$PASSWORD" scp -o StrictHostKeyChecking=no "$1" "$USER@$HOST:$2"
}

echo "📦 Step 1/6: Installing Docker..."
run_ssh "bash -s" << 'EOF'
    if ! command -v docker &> /dev/null; then
        curl -fsSL https://get.docker.com -o get-docker.sh
        sh get-docker.sh
        systemctl enable docker
        systemctl start docker
        rm get-docker.sh
    else
        echo "Docker already installed"
    fi
EOF

echo "🔥 Step 2/6: Configuring firewall..."
run_ssh "bash -s" << 'EOF'
    # UFW
    if command -v ufw &> /dev/null; then
        ufw --force enable
        ufw allow 443/tcp
        ufw allow 8443/tcp
    fi
    
    # iptables fallback
    iptables -A INPUT -p tcp --dport 443 -j ACCEPT
    iptables -A INPUT -p tcp --dport 8443 -j ACCEPT
    
    # Сохраняем правила
    if command -v netfilter-persistent &> /dev/null; then
        netfilter-persistent save
    fi
EOF

echo "📁 Step 3/6: Creating directories..."
run_ssh "mkdir -p /opt/tunnel/data /opt/tunnel/configs"

echo "⚙️  Step 4/6: Generating configuration..."
OBFUSCATION_KEY=$(openssl rand -base64 32 | head -c 32)
cat > /tmp/server.yml << EOF
server:
  listen_port: 443
  metrics_port: 8443

tunnel:
  obfuscation_enabled: true
  obfuscation_key: "$OBFUSCATION_KEY"

tls:
  cert_path: /app/data/cert.pem
  key_path: /app/data/key.pem
  auto_cert: false

monitoring:
  enabled: true
  metrics_endpoint: /metrics
EOF

copy_file /tmp/server.yml /opt/tunnel/configs/server.yml
rm /tmp/server.yml

echo "🔐 Step 5/6: Generating TLS certificates..."
run_ssh "bash -s" << 'EOF'
    cd /opt/tunnel/data
    if [ ! -f cert.pem ]; then
        openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem \
            -days 365 -nodes -subj "/CN=tunnel-server"
        chmod 600 key.pem
    fi
EOF

echo "🐳 Step 6/6: Starting Docker container..."
run_ssh "bash -s" << 'EOF'
    # Останавливаем старый контейнер
    docker stop tunnel-server 2>/dev/null || true
    docker rm tunnel-server 2>/dev/null || true
    
    # Запускаем новый
    docker run -d \
      --name tunnel-server \
      --restart unless-stopped \
      --network host \
      --cap-add NET_ADMIN \
      -v /opt/tunnel/data:/app/data \
      -v /opt/tunnel/configs:/app/configs \
      ghcr.io/yourusername/tunnel-server:latest \
      --config /app/configs/server.yml
    
    # Проверяем статус
    sleep 3
    docker ps | grep tunnel-server
EOF

echo ""
echo "✅ Deployment complete!"
echo ""
echo "Server details:"
echo "  Host: $HOST"
echo "  Port: 443"
echo "  Metrics: http://$HOST:8443/metrics"
echo "  Obfuscation key: $OBFUSCATION_KEY"
echo ""
echo "Add this server to your client config:"
echo "  - id: server-$HOST"
echo "    host: $HOST"
echo "    port: 443"
echo "    enabled: true"
echo ""
