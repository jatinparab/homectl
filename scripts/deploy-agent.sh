#!/bin/bash
set -e

INSTALL_DIR="/home/jatin/homectll"
SERVICE_NAME="homectl-agent"
SECRETS_DIR="/etc/homectl"
SECRETS_FILE="${SECRETS_DIR}/secrets.env"

echo "=== Deploying Homectl Agent ==="

# Stop any existing instances
echo "Stopping any existing instances..."

# Stop systemd service if it exists
if systemctl is-active --quiet ${SERVICE_NAME} 2>/dev/null; then
    echo "Stopping ${SERVICE_NAME} systemd service..."
    sudo systemctl stop ${SERVICE_NAME}
fi

# Kill any stray agent processes (not managed by systemd)
if pgrep -f "homectl-agent" > /dev/null 2>&1; then
    echo "Killing stray agent processes..."
    pkill -f "homectl-agent" || true
fi

# Wait for ready server port (1337) to be released
READY_PORT=1337
echo "Waiting for port ${READY_PORT} to be released..."
for i in {1..30}; do
    if ! ss -tlnp 2>/dev/null | grep -q ":${READY_PORT} " && \
       ! netstat -tlnp 2>/dev/null | grep -q ":${READY_PORT} "; then
        echo "Port ${READY_PORT} is free"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "WARNING: Port ${READY_PORT} still in use after 30 seconds"
        echo "Checking what's using it..."
        ss -tlnp 2>/dev/null | grep ":${READY_PORT} " || netstat -tlnp 2>/dev/null | grep ":${READY_PORT} " || true
    fi
    sleep 1
done

# Ensure bin directory exists and binary is executable
cd "$INSTALL_DIR"
chmod +x bin/agent

# Create systemd service file
echo "Creating systemd service..."
sudo tee /etc/systemd/system/${SERVICE_NAME}.service > /dev/null << EOF
[Unit]
Description=Homectl Agent
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=jatin
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${SECRETS_FILE}
EnvironmentFile=-/etc/homectl/agent.env
ExecStart=${INSTALL_DIR}/bin/agent
Restart=always
RestartSec=10

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=homectl-agent

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd and restart service
echo "Reloading systemd..."
sudo systemctl daemon-reload

echo "Starting ${SERVICE_NAME}..."
sudo systemctl enable ${SERVICE_NAME}
sudo systemctl restart ${SERVICE_NAME}

echo "=== Agent deployment complete ==="
echo "Check status with: systemctl status ${SERVICE_NAME}"
echo "View logs with: journalctl -u ${SERVICE_NAME} -f"
