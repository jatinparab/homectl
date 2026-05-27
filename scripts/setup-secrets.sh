#!/bin/bash
# Setup secrets on a remote node
# Usage: ./setup-secrets.sh <node_ip> [secrets_file]

set -e

NODE_IP="${1:?Usage: $0 <node_ip> [secrets_file]}"
SECRETS_FILE="${2:-./secrets.env}"
REMOTE_PATH="/etc/homectl/secrets.env"

if [ ! -f "$SECRETS_FILE" ]; 
    echo "Secrets file not found: $SECRETS_FILE"
    echo ""
    echo "Create a secrets.env file with:"
    echo "  B2_ACCOUNT_ID=your_account_id"
    echo "  B2_ACCOUNT_KEY=your_account_key"
    echo "  RESTIC_PASSWORD=your_restic_password"
    exit 1
fi

echo "=== Setting up secrets on $NODE_IP ==="

# Create directory and copy secrets (using jatin user with sudo)
ssh jatin@"$NODE_IP" "sudo mkdir -p /etc/homectl && sudo chmod 700 /etc/homectl"
scp "$SECRETS_FILE" jatin@"$NODE_IP":/tmp/secrets.env
ssh jatin@"$NODE_IP" "sudo mv /tmp/secrets.env $REMOTE_PATH && sudo chmod 600 $REMOTE_PATH"

echo "Secrets deployed to $NODE_IP:$REMOTE_PATH"
echo ""
echo "Make sure your systemd service includes:"
echo "  EnvironmentFile=$REMOTE_PATH"
