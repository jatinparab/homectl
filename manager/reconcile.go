package manager

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jatinparab/homectl/common"
)

var reconcileLog = slog.With("area", "reconcile")

const (
	reconcileInterval    = 30 * time.Second
	migrationCooldown    = 10 * time.Minute
	deadAgentGracePeriod = 3 * time.Minute
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
				reconcileLog.Info("Reconcile loop stopped")
				return
			case <-ticker.C:
				m.reconcileOnce(time.Now())
			}
		}
	}()
	reconcileLog.Info("Reconcile loop started")
}

// reconcileOnce runs one full reconciliation cycle with 5 sequential steps.
func (m *Manager) reconcileOnce(now time.Time) {
	alive := m.aliveAgents()
	if len(alive) == 0 {
		return
	}

	lastSeenMap := m.buildLastSeenMap()
	activeApps := m.appsWithActiveConnections()
	excludedNodes := m.buildExcludedNodes()

	var manualAssignmentChanged bool
	m.appRegistry.WithLock(func(apps map[common.AppID]*App) {
		before := countManuallyAssigned(apps)
		handleDeadAgents(apps, alive, lastSeenMap, now)
		manualAssignmentChanged = countManuallyAssigned(apps) != before
		assignUnassignedApps(apps, alive, excludedNodes)
		handleMigrations(apps, alive, now, excludedNodes)
		rebalance(apps, alive, activeApps, excludedNodes)
	})
	if manualAssignmentChanged {
		m.saveManualAssignments()
	}

	m.resolveGateway()
	m.invalidateWorkAssignments(alive)
	m.reconcileRepoOps(now)
}

// resolveGateway picks one online agent with traefik running+healthy as the gateway node.
// If a manual gateway is set and valid, uses it. Otherwise it scans for a new one automatically.
func (m *Manager) resolveGateway() {
	m.gatewayMu.Lock()
	oldGateway := m.gatewayNode
	defer func() {
		newGateway := m.gatewayNode
		m.gatewayMu.Unlock()
		if oldGateway != newGateway {
			m.restartNewtOnGatewayChange(oldGateway, newGateway)
		}
	}()

	// Check if manual gateway is set and still valid
	if m.manualGatewayNode != "" {
		if m.isTraefikHealthy(m.manualGatewayNode) {
			if m.gatewayNode != m.manualGatewayNode {
				reconcileLog.Debug("Gateway: using manual selection", "node", m.manualGatewayNode)
			}
			m.gatewayNode = m.manualGatewayNode
			return
		}
		reconcileLog.Warn("Gateway: manual selection no longer valid, falling back to automatic", "node", m.manualGatewayNode)
	}

	// Check if current gateway is still valid
	if m.gatewayNode != "" && m.gatewayNode != m.manualGatewayNode {
		if m.isTraefikHealthy(m.gatewayNode) {
			return
		}
		reconcileLog.Warn("Gateway: node no longer valid, scanning for new gateway", "node", m.gatewayNode)
	}

	// Scan all online agents for one with traefik running+healthy
	agents := m.leaseManager.ListAgents()
	for _, agent := range agents {
		if m.isTraefikHealthy(agent.Name) {
			old := m.gatewayNode
			m.gatewayNode = agent.Name
			if old == "" {
				reconcileLog.Info("Gateway: assigned", "node", agent.Name)
			} else if old != agent.Name {
				reconcileLog.Info("Gateway: transitioned", "from", old, "to", agent.Name)
			}
			return
		}
	}

	// No valid gateway found
	if m.gatewayNode != "" {
		reconcileLog.Warn("Gateway: no valid candidate, clearing", "was", m.gatewayNode)
		m.gatewayNode = ""
	}
}

// restartNewtOnGatewayChange restarts newt when gateway ownership changes.
func (m *Manager) restartNewtOnGatewayChange(oldGateway, newGateway string) {
	reconcileLog.Info("Gateway: changed, restarting newt", "from", oldGateway, "to", newGateway)

	now := time.Now()
	targetSet := make(map[string]bool)
	for _, agent := range m.leaseManager.ListAgents() {
		if now.Sub(agent.LastSeen) >= 30*time.Second {
			continue
		}
		for _, report := range agent.AppReports {
			if report.AppID == "newt" {
				targetSet[agent.Name] = true
				break
			}
		}
	}

	// Fallback: if newt isn't currently reported, try restarting on the new gateway.
	if len(targetSet) == 0 && newGateway != "" {
		targetSet[newGateway] = true
	}

	targets := make([]string, 0, len(targetSet))
	for name := range targetSet {
		targets = append(targets, name)
	}
	sort.Strings(targets)

	for _, node := range targets {
		if err := m.restartAppOnNode("newt", node); err != nil {
			reconcileLog.Error("Gateway: failed restarting newt", "node", node, "err", err)
			continue
		}
		reconcileLog.Info("Gateway: restarted newt", "node", node)
	}
}

func (m *Manager) restartAppOnNode(appID common.AppID, nodeName string) error {
	agentCfg := m.config.GetAgentConfig(nodeName)
	if agentCfg == nil {
		return fmt.Errorf("node %s not found in config", nodeName)
	}

	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) >= 30*time.Second {
		return fmt.Errorf("node %s offline", nodeName)
	}

	status, body, err := m.requestAgentUnary(context.Background(), nodeName, common.LinkReqRestartApp, common.RestartAppPayload{AppID: string(appID)})
	if err != nil {
		return err
	}

	if status < 200 || status >= 300 {
		return fmt.Errorf("agent returned HTTP %d: %s", status, string(body))
	}

	return nil
}

// isTraefikHealthy checks if an agent reports traefik as running+healthy.
func (m *Manager) isTraefikHealthy(agentName string) bool {
	agent, ok := m.leaseManager.GetAgent(agentName)
	if !ok {
		return false
	}
	if time.Since(agent.LastSeen) >= 30*time.Second {
		return false
	}
	for _, report := range agent.AppReports {
		if report.AppID == "traefik" && report.ObservedState == common.ObservedRunning && report.Health == common.HealthHealthy {
			return true
		}
	}
	return false
}

// aliveAgents returns a set of agent names whose last heartbeat is within leaseTTL.
func (m *Manager) aliveAgents() map[string]bool {
	agents := m.leaseManager.ListAgents()
	ttl := m.leaseManager.LeaseTTL()
	now := time.Now()
	alive := make(map[string]bool, len(agents))
	for _, a := range agents {
		if now.Sub(a.LastSeen) <= ttl {
			alive[a.Name] = true
		}
	}
	return alive
}

// buildLastSeenMap returns a map of agent name → LastSeen for all known agents
// (including recently-offline ones kept by the extended staleAgentTTL).
func (m *Manager) buildLastSeenMap() map[string]time.Time {
	agents := m.leaseManager.ListAgents()
	result := make(map[string]time.Time, len(agents))
	for _, a := range agents {
		result[a.Name] = a.LastSeen
	}
	return result
}

// buildExcludedNodes loads exclude_nodes from each app's homectl-config.json.
func (m *Manager) buildExcludedNodes() map[common.AppID][]string {
	appConfigs := m.loadAppConfigs()
	result := make(map[common.AppID][]string)
	for name, cfg := range appConfigs {
		if len(cfg.ExcludeNodes) > 0 {
			result[common.AppID(name)] = cfg.ExcludeNodes
		}
	}
	return result
}

func countManuallyAssigned(apps map[common.AppID]*App) int {
	n := 0
	for _, app := range apps {
		if app.ManuallyAssigned {
			n++
		}
	}
	return n
}

// handleDeadAgents clears AssignedAgent for apps whose agent is no longer alive,
// but only after the deadAgentGracePeriod has elapsed since the agent's last heartbeat.
// This prevents unnecessary app reshuffling during rolling deploys.
func handleDeadAgents(apps map[common.AppID]*App, alive map[string]bool, lastSeenMap map[string]time.Time, now time.Time) {
	for _, app := range apps {
		if app.AssignedAgent == nil || alive[*app.AssignedAgent] {
			continue
		}
		agent := *app.AssignedAgent
		if lastSeen, ok := lastSeenMap[agent]; ok && now.Sub(lastSeen) < deadAgentGracePeriod {
			reconcileLog.Debug("Agent offline within grace period", "agent", agent, "app", app.ID, "offline_for", now.Sub(lastSeen).Round(time.Second))
			continue
		}
		reconcileLog.Info("Agent dead past grace period, unassigning app", "agent", agent, "app", app.ID)
		app.AssignedAgent = nil
		app.ManuallyAssigned = false // dead node clears manual assignment
	}
}

// assignUnassignedApps assigns each unassigned running app to the least-loaded alive agent.
func assignUnassignedApps(apps map[common.AppID]*App, alive map[string]bool, excludedNodes map[common.AppID][]string) {
	for _, app := range apps {
		if app.IsGlobal {
			continue // global apps run on all nodes, no assignment needed
		}
		if app.DesiredState != common.DesiredRunning {
			continue
		}
		if app.AssignedAgent != nil {
			continue
		}

		excluded := append(app.QuarantinedOn, excludedNodes[app.ID]...)
		target := leastLoadedExcluding(apps, alive, excluded)
		if target == "" {
			reconcileLog.Warn("No available agent for app", "app", app.ID)
			continue
		}

		app.AssignedAgent = &target
		reconcileLog.Info("Assigned app to agent", "app", app.ID, "agent", target)
	}
}

// handleMigrations migrates at most 1 quarantined app past its cooldown to a different agent.
func handleMigrations(apps map[common.AppID]*App, alive map[string]bool, now time.Time, excludedNodes map[common.AppID][]string) {
	for _, app := range apps {
		if app.IsGlobal {
			continue
		}
		if app.ManuallyAssigned {
			continue // respect manual placement
		}
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
		excluded = append(excluded, excludedNodes[app.ID]...)
		target := leastLoadedExcluding(apps, alive, excluded)
		if target == "" {
			continue
		}

		reconcileLog.Info("Migrating app from quarantined agent", "app", app.ID, "from", oldAgent, "to", target)
		seen := false
		for _, node := range app.QuarantinedOn {
			if node == oldAgent {
				seen = true
				break
			}
		}
		if !seen {
			app.QuarantinedOn = append(app.QuarantinedOn, oldAgent)
		}
		app.AssignedAgent = &target
		app.Quarantined = false // clear so the new agent gets a fresh start
		app.LastMigratedAt = now
		app.MigrationCount++
		return // max 1 migration per cycle
	}
}

// appsWithActiveConnections returns the set of app IDs that have active connections
// on any agent, based on the latest heartbeat reports.
func (m *Manager) appsWithActiveConnections() map[common.AppID]bool {
	result := make(map[common.AppID]bool)
	for _, agent := range m.leaseManager.ListAgents() {
		for _, report := range agent.AppReports {
			if report.ActiveConnections {
				result[report.AppID] = true
			}
		}
	}
	return result
}

// rebalance moves at most 1 non-quarantined, non-running app from an overloaded agent to an underloaded one.
// Only stopped apps are moved during automatic rebalancing to avoid disrupting running services.
func rebalance(apps map[common.AppID]*App, alive map[string]bool, activeApps map[common.AppID]bool, excludedNodes map[common.AppID][]string) {
	rebalanceApps(apps, alive, activeApps, excludedNodes, false)
}

// forceRebalance moves ALL eligible apps at once to achieve balance, including running apps.
// Used for user-triggered rebalancing via the UI.
func forceRebalance(apps map[common.AppID]*App, alive map[string]bool, excludedNodes map[common.AppID][]string) {
	rebalanceApps(apps, alive, nil, excludedNodes, true)
}

// rebalanceApps is the shared implementation for both automatic and forced rebalancing.
// When force=true, running apps can be moved and multiple moves happen in one pass.
// When force=false, only stopped (ObservedState != running) apps are moved, max 1 per cycle.
func rebalanceApps(apps map[common.AppID]*App, alive map[string]bool, activeApps map[common.AppID]bool, excludedNodes map[common.AppID][]string, force bool) {
	for {
		// Count apps per agent (exclude global apps from load counting)
		loadMap := make(map[string]int)
		for name := range alive {
			loadMap[name] = 0
		}
		for _, app := range apps {
			if app.IsGlobal {
				continue
			}
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

		// Find a non-quarantined, non-global app on the overloaded agent to move
		moved := false
		for _, app := range apps {
			if app.IsGlobal {
				continue
			}
			if app.AssignedAgent == nil || *app.AssignedAgent != maxAgent {
				continue
			}
			if app.ManuallyAssigned {
				continue // respect manual placement
			}
			if app.Quarantined {
				continue
			}
			if app.DesiredState != common.DesiredRunning {
				continue
			}
			if !force {
				// Automatic rebalance: skip running apps and apps with active connections
				if app.ObservedState == common.ObservedRunning {
					continue
				}
				if activeApps[app.ID] {
					continue
				}
			}

			// Check minAgent is not in QuarantinedOn or ExcludeNodes
			excluded := false
			for _, q := range app.QuarantinedOn {
				if q == minAgent {
					excluded = true
					break
				}
			}
			if !excluded {
				for _, n := range excludedNodes[app.ID] {
					if n == minAgent {
						excluded = true
						break
					}
				}
			}
			if excluded {
				continue
			}

			reconcileLog.Info("Rebalancing app", "app", app.ID, "from", maxAgent, "from_load", maxLoad, "to", minAgent, "to_load", minLoad)
			app.AssignedAgent = &minAgent
			moved = true
			break // move one app per inner loop iteration
		}

		if !moved || !force {
			return // no more moves possible, or non-force mode (max 1)
		}
		// force mode: loop again to check if more rebalancing is needed
	}
}

// ForceRebalance triggers an immediate full rebalance, moving all apps (including running)
// to achieve optimal distribution. Called from the API.
func (m *Manager) ForceRebalance() {
	excludedNodes := m.buildExcludedNodes()

	m.appRegistry.WithLock(func(apps map[common.AppID]*App) {
		alive := m.aliveAgents()
		forceRebalance(apps, alive, excludedNodes)
	})

	reconcileLog.Info("Force rebalance completed")
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
		if app.IsGlobal {
			continue
		}
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
