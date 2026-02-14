package manager

import (
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func TestAppRegistry_Register_Idempotent(t *testing.T) {
	reg := NewAppRegistry()
	reg.Register("app1", common.DesiredRunning)
	reg.Register("app1", common.DesiredStopped) // should not overwrite

	app := reg.Get("app1")
	if app == nil {
		t.Fatal("expected app1 to exist")
	}
	if app.DesiredState != common.DesiredRunning {
		t.Errorf("expected DesiredRunning, got %s", app.DesiredState)
	}
}

func TestAppRegistry_Get_ReturnsNilForUnknown(t *testing.T) {
	reg := NewAppRegistry()
	if app := reg.Get("nonexistent"); app != nil {
		t.Error("expected nil for unknown app")
	}
}

func TestAppRegistry_Get_ReturnsCopy(t *testing.T) {
	reg := NewAppRegistry()
	reg.Register("app1", common.DesiredRunning)

	app := reg.Get("app1")
	agent := "node1"
	app.AssignedAgent = &agent

	// Original should be unmodified
	original := reg.Get("app1")
	if original.AssignedAgent != nil {
		t.Error("Get should return a copy; original was modified")
	}
}

func TestAppRegistry_ListAll(t *testing.T) {
	reg := NewAppRegistry()
	reg.Register("app1", common.DesiredRunning)
	reg.Register("app2", common.DesiredStopped)

	apps := reg.ListAll()
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}

	found := map[common.AppID]bool{}
	for _, a := range apps {
		found[a.ID] = true
	}
	if !found["app1"] || !found["app2"] {
		t.Error("expected both app1 and app2 in ListAll")
	}
}

func TestAppRegistry_AppsForAgent(t *testing.T) {
	reg := NewAppRegistry()
	reg.Register("app1", common.DesiredRunning)
	reg.Register("app2", common.DesiredRunning)
	reg.Register("app3", common.DesiredRunning)

	// Assign app1 and app3 to node1, app2 to node2
	reg.WithLock(func(apps map[common.AppID]*App) {
		n1 := "node1"
		n2 := "node2"
		apps["app1"].AssignedAgent = &n1
		apps["app2"].AssignedAgent = &n2
		apps["app3"].AssignedAgent = &n1
	})

	node1Apps := reg.AppsForAgent("node1")
	if len(node1Apps) != 2 {
		t.Fatalf("expected 2 apps for node1, got %d", len(node1Apps))
	}

	node2Apps := reg.AppsForAgent("node2")
	if len(node2Apps) != 1 {
		t.Fatalf("expected 1 app for node2, got %d", len(node2Apps))
	}
	if node2Apps[0].ID != "app2" {
		t.Errorf("expected app2 for node2, got %s", node2Apps[0].ID)
	}

	// Unassigned agent returns empty
	empty := reg.AppsForAgent("node99")
	if len(empty) != 0 {
		t.Errorf("expected 0 apps for node99, got %d", len(empty))
	}
}

func TestAppRegistry_UpdateFromReport_KnownApp(t *testing.T) {
	reg := NewAppRegistry()
	reg.Register("app1", common.DesiredRunning)

	now := time.Now()
	report := common.AppReport{
		AppID:         "app1",
		ObservedState: common.ObservedRunning,
		Health:        common.HealthHealthy,
		RestartCount:  3,
		Quarantined:   true,
	}

	reg.UpdateFromReport(report, "node1", now)

	app := reg.Get("app1")
	if app.ObservedState != common.ObservedRunning {
		t.Errorf("expected ObservedRunning, got %s", app.ObservedState)
	}
	if app.Health != common.HealthHealthy {
		t.Errorf("expected HealthHealthy, got %s", app.Health)
	}
	if app.RestartCount != 3 {
		t.Errorf("expected RestartCount 3, got %d", app.RestartCount)
	}
	if !app.Quarantined {
		t.Error("expected Quarantined true")
	}
	if !app.LastSeen.Equal(now) {
		t.Errorf("expected LastSeen %v, got %v", now, app.LastSeen)
	}
}

func TestAppRegistry_UpdateFromReport_IgnoresUnknown(t *testing.T) {
	reg := NewAppRegistry()

	report := common.AppReport{
		AppID:         "unknown_app",
		ObservedState: common.ObservedRunning,
	}

	// Should not panic or create the app
	reg.UpdateFromReport(report, "node1", time.Now())

	if app := reg.Get("unknown_app"); app != nil {
		t.Error("UpdateFromReport should not create unknown apps")
	}
}

func TestAppRegistry_WithLock(t *testing.T) {
	reg := NewAppRegistry()
	reg.Register("app1", common.DesiredRunning)

	agent := "node1"
	reg.WithLock(func(apps map[common.AppID]*App) {
		apps["app1"].AssignedAgent = &agent
	})

	app := reg.Get("app1")
	if app.AssignedAgent == nil || *app.AssignedAgent != "node1" {
		t.Error("WithLock should allow mutation of apps")
	}
}

func TestAppRegistry_QuarantinedOn_CopiedCorrectly(t *testing.T) {
	reg := NewAppRegistry()
	reg.Register("app1", common.DesiredRunning)

	reg.WithLock(func(apps map[common.AppID]*App) {
		apps["app1"].QuarantinedOn = []string{"node1", "node2"}
	})

	app := reg.Get("app1")
	app.QuarantinedOn = append(app.QuarantinedOn, "node3")

	// Original should still have only 2 entries
	original := reg.Get("app1")
	if len(original.QuarantinedOn) != 2 {
		t.Errorf("expected 2 quarantined nodes, got %d", len(original.QuarantinedOn))
	}
}
