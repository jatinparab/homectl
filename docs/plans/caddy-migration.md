# Caddy Migration Plan (Traefik → Caddy)

## Goal

Replace Traefik with Caddy as the ingress implementation in homectl, and make it a **first-class system behavior**:

- The gateway node is always expected to run Caddy.
- Caddy is no longer treated as a user/global app that must be configured manually in app definitions.
- Certificate generation/renewal is handled correctly via Caddy’s native ACME flow.

---

## Scope

### In scope

1. Replace Traefik-specific control/config sync paths with Caddy equivalents.
2. Update gateway election/readiness checks from “traefik healthy” to “caddy healthy”.
3. Ensure desired-state generation enforces:
   - Caddy `running` on gateway node.
   - Caddy `stopped` on non-gateway nodes.
4. Update manager-generated reverse-proxy config format from Traefik dynamic YAML to Caddyfile.
5. Update e2e fixtures/tests to use `caddy` app ID and image.
6. Keep backward compatibility where useful (temporary alias endpoints/request types) for rolling upgrades.

### Out of scope (phase 1)

- Full migration tooling for existing user repos (auto-renaming app folders, preserving all custom Traefik flags).
- Non-HTTP protocols beyond current routing expectations.
- UI redesign beyond text/label changes required for consistency.

---

## Design Decisions

1. **Gateway proxy app identity**
   - Introduce a canonical constant: `GatewayProxyAppID = "caddy"`.
   - Replace hardcoded `"traefik"` checks with this constant in manager/agent gateway logic.

2. **First-class behavior vs global app behavior**
   - Do not depend on `is_global` semantics for Caddy.
   - Enforce desired state at manager response composition time:
     - Gateway node gets Caddy running.
     - Other nodes get Caddy stopped.

3. **Gateway election bootstrap**
   - Current logic assumes proxy is already running somewhere.
   - Add bootstrap behavior: if no healthy Caddy exists, pick a deterministic alive node as provisional gateway so Caddy can start there.

4. **Config generation model**
   - Replace `GenerateTraefikConfig` with `GenerateCaddyConfig`.
   - Output Caddyfile with:
     - one site block per routed hostname,
     - `reverse_proxy <ip>:<port>` targets,
     - global options needed for ACME email/logging,
     - deterministic ordering for stable diffs.

5. **Certificate handling**
   - Use Caddy automatic HTTPS/ACME for public domains.
   - Persist Caddy data directory on gateway nodes so cert state survives restarts.
   - Ensure runtime mount path supports Caddy storage (`/data` and/or `/config` depending on image setup).

---

## Implementation Plan

### Phase 1 — Core constants and naming migration

Files likely affected:
- `agent/ready.go`
- `agent/agent.go`
- `agent/traefik.go` (rename to caddy-oriented implementation)
- `manager/reconcile.go`
- `manager/server.go`
- `manager/agent_link.go`
- `manager/client/client.go`
- `common/link.go`
- `manager/manager.go`

Tasks:
1. Add new link request type for Caddy config (keep old Traefik one as alias during transition).
2. Introduce proxy app constants and update readiness checks.
3. Rename sync structs/functions (`TraefikSync` → `CaddySync`) and paths (`.../traefik/...` → `.../caddy/...`).
4. Update manager endpoint handler names and routes:
   - primary: `/agents/{id}/caddy-config`
   - optional compatibility: `/agents/{id}/traefik-config` delegating to caddy generator.

---

### Phase 2 — Gateway logic and desired-state enforcement

Files likely affected:
- `manager/reconcile.go`
- `manager/server.go`
- `manager/app.go` (if needed for helper behavior)

Tasks:
1. Replace `isTraefikHealthy` with `isGatewayProxyHealthy` (checking Caddy app report).
2. Add bootstrap gateway selection when no healthy Caddy exists.
3. In desired apps response creation, force gateway-proxy desired state:
   - if `agentID == gatewayNode`: `caddy => running`
   - else: `caddy => stopped`
4. Ensure old gateway transitions stop Caddy cleanly by virtue of new desired-state response.

---

### Phase 3 — Caddy config generation

Files likely affected:
- `manager/traefik.go` (replace or split into new `manager/caddy.go`)
- `agent/*sync*.go` (writer path/content)

Tasks:
1. Implement `GenerateCaddyConfig(forNodeID string)`.
2. Preserve routing semantics already used today:
   - local target via Docker bridge gateway (`172.17.0.1`).
   - remote target via agent IP.
   - `exposed_subdomains` support.
3. Sort all keys/routes for deterministic output.
4. Write generated Caddyfile to appdata path expected by Caddy container.

---

### Phase 4 — Runtime app fixtures and tests

Files likely affected:
- `e2e/apps/traefik/*` → `e2e/apps/caddy/*`
- `e2e/*_test.go`
- `e2e/suite_test.go`
- `testdata/docker/v2/traefik/*` (migrate/add caddy fixture)
- `docker/docker_test.go`

Tasks:
1. Rename e2e app ID constants from `traefik` to `caddy`.
2. Update lifecycle tests to assert Caddy running and healthy.
3. Update parser tests with Caddy compose fixture and expectations.
4. Keep at most one gateway invariant tests unchanged (logic still valid).

---

### Phase 5 — Docs and operational scripts

Files likely affected:
- `docs/networking.md`
- `scripts/deploy-manager-root.sh`
- any README/ops docs mentioning Traefik paths

Tasks:
1. Replace Traefik terminology with Caddy where architecture-specific.
2. Update path examples (`/appdata/caddy/...`).
3. Update deployment scripts creating Traefik directories to Caddy equivalents.

---

## Backward Compatibility Strategy

For one transition release:
1. Accept both manager link request types:
   - `agent_caddy_config` (new)
   - `agent_traefik_config` (legacy alias)
2. Accept both HTTP routes:
   - `/agents/{id}/caddy-config` (new)
   - `/agents/{id}/traefik-config` (legacy alias)
3. Keep logs clear when legacy path is used (deprecation warning).

This avoids breaking mixed-version manager/agent upgrades.

---

## Risks and Mitigations

1. **Gateway deadlock at startup** (no node marked gateway because Caddy not yet running)
   - Mitigation: provisional gateway bootstrap selection.

2. **Certificate issuance failures** (DNS/firewall/misconfigured domain)
   - Mitigation: surface Caddy health and logs clearly in app reports/log APIs.

3. **Route churn causing reload interruptions**
   - Mitigation: deterministic config ordering + write-on-diff behavior retained.

4. **Rolling upgrade incompatibility**
   - Mitigation: temporary dual request-type/route support.

---

## Verification Plan

1. **Unit tests**
   - Caddy config generation for local/remote routing.
   - Deterministic output ordering.
   - Gateway election with Caddy health and bootstrap path.

2. **E2E tests**
   - Caddy eventually running on gateway.
   - Non-gateway nodes keep Caddy stopped.
   - Gateway failover moves Caddy target correctly.
   - App traffic continues through new gateway after failover.

3. **Manual smoke checks**
   - Verify cert files created/persisted in Caddy data dir.
   - Verify readiness endpoint reflects gateway + Caddy health.

---

## Rollout Sequence

1. Implement compatibility layer (new + legacy request paths).
2. Land Caddy config generation and gateway enforcement.
3. Migrate e2e fixtures/tests.
4. Update docs/scripts.
5. Remove legacy Traefik aliases in a later cleanup release.

---

## Definition of Done

- No production code path depends on Traefik app identity.
- Gateway election/readiness is based on Caddy health.
- Gateway node always receives desired running state for Caddy; non-gateways do not.
- Config sync and certificate flow operate via Caddy.
- E2E suite passes with Caddy fixtures.
- Networking docs reflect Caddy architecture.
