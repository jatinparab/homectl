# Homectl

Homectl is a container-first control plane for running Docker Compose apps across
a small homelab cluster. A manager container owns scheduling, desired state, and
the web UI; agent containers join the manager and run assigned apps on their
local Docker host.

Homectl is built for self-hosted environments where each host already runs
Docker, but the hosts should not need to share a LAN or preconfigured VPN. The
manager and agents create a mandatory WireGuard overlay and use it for
manager-agent control traffic.

## Features

- Manager web UI and HTTP API
- Agent-based Docker Compose app execution
- Mandatory WireGuard overlay between manager and agents
- Manager-issued join flow using a shared join token
- Docker-socket based orchestration from containers
- Restic-backed backup and restore plumbing
- Unit tests separated from live end-to-end cluster tests

## Architecture

Homectl runs as Docker containers with the host Docker socket mounted:

- `homectl/manager` runs on the manager host.
- `homectl/agent` runs on each agent host.
- Each Homectl container starts a sibling WireGuard container through the Docker
  socket.
- Agents bootstrap over the manager's public HTTP API, then switch steady-state
  control traffic to the WireGuard overlay.

Default overlay values:

| Setting | Value |
| --- | --- |
| Manager WireGuard IP | `10.44.0.1` |
| WireGuard UDP port | `51844` |
| Agent control URL after join | `http://10.44.0.1:1338` |

`HOMECTL_WG_ENDPOINT_IP` must be an IPv4 address, not a hostname. It should be
the public IP address agents can use to reach the manager host's UDP `51844`
listener.

## Prerequisites

- Docker Engine on the manager host and every agent host
- Docker Compose v2 for running the example compose files
- A public IPv4 address for the manager host
- UDP `51844` open to agent hosts on the manager host
- DNS for `HOMECTL_PUBLIC_HOST` pointing at the manager host
- TCP `80` and `443` open to users and new agents so Caddy can issue
  certificates, serve the manager UI, and expose `/api/join`
- A long random shared `HOMECTL_JOIN_TOKEN`
- A Git repository containing app definitions in the required [app repository structure](./docs/repo-structure.md)
- A directory on each host mounted at `/opt/homectl` in the container
- Optional: a configured Restic repository for backups

Because Homectl controls sibling containers through `/var/run/docker.sock`, the
container effectively has administrative access to the host Docker daemon. Run it
only on hosts you intend Homectl to manage.

## Quickstart

1. Build local images:

```sh
make docker-images
```

2. On the manager host, copy `docker-compose.example.yml` and edit:

```yaml
HOMECTL_JOIN_TOKEN: replace-with-a-long-random-token
HOMECTL_WG_ENDPOINT_IP: 203.0.113.10
HOMECTL_GIT_REPO: https://github.com/your-org/homelab.git
HOMECTL_DOMAIN_SUFFIX: your-domain.example
HOMECTL_PUBLIC_HOST: homectl.your-domain.example
CADDY_ACME_EMAIL: you@example.com
CADDY_DNS_PROVIDER: cloudflare
ADMIN_USER: admin
ADMIN_PASSWORD: replace-me
```

3. Start the manager:

```sh
docker compose -f docker-compose.example.yml up -d
```

4. On each agent host, copy `docker-compose.agent.example.yml` and edit:

```yaml
HOMECTL_AGENT_NAME: node-1
HOMECTL_JOIN_TOKEN: replace-with-the-manager-token
HOMECTL_MANAGER_URL: https://homectl.your-domain.example
CADDY_ACME_EMAIL: you@example.com
CLOUDFLARE_API_TOKEN: replace-with-cloudflare-token
RESTIC_REPOSITORY: s3:https://s3.example.com/bucket/path
```

5. Start the agent:

```sh
docker compose -f docker-compose.agent.example.yml up -d
```

6. Open the manager UI through Caddy:

```text
https://homectl.your-domain.example
```

The manager generates a Caddy site for `HOMECTL_PUBLIC_HOST` and reverse proxies
it to the internal manager HTTP server. The browser UI prompts for `ADMIN_USER`
and `ADMIN_PASSWORD`. Agents join through that same public origin at `/api/join`,
authenticated by `HOMECTL_JOIN_TOKEN`. After join, steady-state control traffic
uses `http://10.44.0.1:1338` over WireGuard.

The manager image uses stock Caddy and issues certificates for public manager
hosts through normal ACME HTTP/TLS-ALPN challenges. `CADDY_DNS_PROVIDER` only
controls the Caddyfile generated for gateway agents. Agents use the DNS-enabled
Caddy build and need `CLOUDFLARE_API_TOKEN` when wildcard or private-VIP
hostnames such as `*.internal.your-domain.example` must have public CA
certificates.

## Configuration

Manager environment variables:

| Variable | Required | Description |
| --- | --- | --- |
| `HOMECTL_JOIN_TOKEN` | Yes | Shared token agents present during join and TCP link setup. |
| `HOMECTL_WG_ENDPOINT_IP` | Yes | Public IPv4 address agents use for WireGuard. |
| `HOMECTL_GIT_REPO` | Yes | Git URL for the app-definition repository. |
| `HOMECTL_REPO_DIR` | Yes | Path to the app repository inside the container. |
| `HOMECTL_DOMAIN_SUFFIX` | Yes | Base domain for generated app routes. |
| `HOMECTL_PUBLIC_HOST` | Yes | Public hostname reserved for the manager UI. |
| `CADDY_ACME_EMAIL` | Recommended | Contact email used by Caddy's ACME account. |
| `CADDY_DNS_PROVIDER` | If gateway wildcard TLS is needed | DNS provider name for generated agent Caddyfiles, for example `cloudflare`. |
| `ADMIN_USER` | Recommended | Basic-auth username for the public manager UI host. |
| `ADMIN_PASSWORD` | Recommended | Basic-auth password for the public manager UI host. |

Agent environment variables:

| Variable | Required | Description |
| --- | --- | --- |
| `HOMECTL_AGENT_NAME` | Yes | Stable agent name shown in the manager. |
| `HOMECTL_JOIN_TOKEN` | Yes | Must match the manager join token. |
| `HOMECTL_MANAGER_URL` | Yes | Public manager origin served by Caddy, for example `https://homectl.your-domain.example`. |
| `HOMECTL_DOCKER_ROOT_DIR` | Yes | Host-visible path where synced Compose files are stored. |
| `HOMECTL_APPDATA_DIR` | Yes | Host-visible path for app data. |
| `HOMECTL_COMPOSE_EXTRA_ARGS` | No | Extra Docker Compose options for this agent, inserted before `up`, `down`, `pull`, `ps`, `stop`, and `logs`; for example `-f docker-compose.gpu.yml --profile gpu`. Relative `-f` paths are resolved from each app directory. |
| `CADDY_ACME_EMAIL` | Recommended | Contact email used by the gateway agent's Caddy instance. |
| `CLOUDFLARE_API_TOKEN` | If `CADDY_DNS_PROVIDER=cloudflare` | Token used by the agent Caddy DNS plugin for DNS-01 challenges. |
| `RESTIC_REPOSITORY` | Recommended | Restic repository URL for backups. |
| `RESTIC_PASSWORD` | If using backups | Restic repository password. |
| `B2_ACCOUNT_ID`, `B2_ACCOUNT_KEY` | If using B2 | Backblaze B2 credentials passed to Restic. |

## App Repository Layout

Homectl expects apps under:

```text
<HOMECTL_REPO_DIR>/docker/<app-name>/docker-compose.yml
```

On first manager startup, Homectl clones `HOMECTL_GIT_REPO` into
`HOMECTL_REPO_DIR`. On later syncs, it runs `git pull --ff-only` in that
checkout.

An app may include `homectl-config.json` next to its Compose file for Homectl
metadata such as exposed subdomains, excluded nodes, backup settings, and global
app placement.

See:

- [App Repository Structure](./docs/repo-structure.md)
- [Homectl Config](./docs/homectl-config.md)

## Development

Run unit tests:

```sh
go test ./...
```

Start the hot-reload development cluster:

```sh
make dev-up
```

Tail dev logs:

```sh
make dev-logs
```

Stop the dev cluster:

```sh
make dev-down
```

## End-to-End Tests

End-to-end tests are excluded from the default `go test ./...` run by the `e2e`
build tag.

Start an e2e cluster, run tests, and tear it down:

```sh
make e2e
```

Run e2e tests against an already-running cluster:

```sh
make e2e-tests
```

## Project Status

Homectl is early-stage software. The current public packaging path is Docker
Compose based, with the manager and agents running as containers that control
host Docker through the mounted Docker socket.
