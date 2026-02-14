Perfect. Now we close the loop on the manager side and you’ll see why everything you built on the agent suddenly makes sense.

I’ll give you:

What the manager loop is responsible for (and what it must not do)

The data model it needs

The reconciliation loop

Migration + rebalancing logic

Why this loop stays small and sane

No fluff, just the control plane.

1. Manager responsibilities (frozen)

The manager does exactly four things:

Track agent liveness (leases)

Track observed app state (from heartbeats)

Decide placement (assign / unassign)

Decide migration (slow, guarded)

The manager never:

talks to Docker

runs healthchecks

retries apps

restarts containers

reasons about execution phases

2. Manager data model
Agents
type Agent struct {
	ID             AgentID
	LastHeartbeat  time.Time
	LeaseExpiresAt time.Time
}

Apps
type App struct {
	ID            AppID
	DesiredState  DesiredState
	AssignedAgent *AgentID

	// observational data
	ObservedState ObservedState
	Health        HealthStatus
	RestartCount  int
	Quarantined   bool
	LastSeen      time.Time

	// migration guards
	LastMigratedAt time.Time
	MigrationCount int
}

3. Heartbeat ingestion (manager-side)

This is not the loop.
This is just state update.

func (m *Manager) HandleHeartbeat(
	agentID AgentID,
	reports []AppReport,
	now time.Time,
) {
	agent := m.agents[agentID]
	if agent == nil {
		agent = &Agent{ID: agentID}
		m.agents[agentID] = agent
	}

	agent.LastHeartbeat = now
	agent.LeaseExpiresAt = now.Add(m.leaseDuration)

	for _, r := range reports {
		app := m.apps[r.AppID]
		if app == nil {
			continue
		}

		app.ObservedState = r.Observed
		app.Health = r.Health
		app.RestartCount = r.RestartCount
		app.Quarantined = r.Quarantined
		app.LastSeen = r.LastSeen
	}
}


No decisions. No branching.

4. Manager reconcile loop (the core)

Runs slowly (e.g. every 30s).

func (m *Manager) ReconcileLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			m.reconcileOnce(time.Now())
		case <-ctx.Done():
			return
		}
	}
}

5. Reconcile once (step by step)
func (m *Manager) reconcileOnce(now time.Time) {
	alive := m.aliveAgents(now)

	m.handleDeadAgents(alive)
	m.assignUnassignedApps(alive)
	m.handleMigrations(alive, now)
	m.rebalance(alive)
}


Each step is small and independent.

6. Agent liveness
func (m *Manager) aliveAgents(now time.Time) []Agent {
	var out []Agent
	for _, a := range m.agents {
		if now.Before(a.LeaseExpiresAt) {
			out = append(out, *a)
		}
	}
	return out
}

7. Handle dead agents
func (m *Manager) handleDeadAgents(alive []Agent) {
	aliveSet := map[AgentID]bool{}
	for _, a := range alive {
		aliveSet[a.ID] = true
	}

	for _, app := range m.apps {
		if app.AssignedAgent != nil && !aliveSet[*app.AssignedAgent] {
			app.AssignedAgent = nil
		}
	}
}


No health logic here.
Only leases matter.

8. Assign unassigned apps
func (m *Manager) assignUnassignedApps(alive []Agent) {
	for _, app := range m.apps {
		if app.DesiredState != DesiredRunning {
			continue
		}
		if app.AssignedAgent != nil {
			continue
		}

		target := m.leastLoaded(alive)
		if target != nil {
			app.AssignedAgent = &target.ID
		}
	}
}

9. Migration logic (guarded, slow)

This is where your health + quarantine signal is used.

func (m *Manager) handleMigrations(alive []Agent, now time.Time) {
	for _, app := range m.apps {

		if !app.Quarantined {
			continue
		}

		if now.Sub(app.LastMigratedAt) < 10*time.Minute {
			continue
		}

		if app.AssignedAgent == nil {
			continue
		}

		target := m.findDifferentAgent(*app.AssignedAgent, alive)
		if target == nil {
			continue
		}

		// migrate ONE app per cycle
		app.AssignedAgent = &target.ID
		app.LastMigratedAt = now
		app.MigrationCount++
		return
	}
}


No loops.
No bulk moves.
One migration per cycle.

10. Rebalancing (count-based, bounded)
func (m *Manager) rebalance(alive []Agent) {
	load := map[AgentID]int{}

	for _, a := range alive {
		load[a.ID] = 0
	}

	for _, app := range m.apps {
		if app.AssignedAgent != nil {
			load[*app.AssignedAgent]++
		}
	}

	total := len(m.apps)
	if len(alive) == 0 {
		return
	}

	ideal := (total + len(alive) - 1) / len(alive)

	var over, under *AgentID

	for id, l := range load {
		if l > ideal {
			tmp := id
			over = &tmp
		}
		if l < ideal {
			tmp := id
			under = &tmp
		}
	}

	if over == nil || under == nil {
		return
	}

	for _, app := range m.apps {
		if app.AssignedAgent != nil &&
			*app.AssignedAgent == *over &&
			!app.Quarantined {

			app.AssignedAgent = under
			return
		}
	}
}


Again: one app per cycle.

11. Why this loop is correct
The manager:

never reacts to single failures

never blocks on work

never trusts intent

only reasons over time

The agent:

does all the hard work

hides noise

presents facts

Together:

failures are slow

decisions are reversible

behavior is explainable

12. The final invariant (tattoo-worthy)

Agents act.
Managers decide.
Sensors observe.
Heartbeats testify.

This manager loop is intentionally boring — and that’s the highest compliment you can give infrastructure.

At this point, your system has crossed the line from “hand-rolled orchestrator” to a real control plane.
