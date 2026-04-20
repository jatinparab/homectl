#!/bin/sh
set -e

# Start the Docker daemon in the background (DinD)
dockerd-entrypoint.sh &
DOCKERD_PID=$!

# Wait for the Docker daemon to become ready
echo "Waiting for Docker daemon..."
until docker info >/dev/null 2>&1; do
    sleep 1
done
echo "Docker daemon ready"

# Create required directories
mkdir -p "${HOMECTL_APPDATA_DIR:-/appdata}"
mkdir -p "${HOMECTL_DOCKER_ROOT_DIR:-/docker}"

# Run the agent (agent syncs docker configs from manager)
exec /usr/local/bin/agent
