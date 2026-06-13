#!/usr/bin/env bash
set -euo pipefail

USER="${1:?usage: $0 <ssh-user> <compose-dir> <host> [host...]}"
COMPOSE_DIR="$2"
shift 2

for host in "$@"; do
	echo "==> Restarting agent on $host..."
	ssh "${USER}@${host}" "cd ${COMPOSE_DIR} && HOMECTL_IMAGE=homectl/agent:local docker compose down && HOMECTL_IMAGE=homectl/agent:local docker compose up -d" || echo "WARN: $host unreachable, skipping"
done
