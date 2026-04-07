# Networking Architecture

## Overview

The system uses two separate networks:

- **LAN (`192.168.1.0/24`)** — physical node connectivity and keepalived VIP failover
- **WireGuard VPN (`10.42.1.0/24`)** — encrypted overlay for all manager↔agent communication

Internet traffic enters via a floating Virtual IP (keepalived), hits Traefik on the active gateway node, and is routed either locally (Docker bridge) or across WireGuard to apps running on other nodes.

---

## Network Topology

```
[Internet] HTTP/HTTPS (80/443)
    |
    ▼
[VIP 192.168.50.250]  ← keepalived VRRP, floats between nodes
    |
    ▼
[Active Gateway Node]
 ├─ Traefik container  (routes HTTP/HTTPS)
 │   ├─ Local apps  → 172.17.0.1:<port>  (Docker bridge)
 │   └─ Remote apps → 10.42.1.10x:<port> (WireGuard)
 │
 └─ Agent (port 1337)
     ├─ Heartbeat  → Manager 10.42.1.100:1338  (every 10s)
     ├─ Desired-state fetch  ← Manager (every 10s)
     ├─ Traefik config fetch ← Manager (every 10s)
     └─ Docker config sync   ← Manager (on change)

[WireGuard mesh — 10.42.1.0/24, UDP 51824]
 VPS hub  10.42.1.100  (203.0.113.10)   ← Manager runs here
 main     10.42.1.101  (192.168.50.101)
 rpi      10.42.1.102  (192.168.50.102)
 node-3 10.42.1.103 (192.168.50.103)
 local    10.42.1.104  (developer machine)
```

---

## Components

### Manager (VPS, `10.42.1.100:1338`)

Runs on the VPS hub. Exposes an HTTP API on port **1338** that all agents call over WireGuard. Responsibilities:

- Stores desired state for all apps
- Generates per-node Traefik dynamic configs
- Serves docker-compose archives to agents
- Elects the gateway node (which node runs Traefik as the active ingress)
- Runs the reconciliation loop (assigns, migrates, rebalances apps across nodes)

### Agent (each node, port `1337`)

Runs on every node. Polls the manager and executes locally. Exposes a **ReadyServer on port 1337** used by keepalived health checks and by the manager to trigger app restarts.

Agent timing:
| Interval | Action |
|---|---|
| 10s | Heartbeat to manager (reports app states, node metrics) |
| 10s | Fetch desired app state from manager |
| 10s | Fetch Traefik config from manager |
| 5s | Reconnect retry if disconnected |

### Traefik

One Traefik instance runs on the active gateway node. The manager generates its dynamic config: local apps are routed via `172.17.0.1:<port>` (Docker bridge gateway), remote apps via `10.42.1.10x:<port>` (WireGuard). The config is written to `/appdata/traefik/config/traefik.yml` by the agent only when it differs from disk, avoiding unnecessary reloads.

Public domain: `{app}.example.net`
Internal domain: `{app}.internal.example.net`

### Gateway Election

The manager continuously monitors which node is the active Traefik gateway (`reconcile.go`):

1. If a manual gateway override is set and healthy → use it
2. If the current gateway still has Traefik running and healthy → keep it
3. Otherwise scan all online agents and elect the first healthy Traefik instance
4. On gateway change, the manager restarts **newt** on all involved nodes (newt depends on gateway identity)

---

## Keepalived — VIP Failover

Configured via `setup-keepalived` in the homelab repo.

- **Virtual IP:** `192.168.50.250` (floats to whichever node is healthy)
- **VRRP Router ID:** 44
- **Mode:** All nodes start as `BACKUP` with `nopreempt` — the VIP doesn't bounce back to a higher-priority node when it recovers; it only moves on failure

Node priorities (higher wins on failover):

| Node | LAN IP | Priority |
|---|---|---|
| main | 192.168.50.101 | 120 |
| rpi | 192.168.50.102 | 110 |
| node-3 | 192.168.50.103 | 100 |

**Health check:** Every 2 seconds keepalived runs:
```sh
curl -fsS http://127.0.0.1:1337/ready
```
This hits the agent's ReadyServer (port 1337), which returns healthy only when the agent is connected to the manager and Traefik is running and healthy. Three consecutive failures trigger VIP failover; two consecutive passes restore readiness.

**Failover sequence:**

1. Agent on current VIP holder fails its health check (3×)
2. keepalived withdraws the VIP from that node
3. Node with next-highest priority that is passing health checks acquires the VIP
4. Concurrently, the manager's gateway election detects Traefik is unhealthy on the old gateway and elects a new one
5. Traffic resumes — the VIP and the elected Traefik gateway converge on the same healthy node

---

## WireGuard VPN Mesh

Configured via `setup-wireguard` in the homelab repo.

**Topology:** Star with the VPS as hub. Nodes do not peer directly with each other; inter-node traffic is routed through the VPS hub.

```
           VPS Hub
       10.42.1.100
      /     |      \
  .101    .102    .103
  main    rpi   node-3
```

**Parameters:**

| Setting | Value |
|---|---|
| Interface | `wg0` |
| Network | `10.42.1.0/24` |
| Port | `51824` UDP |
| VPS endpoint | `203.0.113.10:51824` |

**Key management:** Private keys are generated on each node and stored at `/etc/wireguard/.privkey` (mode 600). Public keys are exchanged manually between nodes during setup. Keys are never transmitted in plaintext.

**Node AllowedIPs:** Each node allows all `10.42.1.0/24` traffic through the VPS peer, enabling node-to-node routing without direct peering.

**Startup order:** VPS WireGuard must be up before nodes connect (nodes initiate outbound connections; VPS listens).

All management traffic (agent heartbeats, desired-state fetches, docker syncs) travels over this encrypted tunnel. The manager on the VPS communicates back to agents using their WireGuard IPs (`10.42.1.10x`), not their LAN IPs.

---

## Port Reference

| Port | Protocol | Direction | Purpose |
|---|---|---|---|
| 1338 | TCP | Agent → Manager | Manager HTTP API (heartbeat, desired state, configs, file sync) |
| 1337 | TCP | keepalived → Agent, Manager → Agent | Agent ReadyServer (health, app restart triggers) |
| 80 | TCP | Internet → VIP | Traefik HTTP ingress |
| 443 | TCP | Internet → VIP | Traefik HTTPS ingress |
| 51824 | UDP | All nodes ↔ VPS | WireGuard VPN tunnel |

---

## Traffic Flow: Inbound HTTP Request

```
Browser → 192.168.50.250:443 (VIP, held by active node)
  → Traefik on active node
  → If app is LOCAL:  172.17.0.1:<app-port>  (Docker bridge)
  → If app is REMOTE: 10.42.1.10x:<app-port> (WireGuard to peer node)
  → App container responds → Traefik → Browser
```

## Traffic Flow: Agent Heartbeat

```
Agent on 10.42.1.10x → Manager on 10.42.1.100:1338 (WireGuard tunnel)
  POST /agents/heartbeat  { name, ip, app_reports, node_metrics }
  ← 200 OK

Agent → GET /agents/{id}/desired
  ← { apps: [...], is_gateway: bool }

Agent → GET /agents/{id}/traefik-config
  ← Traefik dynamic config YAML

Agent → GET /docker/sync
  ← tar.gz of docker-compose files
```
