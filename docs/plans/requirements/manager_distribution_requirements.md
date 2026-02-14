# Manager Distribution Logic Requirements

> Distilled from the design discussion in `docs/plans/distribution_logic_plan.md`.
> Related docs: `docs/plans/plan.md` (agent architecture), `docs/plans/backup_plan.md` (backup mechanisms).

## 1. Manager Scope and Boundaries

### 1.1 Responsibilities

The manager is responsible for exactly four things:

| # | Responsibility | Description |
|---|----------------|-------------|
| 1 | Track agent liveness | Maintain lease-based liveness for all registered agents |
| 2 | Track observed app state | Ingest heartbeat reports and update app state |
| 3 | Decide placement | Assign unassigned apps to agents; unassign apps from dead agents |
| 4 | Decide migration | Move quarantined apps to different agents (slow, guarded) |

### 1.2 Exclusions

The manager must **never**:

- Talk to Docker directly
- Run health checks
- Retry app operations
- Restart containers
- Reason about execution phases (e.g., backup phases, restore phases)

All execution and observation is the agent's responsibility.

---

## 2. Data Model

### 2.1 Agent Record

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `AgentID` | Unique agent identifier |
| `LastHeartbeat` | `time.Time` | Timestamp of most recent heartbeat received |
| `LeaseExpiresAt` | `time.Time` | When the agent's lease expires (heartbeat + lease duration) |

### 2.2 App Record

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `AppID` | Unique app identifier |
| `DesiredState` | `DesiredState` | What the user wants (`running` / `stopped`) |
| `AssignedAgent` | `*AgentID` | Which agent this app is assigned to (nullable) |
| `ObservedState` | `ObservedState` | Last reported observed state from the agent |
| `Health` | `HealthStatus` | Last reported health status from the agent |
| `RestartCount` | `int` | Last reported restart count from the agent |
| `Quarantined` | `bool` | Whether the agent has quarantined this app |
| `LastSeen` | `time.Time` | Last time agent reported on this app |
| `LastMigratedAt` | `time.Time` | Timestamp of last migration (for cooldown enforcement) |
| `MigrationCount` | `int` | Total number of times this app has been migrated |
| `QuarantinedOn` | `[]AgentID` | Agents that have previously quarantined this app (append-only history) |

---

## 3. Heartbeat Ingestion

### 3.1 Purpose

Heartbeat ingestion is a **pure state update** operation. It records facts reported by agents. It makes no decisions and contains no branching logic.

### 3.2 Behavior

On receiving a heartbeat from agent `A` with app reports:

1. Look up or create the agent record for `A`
2. Set `LastHeartbeat = now`
3. Set `LeaseExpiresAt = now + leaseDuration`
4. For each app report in the heartbeat:
   - Look up the app by ID (skip if unknown)
   - Update: `ObservedState`, `Health`, `RestartCount`, `Quarantined`, `LastSeen`

### 3.3 Constraints

- REQ-HB-1: Heartbeat ingestion must not trigger any placement or migration decisions
- REQ-HB-2: Unknown app IDs in reports must be silently ignored
- REQ-HB-3: Unknown agent IDs must result in auto-registration (new agent record created)

---

## 4. Reconciliation Loop

### 4.1 Timing

- The reconciliation loop runs on a fixed interval (default: **30 seconds**)
- It is the sole driver of placement and migration decisions

### 4.2 Reconcile Cycle

Each reconciliation cycle executes these steps **in order**:

| Step | Function | Purpose |
|------|----------|---------|
| 1 | `aliveAgents(now)` | Determine which agents have valid leases |
| 2 | `handleDeadAgents(alive)` | Unassign apps from dead agents |
| 3 | `assignUnassignedApps(alive)` | Place apps that have no agent |
| 4 | `handleMigrations(alive, now)` | Migrate quarantined apps (max 1 per cycle) |
| 5 | `rebalance(alive)` | Move apps to balance load (max 1 per cycle) |

### 4.3 Constraints

- REQ-RC-1: Steps must execute sequentially within a single cycle
- REQ-RC-2: Each step is independent and does not depend on side effects of other steps within the same cycle
- REQ-RC-3: The reconcile loop must never block on external I/O

---

## 5. Agent Liveness

### 5.1 Liveness Determination

An agent is considered **alive** if and only if:

```
now < agent.LeaseExpiresAt
```

### 5.2 Constraints

- REQ-LV-1: Liveness is determined solely by lease expiration, not by health status or heartbeat recency
- REQ-LV-2: The alive agent set is computed once per reconcile cycle and reused across all steps

---

## 6. Dead Agent Handling

### 6.1 Behavior

For every app currently assigned to an agent **not** in the alive set:

- Set `AssignedAgent = nil`

This makes the app eligible for reassignment in the next step.

### 6.2 Constraints

- REQ-DA-1: Dead agent handling must only clear `AssignedAgent`; it must not modify any other app fields
- REQ-DA-2: No health or observed state logic is considered; only lease expiration matters
- REQ-DA-3: Apps unassigned from dead agents are not immediately reassigned in the same step; they become eligible for step 3

---

## 7. App Assignment (Placement)

### 7.1 Eligibility

An app is eligible for assignment when **all** of the following are true:

- `DesiredState == running`
- `AssignedAgent == nil`

### 7.2 Placement Strategy

Eligible apps are assigned to the **least-loaded** alive agent that is **not** in the app's `QuarantinedOn` list. Load is defined as the number of apps currently assigned to that agent.

### 7.3 Constraints

- REQ-AS-1: Apps with `DesiredState != running` must never be assigned
- REQ-AS-2: If no alive agents exist, apps remain unassigned
- REQ-AS-3: Assignment sets only `AssignedAgent`; it does not modify any other app fields
- REQ-AS-4: An app must never be placed on an agent in its `QuarantinedOn` list. If all alive agents are excluded, the app remains unassigned.

---

## 8. Migration Logic

### 8.1 Purpose

Migration moves an app from its current agent to a different agent when the app has been **quarantined** by the current agent. Quarantine indicates the agent has exhausted local recovery options.

### 8.2 Eligibility

An app is eligible for migration when **all** of the following are true:

- `Quarantined == true`
- `AssignedAgent != nil`
- Time since `LastMigratedAt` exceeds the migration cooldown (default: **10 minutes**)
- A different alive agent is available as a target

### 8.3 Quarantine History

When an app is migrated away from an agent due to quarantine, the current agent's ID is appended to `QuarantinedOn`. This list is append-only during normal operation and serves as an exclusion list for future placement.

### 8.4 Target Selection

The migration target must satisfy **all** of the following:

- Is a different agent from the current `AssignedAgent`
- Is **not** in the app's `QuarantinedOn` list
- Is alive

If multiple candidates remain, use any reasonable strategy (e.g., least-loaded). If no eligible candidate exists (all alive agents are in `QuarantinedOn`), the app remains on the current agent — no migration occurs.

### 8.5 Migration Action

When migrating app `X` from agent `A` to agent `B`:

1. Append `A` to `QuarantinedOn`
2. Set `AssignedAgent = B`
3. Set `LastMigratedAt = now`
4. Increment `MigrationCount`

### 8.6 Rate Limiting

- REQ-MG-1: **At most one app may be migrated per reconcile cycle.** After one migration, the function returns immediately.
- REQ-MG-2: The 10-minute cooldown per app prevents migration thrashing
- REQ-MG-3: Non-quarantined apps are never migrated by this step (rebalancing is separate)
- REQ-MG-4: An app must never be assigned to an agent in its `QuarantinedOn` list
- REQ-MG-5: If all alive agents are in `QuarantinedOn`, skip migration for that app rather than re-assigning to a known-bad agent

---

## 9. Rebalancing

### 9.1 Purpose

Rebalancing redistributes apps across agents to maintain roughly even load. It runs after all other reconcile steps.

### 9.2 Load Calculation

- Load per agent = count of apps with `AssignedAgent == agent.ID`
- Ideal load per agent = `ceil(totalApps / aliveAgentCount)`

### 9.3 Rebalance Action

1. Find an **overloaded** agent (load > ideal)
2. Find an **underloaded** agent (load < ideal)
3. If both exist: move **one non-quarantined app** from the overloaded agent to the underloaded agent

### 9.4 Constraints

- REQ-RB-1: **At most one app may be rebalanced per reconcile cycle**
- REQ-RB-2: Quarantined apps must not be rebalanced (they are handled by migration)
- REQ-RB-3: If no overloaded/underloaded pair exists, no action is taken
- REQ-RB-4: Rebalancing only changes `AssignedAgent`; no other fields are modified

---

## 10. Design Invariants

These invariants must hold across all manager behavior:

| ID | Invariant |
|----|-----------|
| INV-1 | The manager never reacts to single failures; it reasons over aggregated state |
| INV-2 | The manager never blocks on work or external I/O during reconciliation |
| INV-3 | The manager never trusts intent; it only trusts observed state from heartbeats |
| INV-4 | All decisions are reversible in the next reconcile cycle |
| INV-5 | At most one migration and one rebalance occur per reconcile cycle |
| INV-6 | The agent acts; the manager decides; sensors observe; heartbeats testify |

---

## 11. Implementation Status

Current state of the codebase relative to these requirements:

| Requirement Area | Status | Notes |
|-----------------|--------|-------|
| Agent liveness (leases) | Implemented | `manager/lease.go` — `LeaseManager` with TTL-based expiry and cleanup loop |
| Heartbeat ingestion | Partially implemented | `manager/lease.go:Heartbeat()` updates agent liveness and stores `AppReport`s, but there is no separate app registry; reports are stored on the agent record |
| App registry / data model | Not implemented | No `App` struct with `AssignedAgent`, `LastMigratedAt`, etc. Desired apps are hardcoded in `server.go:handleDesiredApps()` |
| Reconciliation loop | Not implemented | No `reconcileOnce()` or periodic reconcile ticker on the manager |
| Dead agent handling | Not implemented | Expired agents are deleted from the map (`lease.go:cleanupExpired`) but apps are not unassigned |
| App assignment (placement) | Not implemented | No least-loaded assignment logic |
| Migration logic | Not implemented | No quarantine-based migration with cooldown guards |
| Rebalancing | Not implemented | No load-based rebalancing |

### Key gaps to close

1. **App registry**: Introduce a manager-side `App` struct (per section 2.2) that tracks `AssignedAgent`, observed state from heartbeats, and migration metadata
2. **Dynamic desired apps**: Replace the hardcoded `handleDesiredApps()` with a lookup against the app registry filtered by `AssignedAgent`
3. **Manager reconcile loop**: Add a 30-second ticker that runs `reconcileOnce()` through all five steps
4. **Heartbeat → app state**: Wire heartbeat ingestion to update the app registry (not just the agent record)
