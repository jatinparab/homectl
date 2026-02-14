package manager

import (
	"context"
	"log"
	"time"

	"github.com/jatinparab/homectl/common"
)

const (
	reconcileInterval  = 30 * time.Second
	migrationCooldown  = 10 * time.Minute
)

// StartReconcileLoop starts the periodic reconciliation loop.
// It runs every reconcileInterval and can be stopped via context cancellation.
func (m *Manager) StartReconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(reconcileInterval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				log.Println("Reconcile loop stopped")
				return
			case <-ticker.C:
				m.reconcileOnce(time.Now())
			}
		}
	}()
	log.Println("Reconcile loop started")
}

// reconcileOnce runs one full reconciliation cycle with 5 sequential steps.
func (m *Manager) reconcileOnce(now time.Time) {
	alive := m.aliveAgents()
	if len(alive) == 0 {
		return
	}

	m.appRegistry.WithLock(func(apps map[common.AppID]*App) {
		handleDeadAgents(apps, alive)
		assignUnassignedApps(apps, alive)
		handleMigrations(apps, alive, now)
		rebalance(apps, alive)
	})
}

// aliveAgents returns a set of alive agent names from the lease manager.
func (m *Manager) aliveAgents() map[string]bool {
	agents := m.leaseManager.ListAgents()
	alive := make(map[string]bool, len(agents))
	for _, a := range agents {
		alive[a.Name] = true
	}
	return alive
}

// handleDeadAgents clears AssignedAgent for apps whose agent is no longer alive.
func handleDeadAgents(apps map[common.AppID]*App, alive map[string]bool) {
	for _, app := range apps {
		if app.AssignedAgent != nil && !alive[*app.AssignedAgent] {
			log.Printf("Agent %s dead, unassigning app %s", *app.AssignedAgent, app.ID)
			app.AssignedAgent = nil
		}
	}
}

// assignUnassignedApps assigns each unassigned running app to the least-loaded alive agent.
func assignUnassignedApps(apps map[common.AppID]*App, alive map[string]bool) {
	for _, app := range apps {
		if app.DesiredState != common.DesiredRunning {
			continue
		}
		if app.AssignedAgent != nil {
			continue
		}

		target := leastLoadedExcluding(apps, alive, app.QuarantinedOn)
		if target == "" {
			log.Printf("No available agent for app %s (all quarantined or no agents)", app.ID)
			continue
		}

		app.AssignedAgent = &target
		log.Printf("Assigned app %s to agent %s", app.ID, target)
	}
}

// handleMigrations migrates at most 1 quarantined app past its cooldown to a different agent.
func handleMigrations(apps map[common.AppID]*App, alive map[string]bool, now time.Time) {
	for _, app := range apps {
		if !app.Quarantined {
			continue
		}
		if app.AssignedAgent == nil {
			continue
		}
		if now.Sub(app.LastMigratedAt) < migrationCooldown {
			continue
		}

		oldAgent := *app.AssignedAgent
		excluded := append(app.QuarantinedOn, oldAgent)
		target := leastLoadedExcluding(apps, alive, excluded)
		if target == "" {
			continue
		}

		log.Printf("Migrating app %s from %s to %s (quarantined)", app.ID, oldAgent, target)
		app.QuarantinedOn = append(app.QuarantinedOn, oldAgent)
		app.AssignedAgent = &target
		app.LastMigratedAt = now
		app.MigrationCount++
		return // max 1 migration per cycle
	}
}

// rebalance moves at most 1 non-quarantined app from an overloaded agent to an underloaded one.
func rebalance(apps map[common.AppID]*App, alive map[string]bool) {
	// Count apps per agent
	loadMap := make(map[string]int)
	for name := range alive {
		loadMap[name] = 0
	}
	for _, app := range apps {
		if app.AssignedAgent != nil && alive[*app.AssignedAgent] {
			loadMap[*app.AssignedAgent]++
		}
	}

	// Find most and least loaded
	var maxAgent, minAgent string
	maxLoad, minLoad := -1, int(^uint(0)>>1)
	for agent, load := range loadMap {
		if load > maxLoad {
			maxLoad = load
			maxAgent = agent
		}
		if load < minLoad {
			minLoad = load
			minAgent = agent
		}
	}

	// Only rebalance if difference > 1
	if maxLoad-minLoad <= 1 {
		return
	}

	// Find a non-quarantined app on the overloaded agent to move
	for _, app := range apps {
		if app.AssignedAgent == nil || *app.AssignedAgent != maxAgent {
			continue
		}
		if app.Quarantined {
			continue
		}
		if app.DesiredState != common.DesiredRunning {
			continue
		}

		// Check minAgent is not in QuarantinedOn
		quarantined := false
		for _, q := range app.QuarantinedOn {
			if q == minAgent {
				quarantined = true
				break
			}
		}
		if quarantined {
			continue
		}

		log.Printf("Rebalancing app %s from %s (%d apps) to %s (%d apps)",
			app.ID, maxAgent, maxLoad, minAgent, minLoad)
		app.AssignedAgent = &minAgent
		return // max 1 rebalance per cycle
	}
}

// leastLoadedExcluding returns the alive agent with the fewest assigned apps,
// excluding agents in the excluded set.
func leastLoadedExcluding(apps map[common.AppID]*App, alive map[string]bool, excluded []string) string {
	excludeSet := make(map[string]bool, len(excluded))
	for _, e := range excluded {
		excludeSet[e] = true
	}

	// Count apps per alive agent
	loadMap := make(map[string]int)
	for name := range alive {
		if !excludeSet[name] {
			loadMap[name] = 0
		}
	}

	for _, app := range apps {
		if app.AssignedAgent != nil {
			if _, ok := loadMap[*app.AssignedAgent]; ok {
				loadMap[*app.AssignedAgent]++
			}
		}
	}

	// Find minimum
	best := ""
	bestLoad := int(^uint(0) >> 1)
	for agent, load := range loadMap {
		if load < bestLoad {
			bestLoad = load
			best = agent
		}
	}
	return best
}
