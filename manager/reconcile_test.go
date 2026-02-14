package manager

import (
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func strPtr(s string) *string { return &s }

func TestHandleDeadAgents(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", AssignedAgent: strPtr("alive-node")},
		"app2": {ID: "app2", AssignedAgent: strPtr("dead-node")},
		"app3": {ID: "app3", AssignedAgent: nil},
	}
	alive := map[string]bool{"alive-node": true}

	handleDeadAgents(apps, alive)

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

	handleDeadAgents(apps, alive)

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

func TestAssignUnassignedApps_LeastLoaded(t *testing.T) {
	apps := map[common.AppID]*App{
		"app1": {ID: "app1", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app2": {ID: "app2", DesiredState: common.DesiredRunning, AssignedAgent: strPtr("node1")},
		"app3": {ID: "app3", DesiredState: common.DesiredRunning, AssignedAgent: nil},
	}
	alive := map[string]bool{"node1": true, "node2": true}

	assignUnassignedApps(apps, alive)

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

	assignUnassignedApps(apps, alive)

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

	assignUnassignedApps(apps, alive)

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

	assignUnassignedApps(apps, alive)

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

	handleMigrations(apps, alive, now)

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

	handleMigrations(apps, alive, now)

	if *apps["app1"].AssignedAgent != "node1" {
		t.Error("app should not be migrated within cooldown")
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

	handleMigrations(apps, alive, now)

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

	rebalance(apps, alive)

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

	rebalance(apps, alive)

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

	rebalance(apps, alive)

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

	rebalance(apps, alive)

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

func TestReconcileOnce_FullIntegration(t *testing.T) {
	// Set up a manager with a lease manager and app registry
	cfg := Config{
		RepoDir: t.TempDir(),
		Lease: LeaseConfig{
			LeaseTTL:        30 * time.Second,
			CleanupInterval: 1 * time.Hour, // disable auto-cleanup for test
		},
	}
	mgr := NewManagerWithConfig(cfg)
	defer mgr.Stop()

	// Register agents
	mgr.leaseManager.Heartbeat("node1", "10.0.0.1", nil)
	mgr.leaseManager.Heartbeat("node2", "10.0.0.2", nil)

	// Register apps
	mgr.appRegistry.Register("app1", common.DesiredRunning)
	mgr.appRegistry.Register("app2", common.DesiredRunning)
	mgr.appRegistry.Register("app3", common.DesiredRunning)
	mgr.appRegistry.Register("app4", common.DesiredStopped)

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
