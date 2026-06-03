# Codex Caddy Migration Plan

## Goals
- Replace Traefik with Caddy as the built-in gateway proxy without regressing ingress, health, or manager→agent flows.
- Keep the "gateway" abstraction manager-controlled: gateway node always runs the proxy, others explicitly stop it.
- Translate every Traefik-only feature in the repo to an equivalent that Caddy can provide, including config sync, routing, TLS, readiness, and test coverage.

## Current Traefik Responsibilities & Caddy Replacements
| Capability | How Traefik is used today | Caddy replacement strategy |
| --- | --- | --- |
| Gateway identity & health | `agent/ready.go` treats `TraefikAppName` as the readiness gate for keepalived; `manager/reconcile.go` elects a gateway only if `isTraefikHealthy` sees reports with `AppID == "traefik"`; docs/networking.md and keepalived rely on the same readiness endpoint. | Introduce `GatewayProxyAppID = "caddy"`, update readiness checks to look at that ID, and change `resolveGateway` to consider the Caddy health report. Keep the same semantics for gateway election & keepalived by ensuring Caddy reports health in the same way Traefik does. |
| Config distribution & caching | Agents request `/agents/{id}/traefik-config` via `LinkReqAgentTraefik` (`common/link.go`, `manager/agent_link.go`); `agent/traefik.go` writes `/appdata/traefik/config/traefik.yml` only when the content changes to avoid reload storms. | Add `/agents/{id}/caddy-config` + `LinkReqAgentCaddy`, but keep `/traefik-config` as a temporary alias. Replace `TraefikSync` with `CaddySync` writing `/appdata/caddy/Caddyfile` (and `/data` for cert storage), keeping the same change-detection logic. |
| Routing model & domain mapping | `manager/traefik.go` generates YAML routers/services per running app: default subdomain = app name, override via `homectl-config.json.exposed_subdomains`, always emits both `{app}.example.com` and `{app}.internal.example.com`, and picks upstream IPs based on local vs remote node (Docker bridge `172.17.0.1` vs agent IP). | Build a deterministic Caddyfile generator returning site blocks. Each site gets two hostnames (public+internal) and a `reverse_proxy` to either `172.17.0.1` or the remote agent IP. Use placeholders such as `:80`/`:443` or global `servers` options for HTTP→HTTPS redir. Keep the exposed_subdomains override semantics identical. |
| HTTP(S) entrypoints & TLS | Traefik listens on entrypoints `web` and `websecure` (static config not in repo) and terminates TLS using Let's Encrypt; the manager just assumes this works when routing to hostnames. | Leverage Caddy automatic HTTPS with ACME. Configure a global `email` and `storage` directive inside the generated Caddyfile (or ship a static JSON). Ensure HTTP->HTTPS redirect is preserved. Persist `/appdata/caddy/data` so certs survive restarts. |
| Desired-state defaults & app identity | Manager default desired state sets `"traefik": true` (`manager/manager.go`), and e2e tests (`e2e/lifecycle_test.go`, `suite_test.go`) assert Traefik is running by default on some node. Agent runtime comment (`agent/runtime.go`) assumes Traefik consumes exposed ports. | Rename app assets (app ID, compose folder, fixtures) to `caddy`. Update manager defaults, desired-state responses, and runtime comments. Adjust e2e expectations to verify `caddy` is running on the gateway node and stopped elsewhere. |
| Docker assets & scripts | Compose fixtures live under `e2e/apps/traefik` and `testdata/docker/v2/traefik`; `scripts/deploy-manager-root.sh` creates `/etc/traefik/dynamic`. | Move/duplicate assets under `caddy` directories. Update scripts to provision `/etc/caddy` (or `/appdata/caddy`) and the new config/data mountpoints. |
| Documentation & operational guidance | docs/networking.md, docs/plans/caddy-migration.md, and `docs/plans/requirements/gateway.md` hardcode Traefik terminology, paths, and behavior. | Rewrite docs to describe Caddy (placement, config, TLS story, internal/public hostnames). Include upgrade guidance referencing compatibility endpoints. |

## Implementation Roadmap
### 1. Proxy identity & compatibility scaffolding
- Add `GatewayProxyAppID` constant (`"caddy"`) used throughout agent/manager logic; keep `TraefikAppName` as alias only where necessary for upgrade parity.
- Extend `common.LinkRequestType` with `LinkReqAgentCaddy` and implement corresponding routing in `manager/agent_link.go` while keeping `/traefik-config` functional until rollout completes.
- Rename logging fields (`traefikLog`, `traefikMgrLog`) or wrap them to avoid churn until code moves.

### 2. Health, readiness, and gateway election
- Update `ReadyServer.handleReady` and `checkTraefikReady` to become proxy-agnostic (`checkGatewayProxyReady`), referencing the new app ID.
- Change `manager.reconcile.resolveGateway` to call `isGatewayProxyHealthy` using the same heuristics (running + healthy report).
- Ensure agents still expose readiness quickly after switching proxies; add telemetry for when no healthy Caddy exists and define a bootstrap rule (e.g., deterministically pick first alive node and mark desired state to run Caddy there).

### 3. Manager↔agent config plumbing
- Replace `TraefikSync` with `CaddySync` (new file) that fetches `/caddy-config` and writes `/appdata/caddy/Caddyfile`. Preserve the cached write optimization and log noise controls.
- Update `manager/server.handleAgentRoutes` to serve `/caddy-config`; for backwards compatibility, have `/traefik-config` call the same generator but emit deprecation headers/logs.
- Modify the agent reconcile loop (`agent/agent.go`) to initialize and invoke `CaddySync` only when the node is a candidate gateway (to avoid pointless syncing on non-gateway nodes).

### 4. Caddy config generation
- Replace `manager/traefik.go` with `manager/caddy.go` and mirror the existing logic:
  - Iterate over alive agents + app reports; skip non-running apps.
  - Determine upstream IP via local vs remote comparison.
  - Respect `exposed_subdomains` overrides, sorted for stable output.
  - Emit deterministic site order for reproducible diffs.
- Embed Caddy-specific features: global options block (email, storage path), automatic HTTP→HTTPS redirect, optional headers (e.g., `encode gzip`).
- Add unit tests around `GenerateCaddyConfig` to cover default subdomain, overrides, and multi-node upstream selection.

### 5. Desired-state enforcement & runtime expectations
- In `manager.reconcile`, inject desired state overrides so that `caddy` is `running` on the elected gateway and `stopped` elsewhere, replacing older manual `is_global`/`DesiredRunning` assumptions.
- Update manager defaults, e2e fixtures, and `docker-compose` assets to start the `caddy` container with the right volumes (`/appdata/caddy/config` → `/etc/caddy/Caddyfile`, `/appdata/caddy/data` → `/data`).
- Adjust agent runtime comments & structures referencing Traefik-managed ports to reflect the new proxy.

### 6. Tests, fixtures, and tooling
- Rename `e2e/apps/traefik` to `e2e/apps/caddy` and update `docker-compose.yml` with the official Caddy image plus necessary flags/environment.
- Refresh `testdata/docker/v2/*` fixtures and parser tests (`docker/docker_test.go`) to expect the new app name and config paths.
- Ensure e2e tests assert that only the gateway node reports Caddy running, and that agents continue to fetch configs successfully.

### 7. Documentation & operational updates
- Revise docs (`docs/networking.md`, requirements, existing caddy plan) to describe the new proxy, configuration path, and TLS behavior.
- Update scripts such as `scripts/deploy-manager-root.sh` to lay down `/etc/caddy` directories instead of Traefik-specific ones.
- Produce an upgrade note covering compatibility endpoints (`/agents/{id}/traefik-config`) and any manual steps (copying certs, cleaning old data directories).

### 8. Rollout & cleanup
- Ship one release that supports both config endpoints so agents can upgrade safely.
- Once all gateways run Caddy, remove the Traefik compatibility layer (old constants, link request type, docs) and delete unused directories.
- Monitor gateway readiness + TLS issuance logs to confirm parity before deprecating Traefik-specific tooling.
