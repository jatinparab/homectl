package manager

import (
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func strPtr(s string) *string { return &s }

func TestHandleDeadAgents(t *testing.T) {
	now := time.Now()
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", AssignedAgent: strPtr("alive-node")},
		"app2": {ID: "app2", AssignedAgent: strPtr("dead-node")},
		"app3": {ID: "app3", AssignedAgent: nil},
	}
	alive := map[string]bool{"alive-node": true}
	lastSeen := map[string]time.Time{
		"alive-node": now,
		"dead-node":  now.Add(-10 * time.Minute), // well past grace period
	}

	handleDeadAgents(apps, alive, lastSeen, now)

	if apps["app1"].AssignedAgent == nil || *apps["app1"].AssignedAgent != "alive-node" {
		t.Error("app1 should still be assigned to alive-node")
	}
	if apps["app2"].AssignedAgent != nil {
		t.Error("app2 should be unassigned (dead-node is dead)")
	}
	if apps["app3"].AssignedAgent != nil {
		t.Error("app3 should remain unassigned")
	}
}

func TestHandleDeadAgents_OnlyClearsAssignment(t *testing.T) {
	now := time.Now()
	apps := map[common.AppID]*App{
		"app1": {
			ID:            "app1",
			AssignedAgent: strPtr("dead-node"),
			DesiredState:  common.DesiredRunning,
			ObservedState: common.ObservedRunning,
			Health:        common.HealthHealthy,
			RestartCount:  5,
			QuarantinedOn: []string{"other-node"},
		},
	}
	alive := map[string]bool{}
	lastSeen := map[string]time.Time{
		"dead-node": now.Add(-10 * time.Minute), // well past grace period
	}

	handleDeadAgents(apps, alive, lastSeen, now)

	app := apps["app1"]
	if app.AssignedAgent != nil {
		t.Error("AssignedAgent should be cleared")
	}
	if app.DesiredState != common.DesiredRunning {
		t.Error("DesiredState should be preserved")
	}
	if app.ObservedState != common.ObservedRunning {
		t.Error("ObservedState should be preserved")
	}
	if app.RestartCount != 5 {
		t.Error("RestartCount should be preserved")
	}
	if len(app.QuarantinedOn) != 1 {
		t.Error("QuarantinedOn should be preserved")
	}
}

func TestHandleDeadAgents_GracePeriod(t *testing.T) {
	now := time.Now()
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", AssignedAgent: strPtr("restarting-node")},
		"app2": {ID: "app2", AssignedAgent: strPtr("long-dead-node")},
	}
	alive := map[string]bool{"other-node": true}
	lastSeen := map[string]time.Time{
		"restarting-node": now.Add(-1 * time.Minute),  // within 3m grace period
		"long-dead-node":  now.Add(-10 * time.Minute), // past grace period
	}

	handleDeadAgents(apps, alive, lastSeen, now)

	// restarting-node is within grace period — app1 should stay assigned
	if apps["app1"].AssignedAgent == nil || *apps["app1"].AssignedAgent != "restarting-node" {
		t.Error("app1 should remain assigned to restarting-node (within grace period)")
	}
	// long-dead-node is past grace period — app2 should be unassigned
	if apps["app2"].AssignedAgent != nil {
		t.Error("app2 should be unassigned (long-dead-node past grace period)")
	}
}

func TestHandleDeadAgents_UnknownAgent(t *testing.T) {
	now := time.Now()
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", AssignedAgent: strPtr("unknown-node")},
	}
	alive := map[string]bool{}
	lastSeen := map[string]time.Time{} // unknown-node not in lastSeenMap

	handleDeadAgents(apps, alive, lastSeen, now)

	// unknown-node has no lastSeen entry — should be unassigned immediately
	if apps["app1"].AssignedAgent != nil {
		t.Error("app1 should be unassigned (unknown-node not in lastSeenMap)")
	}
}

func TestAssignUnassignedApps_LeastLoaded(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app2": {ID: "app2", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app3": {ID: "app3", DesiredState: common.DesiredRunning, AssignedAgent: nil},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	assignUnassignedApps(apps, alive, nil)

	if apps["app3"].AssignedAgent == nil {
		t.Fatal("app3 should be assigned")
	}
	if *apps["app3"].AssignedAgent != "node2" {
		t.Errorf("app3 should be assigned to node2 (least loaded), got %s", *apps["app3"].AssignedAgent)
	}
}

func TestAssignUnassignedApps_RespectsQuarantinedOn(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {
			ID:            "app1",
			DesiredState:  common.DesiredRunning,
			AssignedAgent: nil,
			QuarantinedOn: []string{"node1"},
		},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	assignUnassignedApps(apps, alive, nil)

	if apps["app1"].AssignedAgent == nil {
		t.Fatal("app1 should be assigned")
	}
	if *apps["app1"].AssignedAgent != "node2" {
		t.Errorf("app1 should be assigned to node2 (node1 is quarantined), got %s", *apps["app1"].AssignedAgent)
	}
}

func TestAssignUnassignedApps_SkipsNonRunning(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", DesiredState: common.DesiredStopped, AssignedAgent: nil},
	}
	alive := map[string]bool{"node1": true}

	assignUnassignedApps(apps, alive, nil)

	if apps["app1"].AssignedAgent != nil {
		t.Error("stopped app should not be assigned")
	}
}

func TestAssignUnassignedApps_AllQuarantined(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {
			ID:            "app1",
			DesiredState:  common.DesiredRunning,
			AssignedAgent: nil,
			QuarantinedOn: []string{"node1", "node2"},
		},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	assignUnassignedApps(apps, alive, nil)

	if apps["app1"].AssignedAgent != nil {
		t.Error("app should remain unassigned when all agents quarantined")
	}
}

func TestHandleMigrations_QuarantinedPastCooldown(t *testing.T) {
	now := time.Now()
	oldTime := now.Add(-11 * time.Minute) // past 10-min cooldown

	apps := map[common.AppID]*App{
		"app1": {
			ID:             "app1",
			DesiredState:   common.DesiredRunning,
			AssignedAgent:  strPtr("node1"),
			Quarantined:    true,
			LastMigratedAt: oldTime,
		},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	handleMigrations(apps, alive, now, nil)

	if apps["app1"].AssignedAgent == nil || *apps["app1"].AssignedAgent != "node2" {
		t.Error("app1 should be migrated to node2")
	}
	if len(apps["app1"].QuarantinedOn) != 1 || apps["app1"].QuarantinedOn[0] != "node1" {
		t.Error("node1 should be added to QuarantinedOn")
	}
	if apps["app1"].MigrationCount != 1 {
		t.Errorf("expected MigrationCount 1, got %d", apps["app1"].MigrationCount)
	}
	if !apps["app1"].LastMigratedAt.Equal(now) {
		t.Error("LastMigratedAt should be updated")
	}
}

func TestHandleMigrations_WithinCooldown(t *testing.T) {
	now := time.Now()
	recentTime := now.Add(-5 * time.Minute) // within 10-min cooldown

	apps := map[common.AppID]*App{
		"app1": {
			ID:             "app1",
			DesiredState:   common.DesiredRunning,
			AssignedAgent:  strPtr("node1"),
			Quarantined:    true,
			LastMigratedAt: recentTime,
		},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	handleMigrations(apps, alive, now, nil)

	if *apps["app1"].AssignedAgent != "node1" {
		t.Error("app should not be migrated within cooldown")
	}
}

func TestHandleMigrations_DoesNotDuplicateQuarantinedOn(t *testing.T) {
	now := time.Now()
	oldTime := now.Add(-11 * time.Minute)

	apps := map[common.AppID]*App{
		"app1": {
			ID:             "app1",
			DesiredState:   common.DesiredRunning,
			AssignedAgent:  strPtr("node1"),
			Quarantined:    true,
			LastMigratedAt: oldTime,
			QuarantinedOn:  []string{"node1"},
		},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	handleMigrations(apps, alive, now, nil)

	if got := apps["app1"].QuarantinedOn; len(got) != 1 || got[0] != "node1" {
		t.Fatalf("expected quarantined list to remain deduped, got %#v", got)
	}
}

func TestHandleMigrations_MaxOnePerCycle(t *testing.T) {
	now := time.Now()
	oldTime := now.Add(-11 * time.Minute)

	apps := map[common.AppID]*App{
		"app1": {
			ID: "app1", DesiredState: common.DesiredRunning,
			AssignedAgent: strPtr("node1"), Quarantined: true,
			LastMigratedAt: oldTime,
		},
		"app2": {
			ID: "app2", DesiredState: common.DesiredRunning,
			AssignedAgent: strPtr("node1"), Quarantined: true,
			LastMigratedAt: oldTime,
		},
	}
	alive := map[string]bool{"node1": true, "node2": true, "node3": true}

	handleMigrations(apps, alive, now, nil)

	migrated := 0
	for _, app := range apps {
		if app.AssignedAgent != nil && *app.AssignedAgent != "node1" {
			migrated++
		}
	}
	if migrated != 1 {
		t.Errorf("expected exactly 1 migration per cycle, got %d", migrated)
	}
}

func TestRebalance_OverloadedToUnderloaded(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app2": {ID: "app2", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app3": {ID: "app3", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	rebalance(apps, alive, nil, nil)

	// One app should have moved to node2
	node1Count, node2Count := 0, 0
	for _, app := range apps {
		if app.AssignedAgent != nil {
			switch *app.AssignedAgent {
			case "node1":
				node1Count++
			case "node2":
				node2Count++
			}
		}
	}
	if node1Count != 2 || node2Count != 1 {
		t.Errorf("expected 2/1 split, got node1=%d node2=%d", node1Count, node2Count)
	}
}

func TestRebalance_SkipsQuarantinedApps(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1"), Quarantined: true},
		"app2": {ID: "app2", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1"), Quarantined: true},
		"app3": {ID: "app3", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1"), Quarantined: true},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	rebalance(apps, alive, nil, nil)

	// No moves: all are quarantined
	for _, app := range apps {
		if *app.AssignedAgent != "node1" {
			t.Error("quarantined apps should not be rebalanced")
		}
	}
}

func TestRebalance_MaxOnePerCycle(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app2": {ID: "app2", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app3": {ID: "app3", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app4": {ID: "app4", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	rebalance(apps, alive, nil, nil)

	node2Count := 0
	for _, app := range apps {
		if app.AssignedAgent != nil && *app.AssignedAgent == "node2" {
			node2Count++
		}
	}
	if node2Count != 1 {
		t.Errorf("expected exactly 1 rebalance move, got %d", node2Count)
	}
}

func TestRebalance_BalancedNoMove(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app2": {ID: "app2", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node2")},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	rebalance(apps, alive, nil, nil)

	if *apps["app1"].AssignedAgent != "node1" || *apps["app2"].AssignedAgent != "node2" {
		t.Error("already balanced apps should not be moved")
	}
}

func TestLeastLoadedExcluding(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", AssignedAgent: strPtr("node1")},
		"app2": {ID: "app2", AssignedAgent: strPtr("node1")},
		"app3": {ID: "app3", AssignedAgent: strPtr("node2")},
	}
	alive := map[string]bool{"node1": true, "node2": true, "node3": true}

	// node3 has 0 apps, node2 has 1, node1 has 2
	result := leastLoadedExcluding(apps, alive, nil)
	if result != "node3" {
		t.Errorf("expected node3 (0 apps), got %s", result)
	}

	// Exclude node3 -> node2 should be chosen
	result = leastLoadedExcluding(apps, alive, []string{"node3"})
	if result != "node2" {
		t.Errorf("expected node2 (1 app, node3 excluded), got %s", result)
	}

	// Exclude all -> empty string
	result = leastLoadedExcluding(apps, alive, []string{"node1", "node2", "node3"})
	if result != "" {
		t.Errorf("expected empty (all excluded), got %s", result)
	}
}

func TestLeastLoadedExcludingWithMetricsPrefersLowerLoadOverFreeRAM(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", AssignedAgent: strPtr("node1")},
	}
	alive := map[string]bool{"node1": true, "node2": true}
	metrics := map[string]common.NodeMetrics{
		"node1": {MemoryTotal: 8 << 30, MemoryUsed: 2 << 30, MemoryPercent: 25},
		"node2": {MemoryTotal: 4 << 30, MemoryUsed: 3 << 30, MemoryPercent: 75},
	}

	result := leastLoadedExcludingWithMetrics(apps, alive, nil, metrics, 0)
	// node2 has 0 apps (lower load), node1 has 1 app (more free RAM).
	// Load is preferred over free RAM for stable distribution.
	if result != "node2" {
		t.Fatalf("expected node2 with lower load (0 apps), got %s", result)
	}
}

func TestLeastLoadedExcludingWithMetricsFreeRAMTiebreaksEqualLoad(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", AssignedAgent: strPtr("node1")},
		"app2": {ID: "app2", AssignedAgent: strPtr("node2")},
	}
	alive := map[string]bool{"node1": true, "node2": true}
	metrics := map[string]common.NodeMetrics{
		"node1": {MemoryTotal: 8 << 30, MemoryUsed: 2 << 30, MemoryPercent: 25},
		"node2": {MemoryTotal: 8 << 30, MemoryUsed: 6 << 30, MemoryPercent: 75},
	}

	result := leastLoadedExcludingWithMetrics(apps, alive, nil, metrics, 0)
	// Both have 1 app. node1 has more free RAM → tiebreaker picks node1
	if result != "node1" {
		t.Fatalf("expected node1 with more free RAM (equal load), got %s", result)
	}
}

func TestLeastLoadedExcludingWithMetricsRespectsMaxRAMPercent(t *testing.T) {
	apps := map[common.AppID]*App{}
	alive := map[string]bool{"node1": true, "node2": true}
	metrics := map[string]common.NodeMetrics{
		"node1": {MemoryTotal: 16 << 30, MemoryUsed: 15 << 30, MemoryPercent: 93.75},
		"node2": {MemoryTotal: 8 << 30, MemoryUsed: 5 << 30, MemoryPercent: 62.5},
	}

	result := leastLoadedExcludingWithMetrics(apps, alive, nil, metrics, 85)
	if result != "node2" {
		t.Fatalf("expected node2 below max RAM threshold, got %s", result)
	}
}

func TestMaxRAMPercentDefaultsTo80(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	if got := mgr.maxRAMPercent(); got != 80 {
		t.Fatalf("expected missing max RAM percent to default to 80, got %v", got)
	}
}

func TestRebalanceWithMetricsMovesStoppedAppOffRAMConstrainedNode(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
	}
	alive := map[string]bool{"node1": true, "node2": true}
	metrics := map[string]common.NodeMetrics{
		"node1": {MemoryTotal: 8 << 30, MemoryUsed: 7 << 30, MemoryPercent: 87.5},
		"node2": {MemoryTotal: 8 << 30, MemoryUsed: 2 << 30, MemoryPercent: 25},
	}

	rebalanceWithMetrics(apps, alive, nil, nil, metrics, 80)

	if apps["app1"].AssignedAgent == nil || *apps["app1"].AssignedAgent != "node2" {
		t.Fatalf("expected app1 to move to node2, got %v", apps["app1"].AssignedAgent)
	}
}

func TestResolveGateway_PrefersHealthyAdguardCandidate(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	mgr.leaseManager.Heartbeat("node1", "10.0.0.1", "", []common.AppReport{
		healthyReport(common.GatewayProxyAppID),
	}, common.NodeMetrics{})
	mgr.leaseManager.Heartbeat("node2", "10.0.0.2", "", []common.AppReport{
		healthyReport(common.GatewayProxyAppID),
		healthyReport("adguard"),
	}, common.NodeMetrics{})

	mgr.gatewayNode = "node1"
	mgr.resolveGateway()

	if mgr.gatewayNode != "node2" {
		t.Fatalf("expected gateway node2, got %s", mgr.gatewayNode)
	}
}

func TestResolveGateway_KeepsCurrentPreferredCandidate(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	mgr.leaseManager.Heartbeat("node1", "10.0.0.1", "", []common.AppReport{
		healthyReport(common.GatewayProxyAppID),
		healthyReport("adguard"),
	}, common.NodeMetrics{})
	mgr.leaseManager.Heartbeat("node2", "10.0.0.2", "", []common.AppReport{
		healthyReport(common.GatewayProxyAppID),
		healthyReport("adguard"),
	}, common.NodeMetrics{})

	mgr.gatewayNode = "node2"
	mgr.resolveGateway()

	if mgr.gatewayNode != "node2" {
		t.Fatalf("expected current preferred gateway node2 to remain selected, got %s", mgr.gatewayNode)
	}
}

func TestResolveGateway_ManualGatewayOverridesAdguardPreference(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	mgr.leaseManager.Heartbeat("node1", "10.0.0.1", "", []common.AppReport{
		healthyReport(common.GatewayProxyAppID),
	}, common.NodeMetrics{})
	mgr.leaseManager.Heartbeat("node2", "10.0.0.2", "", []common.AppReport{
		healthyReport(common.GatewayProxyAppID),
		healthyReport("adguard"),
	}, common.NodeMetrics{})

	mgr.manualGatewayNode = "node1"
	mgr.resolveGateway()

	if mgr.gatewayNode != "node1" {
		t.Fatalf("expected manual gateway node1, got %s", mgr.gatewayNode)
	}
}

func healthyReport(appID common.AppID) common.AppReport {
	return common.AppReport{
		AppID:         appID,
		ObservedState: common.ObservedRunning,
		Health:        common.HealthHealthy,
	}
}

func TestReconcileOnce_FullIntegration(t *testing.T) {
	// Set up a manager with a lease manager and app registry
	cfg := Config{
		RepoDir: t.TempDir(),
		Lease: LeaseConfig{
			LeaseTTL:        30 * time.Second,
			CleanupInterval: 1 * time.Hour, // disable auto-cleanup for test
		},
	}
	mgr, err := NewManagerWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	// Register agents
	mgr.leaseManager.Heartbeat("node1", "10.0.0.1", "", nil, common.NodeMetrics{})
	mgr.leaseManager.Heartbeat("node2", "10.0.0.2", "", nil, common.NodeMetrics{})

	// Register apps
	mgr.appRegistry.Register("app1", common.DesiredRunning, false)
	mgr.appRegistry.Register("app2", common.DesiredRunning, false)
	mgr.appRegistry.Register("app3", common.DesiredRunning, false)
	mgr.appRegistry.Register("app4", common.DesiredStopped, false)

	// Assign app1 to a "dead" node (not registered)
	mgr.appRegistry.WithLock(func(apps map[common.AppID]*App) {
		dead := "dead-node"
		apps["app1"].AssignedAgent = &dead
	})

	// Run reconcile
	mgr.reconcileOnce(time.Now())

	// app1: dead-node is dead -> unassigned -> reassigned to least-loaded
	app1 := mgr.appRegistry.Get("app1")
	if app1.AssignedAgent == nil {
		t.Error("app1 should be reassigned after dead agent cleanup")
	}

	// app2 and app3 should be assigned
	app2 := mgr.appRegistry.Get("app2")
	app3 := mgr.appRegistry.Get("app3")
	if app2.AssignedAgent == nil {
		t.Error("app2 should be assigned")
	}
	if app3.AssignedAgent == nil {
		t.Error("app3 should be assigned")
	}

	// app4 (stopped) should NOT be assigned
	app4 := mgr.appRegistry.Get("app4")
	if app4.AssignedAgent != nil {
		t.Error("stopped app4 should not be assigned")
	}

	// Verify distribution: 3 running apps across 2 nodes -> 2/1 or 1/2 split
	loadMap := map[string]int{}
	for _, app := range mgr.appRegistry.ListAll() {
		if app.AssignedAgent != nil {
			loadMap[*app.AssignedAgent]++
		}
	}
	if len(loadMap) > 2 {
		t.Errorf("apps should only be on node1 and node2, got %v", loadMap)
	}
}
