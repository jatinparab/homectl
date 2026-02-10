#!/bin/bash
# Privileged setup for manager deployment
# This script is meant to be run via sudo
set -e

INSTALL_DIR="/home/jatin/homectll"
SERVICE_NAME="homectl-manager"

# Create data directories
mkdir -p /var/lib/homectl
chown jatin:jatin /var/lib/homectl
mkdir -p /etc/traefik/dynamic

# Create systemd service file
cat > /etc/systemd/system/${SERVICE_NAME}.service << EOF
[Unit]
Description=Homectl Manager
After=network.target

[Service]
Type=simple
User=jatin
WorkingDirectory=${INSTALL_DIR}
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
