# Homectl

Homectl manages Docker Compose applications across a small cluster of hosts from a manager process and node-local agents.

## Language

**Manager**:
The control-plane process that assigns applications, accepts node joins, and exposes the UI/API.
_Avoid_: VPS, hub

**Agent**:
The node-local process that joins a manager and runs assigned Docker Compose applications.
_Avoid_: node daemon, worker

**Join Token**:
A manager-issued shared secret that authorizes a new agent to join the cluster.
_Avoid_: password, API key

**WireGuard Overlay**:
The mandatory private network that carries manager-agent control traffic.
_Avoid_: LAN, VPN dependency

**Manager Endpoint IP**:
The public IPv4 address agents use to reach the manager's WireGuard UDP listener.
_Avoid_: hostname, VPS address

## Relationships

- A **Manager** accepts zero or more **Agents**.
- An **Agent** presents a **Join Token** before it receives **WireGuard Overlay** peer configuration.
- The **WireGuard Overlay** uses one **Manager Endpoint IP** and the default UDP port `51844`.

## Example Dialogue

> **Dev:** "Can an agent connect to the manager over the LAN first and switch to WireGuard later?"
> **Domain expert:** "No. The agent may bootstrap over the public manager API, but steady-state control traffic belongs on the WireGuard Overlay."

## Flagged Ambiguities

- "VPS server address" is not domain language. Resolved: use **Manager Endpoint IP**, and keep it explicit rather than inferred from container networking.
