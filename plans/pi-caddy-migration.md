# PI Plan: Traefik → Caddy Migration

## Goal

Replace Traefik with Caddy as the built-in gateway proxy while preserving all current homectl gateway behaviors (routing, readiness, TLS, failover, and manager↔agent config sync).

This plan is based on `docs/plans/caddy-migration.md`, plus a repo-level audit of how Traefik is used today.

---

## Traefik Feature Inventory → Caddy Replacements

| Current Traefik usage (audited) | Where it exists now | Caddy replacement |
|---|---|---|
| **Gateway readiness depends on proxy health** (`/ready` returns healthy only if gateway + traefik running/healthy) | `agent/ready.go` (`TraefikAppName`, `checkTraefikReady`) | Replace with proxy-agnostic check using `GatewayProxyAppID = "caddy"`; keep same readiness semantics for keepalived. |
| **Gateway election keyed to Traefik health** (manual gateway + automatic fallback scan for healthy Traefik) | `manager/reconcile.go` (`isTraefikHealthy`, `resolveGateway`) | Rename to `isGatewayProxyHealthy`; check Caddy report instead. Add bootstrap election when no node yet reports healthy Caddy. |
| **Manager→agent config fetch channel** via link request type `agent_traefik_config` | `common/link.go`, `manager/agent_link.go`, `manager/client/client.go` | Add `agent_caddy_config`; keep `agent_traefik_config` temporarily as compatibility alias. |
| **HTTP endpoint for generated proxy config** (`/agents/{id}/traefik-config`) | `manager/server.go`, `manager/agent_link.go` | Add `/agents/{id}/caddy-config`; keep `/traefik-config` alias for one transition release. |
| **Agent-side config sync with write-on-diff** (avoid proxy reload churn/temporary 404s) | `agent/traefik.go`, `agent/agent.go` | Introduce `CaddySync` with same diff-aware write behavior; write Caddyfile only when changed. |
| **Generated dynamic routing model** per running app | `manager/traefik.go` (`GenerateTraefikConfig`) | Implement `GenerateCaddyConfig` producing deterministic Caddyfile site blocks. |
| **Routing target selection** local app via `172.17.0.1`, remote app via agent IP | `manager/traefik.go` | Keep identical backend target logic in Caddy `reverse_proxy`. |
| **Domain model**: both public and internal hostnames per app | `manager/traefik.go`, `docs/networking.md` | Keep both hostnames in each Caddy site block (`{app}.example.com`, `{app}.internal.example.com`). |
| **`exposed_subdomains` overrides** in `homectl-config.json` | `manager/traefik.go` + `HomectlConfig` | Preserve exact behavior: map subdomain → port; deterministic sort order in generated output. |
| **HTTP/HTTPS ingress ports and redirect behavior** | `testdata/docker/v2/traefik/docker-compose.yml` flags (`web`, `websecure`, redirect) | Let Caddy handle automatic HTTPS and HTTP→HTTPS redirect on :80/:443. |
| **ACME TLS with persisted cert storage** | `testdata/docker/v2/traefik/docker-compose.yml` (`/data/letsencrypt.json`) | Persist Caddy storage (`/data`, optional `/config`) under appdata to preserve cert/account state. |
| **DNS challenge provider/env-driven cert setup** (Cloudflare currently in compose env) | `testdata/docker/v2/traefik/docker-compose.yml`, `.env` usage | Use Caddy build/module strategy for DNS challenge (e.g., caddy-dns/cloudflare) if wildcard DNS challenge is still required; otherwise HTTP challenge for public hostnames. |
| **Traefik init container creating cert file** (`init-letsencrypt`) | `testdata/docker/v2/traefik/docker-compose.yml` | Remove; Caddy manages storage files/directories directly. |
| **Default desired-running includes traefik** | `manager/manager.go` (`runByDefault`) | Replace with `caddy` defaults and explicit gateway override (`running on gateway, stopped elsewhere`). |
| **E2E fixtures/tests assume app id `traefik`** | `e2e/apps/traefik`, `e2e/*_test.go`, `docker/docker_test.go` | Migrate fixtures/tests to `caddy`; keep one compatibility pass only if mixed-version rollout required. |
| **Operational bootstrap creates Traefik dirs** | `scripts/deploy-manager-root.sh` (`/etc/traefik/dynamic`) | Update scripts to create Caddy dirs/paths (`/etc/caddy` or `/appdata/caddy/...` depending on runtime model). |
| **Docs and architecture language are Traefik-specific** | `docs/networking.md`, `docs/plans/requirements/gateway.md` | Rewrite docs to Caddy terms while preserving gateway + keepalived behavior descriptions. |

---

## Migration Plan

### Phase 1 — Proxy identity + compatibility layer

1. Add canonical constants:
   - `GatewayProxyAppID = "caddy"`
   - Optional legacy alias constant for transition (`LegacyGatewayProxyAppID = "traefik"`).
2. Add link request type `LinkReqAgentCaddy` in `common/link.go`.
3. Update manager link dispatch (`manager/agent_link.go`) and client methods (`manager/client/client.go`) to support Caddy config fetch.
4. Add new HTTP route `/agents/{id}/caddy-config`; retain `/agents/{id}/traefik-config` as alias with deprecation logs.

### Phase 2 — Gateway health/election and desired-state enforcement

1. Replace Traefik-specific readiness check in `agent/ready.go` with proxy-agnostic check against Caddy runtime.
2. Replace `isTraefikHealthy`/Traefik checks in `manager/reconcile.go` with Caddy checks.
3. Add bootstrap behavior: if no healthy Caddy is found, pick deterministic alive node as provisional gateway.
4. Enforce desired proxy state in manager response generation (`manager/server.go` path for desired apps):
   - gateway node: `caddy => DesiredRunning`
   - non-gateway nodes: `caddy => DesiredStopped`

### Phase 3 — Config generation + sync

1. Implement `GenerateCaddyConfig(forNodeID)` in `manager/caddy.go` (or replace `manager/traefik.go`).
2. Preserve all routing semantics:
   - local target: `172.17.0.1:<port>`
   - remote target: `<agent-ip>:<port>`
   - dual hostname exposure (public + internal)
   - `exposed_subdomains` overrides.
3. Ensure deterministic output ordering for stable diffs.
4. Add `agent/caddy.go` sync writer to `APPDATA/caddy/Caddyfile` with write-on-diff behavior.

### Phase 4 — Runtime assets and TLS model

1. Replace compose assets under:
   - `e2e/apps/traefik` → `e2e/apps/caddy`
   - `testdata/docker/v2/traefik` → `testdata/docker/v2/caddy`
2. Define final Caddy container mounts:
   - config (`Caddyfile`)
   - persistent cert/state storage (`/data`, `/config` as needed).
3. Decide challenge mode:
   - HTTP challenge (simpler), or
   - DNS challenge with Cloudflare plugin if wildcard cert behavior must be preserved.

### Phase 5 — Tests

1. Unit tests:
   - `GenerateCaddyConfig` correctness and deterministic ordering.
   - readiness and gateway election logic with Caddy health.
2. E2E tests:
   - Caddy runs on elected gateway.
   - Caddy stopped on non-gateway nodes.
   - gateway failover still converges with keepalived readiness.
   - app traffic routing works local and remote.
3. Parser/fixture tests:
   - update docker parser tests to caddy fixture expectations.

### Phase 6 — Docs/scripts and cleanup

1. Update `docs/networking.md` and gateway requirements docs to Caddy terminology.
2. Update scripts (`scripts/deploy-manager-root.sh`) for Caddy paths.
3. After one release cycle, remove Traefik compatibility aliases (`agent_traefik_config`, `/traefik-config`, legacy constants).

---

## Risks and Mitigations

- **Startup deadlock (no healthy proxy yet)** → bootstrap gateway selection.
- **TLS issuance regressions** → preserve persistent storage, validate challenge mode in staging first.
- **Reload churn / transient 404s** → keep write-on-diff config sync behavior.
- **Mixed-version rollouts** → temporary dual route/request-type support.

---

## Definition of Done

- No production code path requires `traefik` identity.
- Gateway readiness/election are based on Caddy health.
- Desired state guarantees Caddy running only on gateway.
- Caddy config generation preserves all current routing semantics.
- E2E suite passes with Caddy fixtures.
- Docs and scripts reflect Caddy-based architecture.
