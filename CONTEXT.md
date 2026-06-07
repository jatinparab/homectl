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

**User**:
A local manager login identity that can access the admin UI/API with a username and password.
_Avoid_: agent, node

**Session**:
A browser login session for a User, represented by an opaque cookie and persisted server-side.
_Avoid_: API key, join token

**Onboarding Token**:
A temporary manager-startup token logged only while no User exists, used to create the first User.
_Avoid_: password, join token

**Agent Config**:
A manager-owned per-Agent JSON document stored encrypted at rest and periodically synced to that Agent.
_Avoid_: app config, homectl-config.json

## Relationships

- A **Manager** accepts zero or more **Agents**.
- An **Agent** presents a **Join Token** before it receives **WireGuard Overlay** peer configuration.
- The **WireGuard Overlay** uses one **Manager Endpoint IP** and the default UDP port `51844`.
- A **User** authenticates to the Manager UI/API with a username and password.
- A **Session** authorizes browser API calls after a User logs in.
- An **Onboarding Token** can create only the first User and is invalidated after onboarding.
- An **Agent Config** belongs to exactly one Agent.

## Example Dialogue

> **Dev:** "Can an agent connect to the manager over the LAN first and switch to WireGuard later?"
> **Domain expert:** "No. The agent may bootstrap over the public manager API, but steady-state control traffic belongs on the WireGuard Overlay."

## Flagged Ambiguities

- "VPS server address" is not domain language. Resolved: use **Manager Endpoint IP**, and keep it explicit rather than inferred from container networking.
