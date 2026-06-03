# Networking

Homectl uses a mandatory WireGuard overlay for manager-agent control traffic.
WireGuard and Caddy run as in-process daemons inside the manager and agent
containers (no sidecar containers).

## Overlay Model

- WireGuard network: `10.44.0.0/24`
- Manager WireGuard IP: `10.44.0.1`
- Default WireGuard UDP port: `51844`
- Manager endpoint: explicit public IPv4 address from `HOMECTL_WG_ENDPOINT_IP`
- Public manager origin: `https://<HOMECTL_PUBLIC_HOST>` served by Caddy

Agents bootstrap by calling `https://<HOMECTL_PUBLIC_HOST>/api/join` with
`HOMECTL_JOIN_TOKEN` and their WireGuard public key. The manager allocates a
stable overlay IP, stores the peer, rewrites its WireGuard config, and returns
the manager peer information. After that, steady-state agent control traffic
uses `http://10.44.0.1:1338` and the TCP link on `10.44.0.1:1339`.

## Container Architecture

### Manager

A single `homectl-manager` container runs three components:
1. **Manager binary** — HTTP API on :1338, TCP agent link on :1339
2. **Caddy** — reverse proxy, auto-TLS on ports 80/443
3. **WireGuard** — wg0 interface on UDP port 51844

Exposed ports: 80, 443 (TCP), 51844 (UDP).
Requires `NET_ADMIN` capability and `net.ipv4.conf.all.src_valid_mark=1`.

### Agent

A single `homectl-agent` container runs four components:
1. **Agent binary** — heartbeat, reconcile, ready server on :1337
2. **Caddy** — local reverse proxy for all apps on ports 80/443
3. **WireGuard** — wg0 interface on UDP port 51844
4. **Ready server** — health/backup/sync APIs on :1337

Exposed ports: 80, 443 (TCP), 51844 (UDP).
Requires `NET_ADMIN` capability and `net.ipv4.conf.all.src_valid_mark=1`.

A separate `homectl-keepalived` container runs in `--network host` for
VRRP-based high availability (optional, configured via `HOMECTL_LOCAL_HA_IP`).

Managed app containers are connected to the `homectl-docker-network` bridge
network. They do not need host-published ports — Caddy routes traffic to them
via Docker network DNS aliases (e.g., `homectl-adguard`).

## Traffic Modes

### Mode 1: Local traffic
`passwords.internal.example.com -> 192.168.1.137 (VRRP VIP) -> agent caddy -> homectl-docker-network -> app`

### Mode 2: Public traffic
`passwords.example.com -> 65.20.72.36 (manager) -> manager caddy -> WG overlay -> gateway agent caddy -> homectl-docker-network -> app`

### Mode 3: Public local manager traffic
`uptime.example.com -> 65.20.72.36 (manager) -> manager caddy -> host-gateway -> local app`

## Required Manager Configuration

```env
HOMECTL_JOIN_TOKEN=replace-with-a-long-random-token
HOMECTL_WG_ENDPOINT_IP=203.0.113.10
HOMECTL_GIT_REPO=https://github.com/your-org/homelab.git
HOMECTL_DOMAIN_SUFFIX=your-domain.example
HOMECTL_PUBLIC_HOST=homectl.your-domain.example
```

Expose UDP `51844` on the manager host for WireGuard and TCP `80`/`443` for
Caddy. The manager's Caddy config serves `HOMECTL_PUBLIC_HOST`, proxies the UI
to the internal manager HTTP server, and exposes `/api/join` for new agents.
After join, steady-state control traffic uses `http://10.44.0.1:1338` over WireGuard.

The manager uses stock Caddy. Public hosts that resolve to the manager, such as
`*.example.com -> <manager-public-ip>`, can use Caddy's normal ACME HTTP-01 or
TLS-ALPN-01 challenges.

## Required Agent Configuration

```env
HOMECTL_AGENT_NAME=node-1
HOMECTL_JOIN_TOKEN=replace-with-the-manager-token
HOMECTL_MANAGER_URL=https://homectl.your-domain.example
RESTIC_REPOSITORY=s3:https://s3.example.com/bucket/path
```

Mount `/config` into every Homectl container. On agents, mount a host directory
at `/app-data` using a bind mount. Homectl resolves that host path and passes it
to managed app Compose files as `APPDATA`.

Gateway agents use the DNS-enabled Caddy build when `CADDY_DNS_PROVIDER` is set
on the manager. The generated agent Caddyfile includes DNS-01 TLS config and the
agent environment must contain the provider token, for example
`CLOUDFLARE_API_TOKEN`. This is required for wildcard certificates and for
hostnames that resolve to a private VIP, such as
`*.internal.example.com -> 192.168.1.137`.

## VRRP / Keepalived

The keepalived health check uses Caddy's HTTP listener on port 80:
`wget -qO- --timeout=2 http://127.0.0.1/is-gateway >/dev/null`

Caddy forwards `/is-gateway` to the ready server on port 1337, which
checks whether this agent is the designated gateway node.
