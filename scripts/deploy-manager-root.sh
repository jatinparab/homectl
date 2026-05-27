#!/bin/bash
# Privileged setup for manager deployment
# This script is meant to be run via sudo
set -e

INSTALL_DIR="/home/jatin/homectll"
SERVICE_NAME="homectl-manager"

# Create data directories
mkdir -p /var/lib/homectl
chown jatin:jatin /var/lib/homectl
mkdir -p /etc/caddy /var/log/homectl
touch /etc/caddy/Caddyfile
chown jatin:jatin /etc/caddy /var/log/homectl
chown jatin:jatin /etc/caddy/Caddyfile
if id caddy >/dev/null 2>&1; then
  touch /var/log/homectl/caddy-access.log
  chown caddy:caddy /var/log/homectl /var/log/homectl/caddy-access.log
  chmod 755 /var/log/homectl
  chmod 644 /var/log/homectl/caddy-access.log
fi

# Create systemd service file
cat > /etc/systemd/system/${SERVICE_NAME}.service << EOF
[Unit]
Description=Homectl Manager
After=network.target

[Service]
Type=simple
User=jatin
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=-/etc/homectl/secrets.env
ExecStart=${INSTALL_DIR}/bin/manager
Restart=always
RestartSec=10

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=homectl-manager

[Install]
WantedBy=multi-user.target
EOF

if command -v docker >/dev/null 2>&1; then
  docker stop chowkidar >/dev/null 2>&1 || true
  docker rm chowkidar >/dev/null 2>&1 || true
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl enable --now caddy >/dev/null 2>&1 || true
fi
