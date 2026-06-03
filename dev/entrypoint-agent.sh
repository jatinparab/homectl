#!/bin/sh
set -e

# Start the Docker daemon in the background (DinD)
dockerd-entrypoint.sh &

# Wait for the Docker daemon to become ready
echo "[dev-agent] Waiting for Docker daemon..."
until docker info >/dev/null 2>&1; do
    sleep 1
done
echo "[dev-agent] Docker daemon ready"

# Create required directories
mkdir -p /app-data
mkdir -p /config/docker

# Run agent via Air — rebuilds automatically on Go source changes
exec /go/bin/air -c /src/dev/.air.agent.toml
