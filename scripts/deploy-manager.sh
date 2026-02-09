#!/bin/bash
set -e

INSTALL_DIR="/home/jatin/homectll"
SERVICE_NAME="homectl-manager"

echo "=== Deploying Homectl Manager ==="

# Stop any existing instances
echo "Stopping any existing instances..."

# Stop systemd service if it exists
if systemctl is-active --quiet ${SERVICE_NAME} 2>/dev/null; then
    echo "Stopping ${SERVICE_NAME} systemd service..."
    sudo systemctl stop ${SERVICE_NAME}
fi

# Kill any stray manager processes (not managed by systemd)
if pgrep -f "homectl-manager" > /dev/null 2>&1; then
    echo "Killing stray manager processes..."
    pkill -f "homectl-manager" || true
    sleep 2
fi

# Ensure bin directory exists and binary is executable
cd "$INSTALL_DIR"
chmod +x bin/manager

# Create data directory with correct ownership
sudo mkdir -p /var/lib/homectl
sudo chown jatin:jatin /var/lib/homectl
sudo mkdir -p /etc/traefik/dynamic

# Create systemd service file
echo "Creating systemd service..."
sudo tee /etc/systemd/system/${SERVICE_NAME}.service > /dev/null << EOF
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

# Reload systemd and restart service
echo "Reloading systemd..."
sudo systemctl daemon-reload

echo "Starting ${SERVICE_NAME}..."
sudo systemctl enable ${SERVICE_NAME}
sudo systemctl restart ${SERVICE_NAME}

echo "=== Manager deployment complete ==="
echo "Check status with: systemctl status ${SERVICE_NAME}"
echo "View logs with: journalctl -u ${SERVICE_NAME} -f"
echo ""
echo "Manager should be accessible at http://localhost:1338"
