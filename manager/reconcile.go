package manager

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

var reconcileLog = slog.With("area", "reconcile")

const (
	reconcileInterval    = 30 * time.Second
	migrationCooldown    = 10 * time.Minute
	deadAgentGracePeriod = 3 * time.Minute
	defaultMaxRAMPercent = 80

	// evacuationRAMBuffer prevents oscillation around the RAM threshold.
	// Evacuation triggers only when MemoryPercent >= maxRAMPercent + buffer.
	// Targets are only accepted when safely under maxRAMPercent.
	evacuationRAMBuffer = 5.0
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
	protectedApps := m.appsProtectedFromRebalance()
	excludedNodes := m.buildExcludedNodes()
	metrics := m.aliveAgentMetrics(alive)
	maxRAMPercent := m.maxRAMPercent()

	var manualAssignmentChanged bool
	m.appRegistry.WithLock(func(apps map[common.AppID]*App) {
		before := countManuallyAssigned(apps)
		handleDeadAgents(apps, alive, lastSeenMap, now)
		manualAssignmentChanged = countManuallyAssigned(apps) != before
		assignUnassignedAppsWithMetrics(apps, alive, excludedNodes, metrics, maxRAMPercent)
		handleMigrationsWithMetrics(apps, alive, now, excludedNodes, metrics, maxRAMPercent)
		rebalanceWithMetrics(apps, alive, protectedApps, excludedNodes, metrics, maxRAMPercent)
	})
	if manualAssignmentChanged {
		m.saveManualAssignments()
	}

	m.resolveGateway()
	m.invalidateWorkAssignments(alive)
	m.reconcileRepoOps(now)
}

// resolveGateway picks one online agent with the gateway proxy running+healthy as the gateway node.
// It prefers candidates that also report adguard running+healthy. If a manual
// gateway is set and valid, uses it.
func (m *Manager) resolveGateway() {
	m.gatewayMu.Lock()
	defer m.gatewayMu.Unlock()

	// Check if manual gateway is set and still valid
	if m.manualGatewayNode != "" {
		if m.isGatewayProxyHealthy(m.manualGatewayNode) {
			if m.gatewayNode != m.manualGatewayNode {
				reconcileLog.Debug("Gateway: using manual selection", "node", m.manualGatewayNode)
			}
			m.gatewayNode = m.manualGatewayNode
			return
		}
		reconcileLog.Warn("Gateway: manual selection no longer valid, falling back to automatic", "node", m.manualGatewayNode)
	}

	selected := m.selectGatewayCandidate()
	if selected != "" {
		old := m.gatewayNode
		m.gatewayNode = selected
		if old == "" {
			reconcileLog.Info("Gateway: assigned", "node", selected)
		} else if old != selected {
			reconcileLog.Info("Gateway: transitioned", "from", old, "to", selected)
		}
		return
	}

	// No valid gateway found
	if m.gatewayNode != "" {
		reconcileLog.Warn("Gateway: no valid candidate, clearing", "was", m.gatewayNode)
		m.gatewayNode = ""
	}
}

func (m *Manager) selectGatewayCandidate() string {
	agents := m.leaseManager.ListAgents()
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })

	bestName := ""
	bestRank := 0
	for _, agent := range agents {
		rank := m.gatewayCandidateRank(agent.Name)
		if rank > bestRank {
			bestName = agent.Name
			bestRank = rank
		}
	}
	if bestRank == 0 {
		return ""
	}

	if m.gatewayNode != "" && m.gatewayNode != m.manualGatewayNode {
		currentRank := m.gatewayCandidateRank(m.gatewayNode)
		if currentRank == bestRank {
			return m.gatewayNode
		}
		if currentRank == 0 {
			reconcileLog.Warn("Gateway: node no longer valid, scanning for new gateway", "node", m.gatewayNode)
		}
	}

	return bestName
}

func (m *Manager) gatewayCandidateRank(agentName string) int {
	if !m.isGatewayProxyHealthy(agentName) {
		return 0
	}
	if m.isAgentAppHealthy(agentName, "adguard") {
		return 2
	}
	return 1
}

// isGatewayProxyHealthy checks if an agent reports the gateway proxy as running+healthy.
func (m *Manager) isGatewayProxyHealthy(agentName string) bool {
	return m.isAgentAppHealthy(agentName, common.GatewayProxyAppID)
}

func (m *Manager) isAgentAppHealthy(agentName string, appID common.AppID) bool {
	agent, ok := m.leaseManager.GetAgent(agentName)
	if !ok {
		return false
	}
	if time.Since(agent.LastSeen) >= 30*time.Second {
		return false
	}
	for _, report := range agent.AppReports {
		if report.AppID == appID && report.ObservedState == common.ObservedRunning && report.Health == common.HealthHealthy {
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

func (m *Manager) aliveAgentMetrics(alive map[string]bool) map[string]common.NodeMetrics {
	agents := m.leaseManager.ListAgents()
	metrics := make(map[string]common.NodeMetrics, len(alive))
	for _, agent := range agents {
		if alive[agent.Name] && agent.Metrics.MemoryTotal > 0 {
			metrics[agent.Name] = agent.Metrics
		}
	}
	return metrics
}

func (m *Manager) maxRAMPercent() float64 {
	raw := strings.TrimSpace(m.configValue(common.ConfigKeyMaxRAMPercent))
	if raw == "" {
		return defaultMaxRAMPercent
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		reconcileLog.Warn("Ignoring invalid max RAM percent", "value", raw)
		return defaultMaxRAMPercent
	}
	if value > 100 {
		return 100
	}
	return value
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
	appConfigs, err := m.LoadAppConfigs(nil, false)
	if err != nil {
		reconcileLog.Error("Failed to load app configs; excluded nodes unavailable",
			"err", err)
		appConfigs = make(map[string]HomectlConfig)
	}
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
	assignUnassignedAppsWithMetrics(apps, alive, excludedNodes, nil, 0)
}

func assignUnassignedAppsWithMetrics(apps map[common.AppID]*App, alive map[string]bool, excludedNodes map[common.AppID][]string, metrics map[string]common.NodeMetrics, maxRAMPercent float64) {
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
		target := leastLoadedExcludingWithMetrics(apps, alive, excluded, metrics, maxRAMPercent)
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
	handleMigrationsWithMetrics(apps, alive, now, excludedNodes, nil, 0)
}

func handleMigrationsWithMetrics(apps map[common.AppID]*App, alive map[string]bool, now time.Time, excludedNodes map[common.AppID][]string, metrics map[string]common.NodeMetrics, maxRAMPercent float64) {
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
		target := leastLoadedExcludingWithMetrics(apps, alive, excluded, metrics, maxRAMPercent)
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

// appsProtectedFromRebalance returns apps that must remain on their current
// agent during automatic balancing. Moving a stopped app in the middle of a
// backup or restore can make the destination restore stale data and prevents
// the original backup assignment from receiving completion evidence.
func (m *Manager) appsProtectedFromRebalance() map[common.AppID]bool {
	result := m.appsWithActiveConnections()
	for _, agent := range m.leaseManager.ListAgents() {
		for _, report := range agent.AppReports {
			if report.BackupBusy || report.PruneBusy || report.PostBackupPhase != "" ||
				report.Phase == common.PhaseBackupStopping ||
				report.Phase == common.PhaseBackupStopped ||
				report.Phase == common.PhaseBackingUp ||
				report.Phase == common.PhaseRestoring {
				result[report.AppID] = true
			}
		}
	}
	return result
}

// rebalance moves at most 1 non-quarantined, non-running app from an overloaded agent to an underloaded one.
// Only stopped apps are moved during automatic rebalancing to avoid disrupting running services.
func rebalance(apps map[common.AppID]*App, alive map[string]bool, protectedApps map[common.AppID]bool, excludedNodes map[common.AppID][]string) {
	rebalanceWithMetrics(apps, alive, protectedApps, excludedNodes, nil, 0)
}

func rebalanceWithMetrics(apps map[common.AppID]*App, alive map[string]bool, protectedApps map[common.AppID]bool, excludedNodes map[common.AppID][]string, metrics map[string]common.NodeMetrics, maxRAMPercent float64) {
	rebalanceApps(apps, alive, protectedApps, excludedNodes, false, metrics, maxRAMPercent)
}

// forceRebalance moves ALL eligible apps at once to achieve balance, including running apps.
// Used for user-triggered rebalancing via the UI.
func forceRebalance(apps map[common.AppID]*App, alive map[string]bool, excludedNodes map[common.AppID][]string) {
	forceRebalanceWithMetrics(apps, alive, excludedNodes, nil, 0)
}

func forceRebalanceWithMetrics(apps map[common.AppID]*App, alive map[string]bool, excludedNodes map[common.AppID][]string, metrics map[string]common.NodeMetrics, maxRAMPercent float64) {
	rebalanceApps(apps, alive, nil, excludedNodes, true, metrics, maxRAMPercent)
}

// rebalanceApps is the shared implementation for both automatic and forced rebalancing.
// When force=true, running apps can be moved and multiple moves happen in one pass.
// When force=false, only stopped (ObservedState != running) apps are moved, max 1 per cycle.
func rebalanceApps(apps map[common.AppID]*App, alive map[string]bool, protectedApps map[common.AppID]bool, excludedNodes map[common.AppID][]string, force bool, metrics map[string]common.NodeMetrics, maxRAMPercent float64) {
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

		// Find most and least loaded. Prefer moving away from nodes over the
		// configured RAM ceiling, then fall back to count balancing.
		var maxAgent, minAgent string
		maxLoad, minLoad := -1, int(^uint(0)>>1)
		for agent, load := range loadMap {
			if load > maxLoad || (load == maxLoad && (maxAgent == "" || agent < maxAgent)) {
				maxLoad = load
				maxAgent = agent
			}
			if load < minLoad || (load == minLoad && (minAgent == "" || agent < minAgent)) {
				minLoad = load
				minAgent = agent
			}
		}

		overLimitAgent := mostMemoryConstrainedAgent(loadMap, metrics, maxRAMPercent)
		if overLimitAgent != "" {
			maxAgent = overLimitAgent
			maxLoad = loadMap[maxAgent]
			minAgent = leastLoadedExcludingWithMetrics(apps, alive, []string{maxAgent}, metrics, maxRAMPercent)
			if minAgent == "" || nodeOverRAMLimit(metrics[minAgent], maxRAMPercent) {
				return
			}
			minLoad = loadMap[minAgent]
		} else if maxLoad-minLoad <= 1 {
			return
		} else {
			target := leastLoadedExcludingWithMetrics(apps, alive, []string{maxAgent}, metrics, maxRAMPercent)
			if target != "" {
				minAgent = target
				minLoad = loadMap[minAgent]
			}
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
				if protectedApps[app.ID] {
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
		metrics := m.aliveAgentMetrics(alive)
		forceRebalanceWithMetrics(apps, alive, excludedNodes, metrics, m.maxRAMPercent())
	})

	reconcileLog.Info("Force rebalance completed")
}

// leastLoadedExcluding returns the alive agent with the fewest assigned apps,
// excluding agents in the excluded set.
func leastLoadedExcluding(apps map[common.AppID]*App, alive map[string]bool, excluded []string) string {
	return leastLoadedExcludingWithMetrics(apps, alive, excluded, nil, 0)
}

func leastLoadedExcludingWithMetrics(apps map[common.AppID]*App, alive map[string]bool, excluded []string, metrics map[string]common.NodeMetrics, maxRAMPercent float64) string {
	excludeSet := make(map[string]bool, len(excluded))
	for _, e := range excluded {
		excludeSet[e] = true
	}

	loadMap := appLoadMap(apps, alive, excludeSet)
	names := make([]string, 0, len(loadMap))
	for name := range loadMap {
		names = append(names, name)
	}
	sort.Strings(names)

	best := ""
	for _, name := range names {
		if best == "" || betterScheduleTarget(name, best, loadMap, metrics, maxRAMPercent) {
			best = name
		}
	}
	return best
}

func appLoadMap(apps map[common.AppID]*App, alive map[string]bool, excludeSet map[string]bool) map[string]int {
	loadMap := make(map[string]int)
	for name := range alive {
		if !excludeSet[name] {
			loadMap[name] = 0
		}
	}
	for _, app := range apps {
		if app.IsGlobal || app.AssignedAgent == nil {
			continue
		}
		if _, ok := loadMap[*app.AssignedAgent]; ok {
			loadMap[*app.AssignedAgent]++
		}
	}
	return loadMap
}

func betterScheduleTarget(candidate, current string, loadMap map[string]int, metrics map[string]common.NodeMetrics, maxRAMPercent float64) bool {
	candidateMetrics, candidateHasMetrics := usableNodeMetrics(metrics[candidate])
	currentMetrics, currentHasMetrics := usableNodeMetrics(metrics[current])
	candidateOver := candidateHasMetrics && nodeOverRAMLimit(candidateMetrics, maxRAMPercent)
	currentOver := currentHasMetrics && nodeOverRAMLimit(currentMetrics, maxRAMPercent)
	if candidateOver != currentOver {
		return !candidateOver
	}
	// Prefer lower app count for stable distribution
	if loadMap[candidate] != loadMap[current] {
		return loadMap[candidate] < loadMap[current]
	}
	// Tiebreak by free RAM (second-order to avoid bouncing on RAM fluctuation)
	if candidateHasMetrics && currentHasMetrics {
		candidateFree := freeMemory(candidateMetrics)
		currentFree := freeMemory(currentMetrics)
		if candidateFree != currentFree {
			return candidateFree > currentFree
		}
	}
	if candidateHasMetrics != currentHasMetrics {
		return candidateHasMetrics
	}
	return candidate < current
}

func usableNodeMetrics(metrics common.NodeMetrics) (common.NodeMetrics, bool) {
	return metrics, metrics.MemoryTotal > 0
}

func freeMemory(metrics common.NodeMetrics) uint64 {
	if metrics.MemoryUsed >= metrics.MemoryTotal {
		return 0
	}
	return metrics.MemoryTotal - metrics.MemoryUsed
}

func nodeOverRAMLimit(metrics common.NodeMetrics, maxRAMPercent float64) bool {
	return maxRAMPercent > 0 && metrics.MemoryTotal > 0 && metrics.MemoryPercent >= maxRAMPercent
}

// evacuationThreshold returns the effective threshold for triggering RAM-based
// evacuation. A buffer prevents oscillation when MemoryPercent naturally
// fluctuates around the limit.
func evacuationThreshold(maxRAMPercent float64) float64 {
	return maxRAMPercent + evacuationRAMBuffer
}

func mostMemoryConstrainedAgent(loadMap map[string]int, metrics map[string]common.NodeMetrics, maxRAMPercent float64) string {
	if maxRAMPercent <= 0 {
		return ""
	}
	evacThreshold := evacuationThreshold(maxRAMPercent)
	best := ""
	bestPercent := 0.0
	for name, load := range loadMap {
		if load == 0 {
			continue
		}
		metric := metrics[name]
		if !nodeOverRAMLimit(metric, evacThreshold) {
			continue
		}
		if best == "" || metric.MemoryPercent > bestPercent || (metric.MemoryPercent == bestPercent && name < best) {
			best = name
			bestPercent = metric.MemoryPercent
		}
	}
	return best
}
