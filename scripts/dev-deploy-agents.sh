#!/usr/bin/env bash
set -euo pipefail

USER="${1:?usage: $0 <ssh-user> <compose-dir> <host> [host...]}"
COMPOSE_DIR="$2"
shift 2

failed=0
for host in "$@"; do
	echo "==> Restarting agent on $host..."
	if ! ssh "${USER}@${host}" "set -e
		if docker compose version >/dev/null 2>&1; then
			compose() { docker compose \"\$@\"; }
		elif command -v docker-compose >/dev/null 2>&1; then
			compose() { docker-compose \"\$@\"; }
		else
			echo 'ERROR: Docker Compose is unavailable' >&2
			exit 1
		fi
		cd '${COMPOSE_DIR}'
		HOMECTL_IMAGE=homectl/agent:local compose down
		HOMECTL_IMAGE=homectl/agent:local compose up -d"; then
		echo "ERROR: agent deployment failed on $host" >&2
		failed=1
	fi
done
exit "$failed"
