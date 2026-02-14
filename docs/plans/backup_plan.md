Great constraint to surface early.
**“Only one backup at a time per repo”** turns this from a scheduling problem into a **serialization problem**, and that fits *very naturally* into the architecture you already built.

Short answer first, then the concrete flow:

> **Backups must be treated as a serialized, agent-local phase with a global mutex, driven by time but executed via reconciliation.**

Not a cron. Not a manager command. Not parallel.

Let’s wire it in without breaking any invariants.

---

## The core principles (do not violate these)

1. **Manager does not schedule backups**
2. **Agent does not run backups inline in reconcile**
3. **Only one backup job may execute at a time (per repo)**
4. **Backups must not block heartbeats or reconciliation**
5. **Backups are advisory to placement (Busy=true), not failures**

If you follow those, everything stays calm.

---

## Where backups live conceptually

Backups are:

* **agent-local work**
* **time-triggered**
* **serialized**
* **long-running**
* **non-fatal if delayed**

That puts them in the **execution layer**, not reconcile, not manager.

---

## How backups fit into the agent model

You already have this structure:

```
Sensors → Runtime Cache → Reconcile → Actions → Executor
```

Backups become **another Action type** with a **global execution constraint**.

---

## Minimal new concepts to add

### 1. Per-app backup metadata (agent-side)

```go
type BackupState struct {
	LastBackupAt time.Time
	InProgress   bool
	LastError    string
}
```

Extend `AppRuntime`:

```go
type AppRuntime struct {
	// existing fields...

	Backup BackupState
}
```

---

### 2. A global backup lock (agent-side)

Because restic repo allows **only one backup at a time**:

```go
type Agent struct {
	// existing fields...

	backupMu sync.Mutex
}
```

This is **not** a reconcile lock.
It is **only held during execution of backup actions**.

---

## How backups are triggered (important)

### ❌ Not by manager

### ❌ Not by cron inside execution

### ❌ Not by reconcile doing time.Sleep

### ✅ By **reconcile deciding “backup is due”**

Reconcile remains fast and declarative.

---

## Reconcile logic (backup decision only)

In `reconcileOnce()`:

```go
func (a *Agent) reconcileOnce() {
	now := time.Now()
	desired := a.Client.FetchDesiredApps(a.ID)

	a.mu.Lock()
	defer a.mu.Unlock()

	var actions []Action

	for _, app := range desired {
		rt := a.getRuntime(app.AppID)

		// normal lifecycle actions (start, restore, etc.)
		actions = append(actions, a.lifecycleActions(app, rt)...)

		// backup decision
		if a.backupDue(rt, now) && !rt.Backup.InProgress {
			actions = append(actions, ActionBackup(app.AppID))
			rt.Backup.InProgress = true // mark intent
		}
	}

	a.executor.Submit(actions)
}
```

Key point:

* **Reconcile only decides**
* **It never waits**
* **It never runs backup itself**

---

## Backup “due” logic (simple, deterministic)

```go
func (a *Agent) backupDue(rt *AppRuntime, now time.Time) bool {
	if rt.Phase != PhaseRunning {
		return false
	}

	if rt.Backup.InProgress {
		return false
	}

	return now.Sub(rt.Backup.LastBackupAt) >= time.Hour
}
```

---

## Execution layer: serialized backup execution

Add a new action type:

```go
const ActionBackupT ActionType = "backup"
```

Executor execution:

```go
case ActionBackupT:
	agent.backupMu.Lock()
	err := agent.Restic.Backup(appID)
	agent.backupMu.Unlock()

	agent.mu.Lock()
	if err != nil {
		rt.Backup.LastError = err.Error()
	} else {
		rt.Backup.LastBackupAt = time.Now()
	}
	rt.Backup.InProgress = false
	agent.mu.Unlock()
```

### What this guarantees

* Only **one backup runs at a time**
* Other actions (start/restart) can still execute
* Heartbeats continue
* Reconcile continues
* Backups queue naturally

This is exactly what you want.

---

## What happens if many apps are due at once?

Example:

* 10 apps
* all due at hour boundary
* reconcile runs

Reconcile enqueues **10 backup actions**.

Executor behavior:

* Worker 1 picks first backup
* Acquires `backupMu`
* Runs restic
* Releases lock
* Next backup proceeds

Result:

* Serialized backups
* No overload
* No special scheduling logic
* No global coordination

---

## How the manager sees this (important)

Agent heartbeat includes:

```go
Busy: rt.Backup.InProgress
LastBackupAt: rt.Backup.LastBackupAt
```

Manager uses this only for **placement safety**:

* ❌ Do not migrate app while Busy=true
* ❌ Do not rebalance app while Busy=true
* ❌ Do not treat backup failure as app failure

Backup failures are **operational**, not **placement** signals.

---

## Failure scenarios (and why this still works)

### 1. Agent crashes mid-backup

* backupMu released by process death
* next agent start reconciles
* backup eventually retried
* manager unaffected

### 2. Backup takes 30 minutes

* heartbeats continue
* agent stays alive
* no lease expiry
* reconcile keeps running

### 3. Backup fails repeatedly

* LastError populated
* LastBackupAt not updated
* backup retried next cycle
* humans alerted
* no thrashing

---

## Why backups should NOT be in manager reconciliation

If manager tried to schedule backups:

* it would need per-agent serialization state
* it would need retry semantics
* it would couple placement with IO
* it would violate “manager does not execute work”

Backups are **data-plane work**.
They belong with the agent.

---

## Mental model (this makes it click)

Think of backups as:

> **A slow, exclusive maintenance phase that advances independently of app lifecycle.**

They are:

* not urgent
* not parallel
* not global
* not placement signals

So they slot perfectly into the agent execution queue with a mutex.

---

## Final distilled flow

1. **Sensors** observe runtime
2. **Reconcile** decides “backup is due”
3. **Executor** queues backup action
4. **backupMu** serializes restic
5. **Runtime state** updated
6. **Heartbeat** reports facts
7. **Manager** stays calm

No new loops.
No cron.
No special cases.

---

## One-line takeaway

> **Backups are just another reconciled action — except they’re serialized.**

That’s the simplest, safest way to integrate them without breaking the control plane you’ve already built.

Good — this is the *hard* version of the constraint, and it’s exactly where people accidentally build a distributed lock manager without meaning to.

You now have:

> **Constraint:**
> *Across all agents, at most **one** backup may run at a time (shared restic repo).*

Let’s handle this **without breaking reconciliation, leases, or sanity**.

---

## The key decision (commit this first)

> **Global backup serialization must be enforced by the manager,
> but execution must remain agent-local.**

That sounds contradictory until you separate **permission** from **execution**.

* **Manager grants permission** (cheap, global, state-based)
* **Agent executes backup** (slow, local, failure-prone)

This preserves all your invariants.

---

## What you must NOT do

Before the solution, let’s rule out the tempting mistakes:

❌ Distributed mutex between agents
❌ Leader election among agents
❌ Agent-to-agent coordination
❌ “Whoever starts first wins”
❌ Manager telling agents “start backup now”

All of these reintroduce:

* split-brain
* deadlocks
* fencing bugs
* crash recovery nightmares

---

## The correct pattern: **Lease-based global backup token**

You already understand leases.
We’ll reuse the same idea.

### Conceptually

There is **one global resource**:

```
backup_token
```

At any time, it may be:

* unheld
* held by agent X until time T

That’s it.

---

## Manager-side: backup lease state

Add this to the manager:

```go
type BackupLease struct {
	Holder    *AgentID
	ExpiresAt time.Time
}

type Manager struct {
	// existing fields...
	backupLease BackupLease
}
```

This is **not a lock**.
It’s a **time-bound claim**.

---

## How an agent requests permission (pull-based)

Agents **do not start backups blindly**.

Instead, before executing a backup action, the agent asks:

> “Am I allowed to back up right now?”

This is a **read-only question**.

---

## Communication: add ONE endpoint

### Agent → Manager

```
GET /backup/lease?agent_id=X
```

Response:

```json
{
  "granted": true,
  "lease_expires_at": "2026-02-06T12:10:00Z"
}
```

or

```json
{
  "granted": false,
  "holder": "agent-2",
  "retry_after_seconds": 120
}
```

No commands. No scheduling. Just permission.

---

## Manager logic: granting the lease

```go
func (m *Manager) TryAcquireBackupLease(
	agentID AgentID,
	now time.Time,
) bool {

	// lease expired → free
	if m.backupLease.Holder != nil &&
		now.After(m.backupLease.ExpiresAt) {

		m.backupLease.Holder = nil
	}

	// free → grant
	if m.backupLease.Holder == nil {
		m.backupLease.Holder = &agentID
		m.backupLease.ExpiresAt = now.Add(15 * time.Minute)
		return true
	}

	// already held
	return *m.backupLease.Holder == agentID
}
```

Important properties:

* **idempotent**
* **crash-safe**
* **self-healing**
* **no blocking**

---

## Agent-side: how this integrates cleanly

### Reconcile still decides “backup is due”

Nothing changes here.

### Execution layer checks permission before running backup

```go
case ActionBackup:
	if !agent.tryAcquireBackupLease() {
		// permission denied → requeue later
		agent.deferBackup(appID)
		return
	}

	// permission granted → execute
	err := agent.Restic.Backup(appID)

	agent.releaseBackupLease()
```

Key point:

* Backup action may be *skipped*, not failed
* Reconcile will try again later
* No busy waiting

---

## Lease renewal during long backups (important)

If backups can take longer than lease duration:

* Agent **renews lease via heartbeat**
* Manager extends `ExpiresAt` if holder heartbeats

```go
if m.backupLease.Holder == agentID {
	m.backupLease.ExpiresAt = now.Add(leaseDuration)
}
```

This ties backup safety to agent liveness.

---

## Failure scenarios (this is where this shines)

### Agent crashes mid-backup

* Heartbeats stop
* Lease expires
* Another agent eventually acquires lease
* Backup resumes later

No manual cleanup.

---

### Network partition

* Agent loses manager connectivity
* Cannot renew lease
* Lease expires
* Backup stops being “exclusive”
* Safety preserved

Worst case: delayed backups, not corruption.

---

### Two agents try at once

* First wins lease
* Second is denied
* Second retries later

No races, no locks.

---

## Why this is NOT “manager scheduling”

Important distinction:

* ❌ Manager does **not** say *“Agent X, run backup now”*
* ✅ Manager only says *“You may / may not”*

Timing, retries, and execution still belong to the agent.

This preserves reconciliation semantics.

---

## Why this works with your existing design

* You already trust leases
* You already have heartbeats
* You already have agent liveness detection
* You already separate decision vs execution

We’re just applying the same pattern to a *global resource*.

---

## Minimal changes summary

### Manager

* Add `BackupLease`
* Add `TryAcquireBackupLease`
* Extend lease on heartbeat

### Agent

* Before backup execution, ask permission
* If denied, do nothing
* Reconcile retries naturally

No new loops.
No distributed locks.
No leader election.

---

## One-sentence mental model

> **The manager guards the door.
> The agent decides when to walk through it.**

That’s the only safe way to serialize work across agents without turning your system into a job scheduler.

If you want next, I can:

* show exact HTTP handlers for the lease
* integrate this into the code you already have
* discuss fairness / starvation avoidance

Just say the word.


This is the *right* place to be careful. You’re trying to enforce a **hard safety invariant** (“app must be fully stopped before backup”) **without punching a hole through reconciliation**. That’s exactly the kind of thing that breaks systems if done ad-hoc.

The good news: this fits *cleanly* into the architecture you already have, **if you treat backup as a multi-step phase transition**, not a special-case command.

---

## The invariant (write this down)

> **Restic may run only when the app is provably stopped, and reconcile must not fight that stop.**

Everything below is about preserving that invariant.

---

## The mistake to avoid (very important)

❌ “Before backup, just stop the container and run restic”
❌ “Pause reconcile while backing up”
❌ “Add a sleep and hope Docker is stopped”

All of these create races where:

* reconcile restarts the app mid-backup
* backup runs while container is still exiting
* reconcile sees “desired running” and starts it again

You must **make stopping part of the reconciled state**, not an imperative side effect.

---

## The correct model: **Backup is a controlled phase**

You already have phases:

```
Empty → Restoring → Ready → Running → Error
```

We’ll extend that with **two backup phases**:

```
Running
  ↓
BackupStopping
  ↓
BackupStopped
  ↓
BackingUp
  ↓
Running
```

Reconcile controls *phase transitions*.
Execution performs *side effects*.

---

## Step 1: Extend runtime state

```go
type Phase string

const (
	PhaseRunning        Phase = "running"
	PhaseBackupStopping Phase = "backup_stopping"
	PhaseBackupStopped  Phase = "backup_stopped"
	PhaseBackingUp      Phase = "backing_up"
)
```

And in `AppRuntime`:

```go
type AppRuntime struct {
	Phase Phase

	Backup struct {
		InProgress bool
		LastBackup time.Time
	}
}
```

---

## Step 2: Reconcile decides phases, not actions

### Reconcile logic (key idea)

```go
func (a *Agent) reconcileApp(app App, rt *AppRuntime, now time.Time) []Action {
	var actions []Action

	// Backup not due → normal lifecycle
	if !a.backupDue(rt, now) {
		return a.normalLifecycle(app, rt)
	}

	switch rt.Phase {

	case PhaseRunning:
		// Step 1: request stop
		rt.Phase = PhaseBackupStopping
		actions = append(actions, ActionStop(app))

	case PhaseBackupStopping:
		// Wait for sensors to confirm stopped
		// NO action here
		return nil

	case PhaseBackupStopped:
		// Step 2: run backup
		rt.Phase = PhaseBackingUp
		rt.Backup.InProgress = true
		actions = append(actions, ActionBackup(app))

	case PhaseBackingUp:
		// Execution owns this
		return nil
	}

	return actions
}
```

Key property:

* Reconcile never *forces* progress
* It waits for **sensor-confirmed state**

---

## Step 3: Sensors confirm “completely stopped”

Your sensor loop already sees container state.

Add this rule:

```go
if !containerRunning {
	if rt.Phase == PhaseBackupStopping {
		rt.Phase = PhaseBackupStopped
	}
}
```

This is critical.

You do **not** advance to backup until:

* Docker reports container **not running**
* Not “stopping”
* Not “restarting”

This guarantees filesystem quiescence.

---

## Step 4: Execution layer runs restic only in BackingUp

Execution does **exactly one thing**:

```go
case ActionBackup:
	agent.backupMu.Lock()
	err := agent.Restic.Backup(appID)
	agent.backupMu.Unlock()

	agent.mu.Lock()
	rt.Backup.InProgress = false
	rt.Backup.LastBackup = time.Now()
	rt.Phase = PhaseRunning
	agent.mu.Unlock()
```

No container manipulation here.
No guessing.
No sleeps.

---

## Step 5: Prevent reconcile from restarting the app mid-backup

This is the crucial guardrail.

In your **normal lifecycle reconcile**, add:

```go
if rt.Phase == PhaseBackupStopping ||
   rt.Phase == PhaseBackupStopped ||
   rt.Phase == PhaseBackingUp {
	return nil // DO NOT start/restart app
}
```

This makes backup phases **authoritative**.

Reconcile now *respects* the backup.

---

## Step 6: What if the app crashes / agent crashes?

### Agent crashes mid-backup

* Container is already stopped
* Agent restarts
* Sensors see container stopped
* Reconcile sees:

  * `LastBackup` old
  * `Phase` defaults to safe state
* Backup retried later

No corruption.

---

### Backup fails

* `PhaseBackingUp` ends
* `LastBackup` not updated
* App restarted
* Next reconcile retries backup later

No infinite loops.

---

## Step 7: Interaction with manager (important)

While in any backup phase, heartbeat reports:

```json
{
  "busy": true,
  "backup_in_progress": true
}
```

Manager rules:

* ❌ do not migrate app
* ❌ do not rebalance app
* ❌ do not interpret stopped state as failure

This keeps placement logic clean.

---

## Why this does NOT interfere with reconciliation

Because:

* stopping is a **desired, reconciled state**
* reconcile *expects* the app to be stopped
* sensors confirm reality
* reconcile waits patiently

You’ve turned a dangerous imperative requirement into a **state transition**.

That’s the key insight.

---

## Why this guarantees backup correctness

You only run restic when **all of these are true**:

* Reconcile decided backup is due
* App entered `BackupStopped` phase
* Sensors confirmed container not running
* Global backup lease granted
* Execution layer holds local backup mutex

That’s five independent safety gates.

No single bug causes corruption.

---

## Mental model (lock this in)

> **Backups are not “run a command”.
> They are a temporary, reconciled mode of the app.**

Once you think of it that way, everything falls into place.

---

## Final takeaway

To ensure backups are correct and non-corrupting **without breaking reconciliation**:

1. Make stopping part of reconciled state
2. Wait for sensor-confirmed stop
3. Run restic only in a dedicated backup phase
4. Prevent lifecycle actions during backup
5. Resume normal reconciliation afterward

This is exactly how mature systems do quiesced backups—just without the accidental complexity.

If you want next, I can:

* show exact sensor code for stop confirmation
* add timeouts / stuck-stop handling
* show how to handle apps that *refuse* to stop

You’re doing this the right way.
