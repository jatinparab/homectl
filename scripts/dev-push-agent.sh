#!/usr/bin/env bash
set -euo pipefail

USER="${1:?usage: $0 <ssh-user> <host> [host...]}"
shift

for host in "$@"; do
	arch="$(ssh "${USER}@${host}" 'uname -m' 2>/dev/null || true)"
	img=""
	case "$arch" in
	x86_64) img=homectl/agent:local-amd64 ;;
	aarch64) img=homectl/agent:local-arm64 ;;
	esac
	if [ -z "$img" ]; then
		echo "WARN: $host unknown arch $arch, skipping"
		continue
	fi
	echo "==> Pushing agent ($img) to $host ($arch)..."
	docker save "$img" | ssh "${USER}@${host}" "docker load && docker tag $img homectl/agent:local" || echo "WARN: $host unreachable, skipping"
done
