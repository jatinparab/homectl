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
- A config directory mounted at `/config` in each Homectl container
- On agents, a host directory bind-mounted at `/app-data` for managed app data
- Optional: a configured Restic repository for backups

Because Homectl controls sibling containers through `/var/run/docker.sock`, the
container effectively has administrative access to the host Docker daemon. Run it
only on hosts you intend Homectl to manage.

## Quickstart

Follow the [two-node quickstart](docs/quickstart.md) to install the preview and
run a sample Docker Compose app on an agent.

## Configuration

Manager environment variables:

| Variable | Required | Description |
| --- | --- | --- |
| `HOMECTL_JOIN_TOKEN` | Yes | Shared token agents present during join and TCP link setup. |
| `HOMECTL_WG_ENDPOINT_IP` | Yes | Public IPv4 address agents use for WireGuard. |
| `HOMECTL_GIT_REPO` | Yes | Git URL for the app-definition repository. |
| `HOMECTL_GIT_TOKEN` | If the Git repo requires auth | Token used for non-interactive HTTPS Git clone/sync. `HOMECTL_GITHUB_TOKEN` and `GITHUB_TOKEN` are also accepted. |
| `HOMECTL_GIT_USERNAME` | No | HTTPS Git username when using `HOMECTL_GIT_TOKEN`; defaults to `x-access-token` for GitHub PATs. |
| `HOMECTL_DOMAIN_SUFFIX` | Yes | Base domain for generated app routes. |
| `HOMECTL_PUBLIC_HOST` | Yes | Public hostname reserved for the manager UI. |
| `ADMIN_USER` | Recommended | Basic-auth username for the public manager UI host. |
| `ADMIN_PASSWORD` | Recommended | Basic-auth password for the public manager UI host. |
| `CADDY_ACME_EMAIL` | Recommended | Seed value for Caddy's ACME account email in encrypted shared node config. |
| `CADDY_DNS_PROVIDER` | If gateway wildcard TLS is needed | DNS provider name for generated agent Caddyfiles, for example `cloudflare`. |
| `HOMECTL_CONFIG_ENCRYPTION_KEY` | No | Optional base64-encoded 32-byte key used to encrypt manager-owned agent config at rest. If omitted, the manager generates and persists one under `/config/state/agent-config-encryption.key`. |

On first startup with no local users, the manager writes a temporary onboarding
token to `/config/state/onboarding-token` inside the container. Open the manager UI, create the first admin user with that token, then
log in again with the new username and password. Until the first user exists,
the manager serves only onboarding, health, and agent join endpoints; other
manager runtime services start after onboarding completes.

Agent environment variables:

| Variable | Required | Description |
| --- | --- | --- |
| `HOMECTL_JOIN_TOKEN` | Yes | Must match the manager join token. |
| `HOMECTL_MANAGER_URL` | Yes | Public manager origin served by Caddy, for example `https://homectl.your-domain.example`. |
| `HOMECTL_BOOTSTRAP_DNS` | Recommended | Comma-separated resolvers passed only to Homectl-owned infrastructure containers such as Technitium. Match these with the agent service's Compose `dns:` list. |
| `HOMECTL_COMPOSE_EXTRA_ARGS` | No | Extra Docker Compose options for this agent, inserted before `up`, `down`, `pull`, `ps`, `stop`, and `logs`; for example `-f docker-compose.gpu.yml --profile gpu`. Relative `-f` paths are resolved from each app directory. |
| `HOMECTL_AGENT_NAME` | No | Optional initial agent identity seed. Prefer a stable compose `hostname:` and manage later overrides from the manager UI. |
| `HOMECTL_RESTIC_USE_SUDO` | No | Run restic through `sudo` on the agent host when required. |

Shared runtime config such as `HOMECTL_LOCAL_HA_IP`, `DNS_HA_IP`, `CADDY_ACME_EMAIL`,
`CADDY_DNS_PROVIDER`, `CADDY_DNS_TOKEN_ENV`, `CF_DNS_API_TOKEN`,
`RESTIC_REPOSITORY`, `RESTIC_PASSWORD`, `B2_ACCOUNT_ID`, `B2_ACCOUNT_KEY`,
`TECHNITIUM_ADMIN_PASSWORD`, `TECHNITIUM_CLUSTER_DOMAIN`, and
`TECHNITIUM_IMAGE` belong in manager-stored encrypted node config, not agent
environment. The
manager compose file may include these values once to seed the encrypted config;
afterward, edit them from the web UI node settings dialog.

## First-class Technitium DNS

Homectl agents directly own a `homectl-technitium` container; the app repository
must not contain a Technitium Compose project. Set the shared administrator
password and each node's stable `TECHNITIUM_NODE_IP` in the node settings dialog.
The cluster domain defaults to
`cluster.home.arpa` and the image defaults to `technitium/dns-server:15.4.0`.

Every agent publishes DNS on TCP/UDP 53 and reports Technitium as a synthetic
global app. Registry images are downloaded by the agent with `skopeo` and
imported through the Docker socket before Compose starts with daemon-side pulls
disabled. Normal starts, health restarts, and backup recovery reuse local
images, including `:latest`; only missing image references are downloaded.
First-time downloads stage original OCI layers under `/config/image-cache`,
retaining completed layers across failed attempts and agent restarts. Layers
are then converted locally to gzip before Docker import, including OCI/zstd
images. Staging is removed after a successful import; interrupted partial blobs
are discarded on retry without removing completed layers.
A restart is not an image update: to deploy a new version, change the configured
tag/digest in the app repository, sync, and restart through the manager API.
Consequently, recovery does not
depend on the host Docker daemon's DNS. The initial Homectl agent image must
still be locally available or pulled successfully once; no container can
bootstrap its own missing image. Only the current gateway is backed up. The
gateway is also kept as
the Technitium primary: role changes first attempt a graceful promotion, then
force-promote after two minutes if the former primary is unavailable. Returning
former primaries are reset and rejoined as secondaries. Agent-side TCP proxies
carry Technitium admin and cluster traffic over Homectl's WireGuard mesh.

### Dedicated DNS virtual IP

Set shared `DNS_HA_IP` to a reserved, unused LAN IPv4 address (for example
`192.168.50.251`), distinct from the HTTP/gateway `LOCAL_HA_IP` (for example
`192.168.50.250`). Remove conflicting DHCP reservations and exclude both VIPs from
DHCP allocation before advertising them. Leaving `DNS_HA_IP` empty disables
this election and preserves legacy behavior.

`homectl-dns-keepalived` uses an independent VRRP instance (ID 45; HTTP uses 44).
It probes the agent's host-loopback `/dns-ready` endpoint. Eligibility requires
healthy, running Technitium and successful authoritative cluster SOA queries on
**both UDP and TCP** through the configured `TECHNITIUM_NODE_IP:53`. These checks
require neither internet access nor a reachable manager. A healthy secondary can
own the DNS VIP without becoming gateway or cluster primary. Ownership is sticky
(`nopreempt`); recovering nodes do not steal it back. With DNS HA configured,
gateway selection depends on Caddy health, not temporary DNS backup state.

Before a DNS backup stops the resolver, the agent withdraws eligibility and
waits up to 30 seconds for local VIP removal and three consecutive successful
UDP/TCP checks through the replacement VIP. Without a working replacement, it
leaves DNS running and postpones backup retries for five minutes. Completed DNS
backups suppress stale pending requests for one minute; failed uploads retain
longer backup backoff. An unexpected
failure instead incurs VRRP detection/election delay (roughly a few seconds),
so clients should retry DNS queries. VRRP requires inter-node LAN connectivity;
a network partition can produce duplicate owners and is not a quorum guarantee.

Deploy and verify VIP ownership, local/public UDP/TCP resolution, and failover
before pointing router DHCP/system DNS at the DNS VIP. Arm an independent,
persistent router/network/DNS rollback watchdog before router changes; an agent
session cannot provide recovery if the change disconnects it.

## App Repository Layout

Homectl expects apps under:

```text
/config/repo/docker/<app-name>/docker-compose.yml
```

On first manager startup, Homectl clones `HOMECTL_GIT_REPO` into
`/config/repo`. On later syncs, it fetches the configured upstream branch,
resets the checkout to that remote state, and removes untracked and ignored
files. Treat the Git remote as authoritative for everything under
`/config/repo`.

## Runtime Paths

Homectl uses fixed container paths:

| Path | Purpose |
| --- | --- |
| `/config` | Homectl config, state, generated Caddy/WireGuard files, and synced Compose files. |
| `/config/repo` | Manager Git checkout. |
| `/config/docker` | Agent copy of the repo's `docker/` directory. |
| `/app-data` | Managed app persistent data on agents. |

Agents require `/app-data` to be a host bind mount, not a Docker named volume.
Homectl inspects the running agent container, resolves the host source mounted at
`/app-data`, and passes that host path to managed Docker Compose apps as
`APPDATA`. This is required because Homectl talks to the host Docker daemon via
`/var/run/docker.sock`.

An app may include `homectl-config.json` next to its Compose file for Homectl
metadata such as exposed subdomains, excluded nodes, backup settings, and global
app placement.

See:

- [App Repository Structure](./docs/repo-structure.md)
- [Homectl Config](./docs/homectl-config.md)
- [Self-Hosted Access](./docs/self-hosted-access.md)

### Manager restart recovery

The manager persists automatic and manual singleton placements atomically in
`/config/state/placements.json`. Restarting the manager restores those placements
without turning automatic assignments into manual pins. Older installations or
invalid snapshots recover ownership from running and backup/restore reports.

For the first three minutes after startup, automatic placement changes pause
while agents reconnect. If ownership is still unknown, desired-state requests
return a retryable 503 rather than a partial list that would stop existing apps.
After this bounded window, unassigned apps and missing owners are handled normally;
regular dead-agent failover retains its three-minute heartbeat grace period.
Only the assigned node can update a singleton app’s observed state, and automatic
balancing requires an explicitly stopped app, not an unknown startup report.

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

## Project Status

Homectl is early-stage software. The current public packaging path is Docker
Compose based, with the manager and agents running as containers that control
host Docker through the mounted Docker socket.
