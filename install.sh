#!/usr/bin/env bash
set -euo pipefail

echo "==================================================="
echo "   UniFi Switch OS (Go + SvelteKit) - Installer    "
echo "==================================================="

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_NAME="horaco-dashboard.service"
SERVICE_USER="switchos"

if [ ! -f "$APP_DIR/switch-dashboard" ]; then
    echo "Compilando frontend SvelteKit e binário Go..."
    if command -v mise >/dev/null 2>&1; then
        (cd "$APP_DIR/frontend" && mise exec -- npm install && mise exec -- npm run build)
        (cd "$APP_DIR" && mise exec -- go build -trimpath -ldflags="-s -w" -o switch-dashboard ./cmd/server)
    else
        (cd "$APP_DIR/frontend" && npm install && npm run build)
        (cd "$APP_DIR" && go build -trimpath -ldflags="-s -w" -o switch-dashboard ./cmd/server)
    fi
fi

if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
    echo "Criando usuário de sistema sem privilégios ($SERVICE_USER)..."
    sudo useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi

sudo chown -R "$SERVICE_USER:$SERVICE_USER" "$APP_DIR"
[ -f "$APP_DIR/config.json" ] && sudo chmod 600 "$APP_DIR/config.json"
[ -f "$APP_DIR/.master.key" ] && sudo chmod 600 "$APP_DIR/.master.key"

echo "Configurando serviço systemd endurecido (/etc/systemd/system/$SERVICE_NAME)..."
sudo tee "/etc/systemd/system/$SERVICE_NAME" > /dev/null <<EOF
[Unit]
Description=UniFi Switch OS Controller (Horaco / KeepLink / RTL)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=$APP_DIR
Environment="DASHBOARD_HOST=127.0.0.1"
Environment="DASHBOARD_PORT=8080"
ExecStart=$APP_DIR/switch-dashboard
Restart=on-failure
RestartSec=5

# Hardening systemd
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full
 ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable "$SERVICE_NAME"
sudo systemctl restart "$SERVICE_NAME"

echo "==================================================="
echo " Instalação concluída! Serviço rodando em:"
echo " http://127.0.0.1:8080 (usuário: $SERVICE_USER)"
echo "==================================================="
