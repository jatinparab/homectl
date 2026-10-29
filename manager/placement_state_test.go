package manager

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func placementTestManager(t *testing.T, repo string, apps ...string) *Manager {
	t.Helper()
	for _, app := range apps {
		dir := filepath.Join(repo, "docker", app)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	m, err := NewManager(repo, 1338)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	m.DiscoverApps()
	return m
}

func runningPlacementApps(t *testing.T, m *Manager, apps ...string) {
	t.Helper()
	for _, app := range apps {
		if err := m.appRegistry.UpdateDesiredState(common.AppID(app), common.DesiredRunning); err != nil {
			t.Fatal(err)
		}
	}
	m.saveDesiredState()
}

func assertPlacement(t *testing.T, m *Manager, app, node string, manual bool) {
	t.Helper()
	a := m.appRegistry.Get(common.AppID(app))
	if a == nil || a.AssignedAgent == nil || *a.AssignedAgent != node || a.ManuallyAssigned != manual {
		t.Fatalf("%s: want node=%s manual=%v, got %+v", app, node, manual, a)
	}
}

func desiredPlacementResponse(m *Manager, node string) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	m.handleDesiredApps(r, httptest.NewRequest(http.MethodGet, "/agents/"+node+"/desired", nil), node)
	return r
}

func TestPlacementsSurviveRestartAndStaggeredEmptyHeartbeats(t *testing.T) {
	repo := t.TempDir()
	m := placementTestManager(t, repo, "photos", "auth", "stopped")
	runningPlacementApps(t, m, "photos", "auth")
	m.appRegistry.WithLock(func(apps map[common.AppID]*App) {
		apps["photos"].AssignedAgent = strPtr("node-1")
		apps["auth"].AssignedAgent = strPtr("node-3")
		apps["auth"].ManuallyAssigned = true
		apps["stopped"].AssignedAgent = strPtr("node-1")
		apps[common.GatewayProxyAppID].AssignedAgent = strPtr("do-not-persist-global")
	})
	m.savePlacements()
	// The complete snapshot takes precedence over old manual-only state.
	if err := os.WriteFile(m.manualAssignmentsPath(), []byte(`{"photos":"wrong-node"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m.Stop()

	restarted := placementTestManager(t, repo, "photos", "auth", "stopped")
	assertPlacement(t, restarted, "photos", "node-1", false)
	assertPlacement(t, restarted, "auth", "node-3", true)
	assertPlacement(t, restarted, "stopped", "node-1", false)
	if restarted.appRegistry.Get(common.GatewayProxyAppID).AssignedAgent != nil {
		t.Fatal("global assignment restored")
	}
	if restarted.appRegistry.Get("stopped").DesiredState != common.DesiredStopped {
		t.Fatal("stopped desired state lost")
	}

	// node-3 reconnects before node-1, without a populated inventory.
	restarted.applyHeartbeat(HeartbeatRequest{Name: "node-3", IP: "10.44.0.3"})
	restarted.reconcileOnce(time.Now())
	assertPlacement(t, restarted, "photos", "node-1", false)
	assertPlacement(t, restarted, "auth", "node-3", true)
	if r := desiredPlacementResponse(restarted, "node-3"); r.Code != http.StatusOK {
		t.Fatalf("known placement unavailable: %d %s", r.Code, r.Body.String())
	}

	restarted.applyHeartbeat(HeartbeatRequest{Name: "node-1", IP: "10.44.0.2"})
	restarted.reconcileOnce(time.Now())
	assertPlacement(t, restarted, "photos", "node-1", false)
	restarted.applyHeartbeat(HeartbeatRequest{Name: "node-1", IP: "10.44.0.2", Reports: []common.AppReport{healthyReport("photos")}})
	restarted.applyHeartbeat(HeartbeatRequest{Name: "node-3", IP: "10.44.0.3", Reports: []common.AppReport{healthyReport("auth"), {AppID: "photos", ObservedState: common.ObservedStopped}}})
	// Past startup recovery, a non-owner's stopped report still cannot move photos.
	restarted.reconcileOnce(restarted.placementRecoveryUntil.Add(time.Second))
	assertPlacement(t, restarted, "photos", "node-1", false)
	if restarted.appRegistry.Get("photos").ObservedState != common.ObservedRunning {
		t.Fatal("non-owner overwrote running state")
	}
}

func TestColdPlacementRecoveryWaitsForWorkloadReports(t *testing.T) {
	for _, state := range []string{"absent", "corrupt", "unsupported-version"} {
		t.Run(state, func(t *testing.T) {
			repo := t.TempDir()
			if state != "absent" {
				dir := filepath.Join(repo, ".homectl")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				data := []byte("{truncated")
				if state == "unsupported-version" {
					data = []byte(`{"version":99,"assignments":{}}`)
				}
				if err := os.WriteFile(filepath.Join(dir, "placements.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			m := placementTestManager(t, repo, "photos", "auth")
			runningPlacementApps(t, m, "photos", "auth")
			m.applyHeartbeat(HeartbeatRequest{Name: "node-3", IP: "10.44.0.3"})
			m.reconcileOnce(time.Now())
			if m.appRegistry.Get("photos").AssignedAgent != nil {
				t.Fatal("empty heartbeat captured an existing workload")
			}
			if r := desiredPlacementResponse(m, "node-3"); r.Code != http.StatusServiceUnavailable {
				t.Fatalf("partial desired state escaped recovery: %d", r.Code)
			}
			m.applyHeartbeat(HeartbeatRequest{Name: "node-1", IP: "10.44.0.2", Reports: []common.AppReport{healthyReport("photos")}})
			if r := desiredPlacementResponse(m, "node-1"); r.Code != http.StatusServiceUnavailable {
				t.Fatalf("missing auth owner must hold destructive desired response: %d", r.Code)
			}
			m.applyHeartbeat(HeartbeatRequest{Name: "node-3", IP: "10.44.0.3", Reports: []common.AppReport{{AppID: "auth", ObservedState: common.ObservedStopped, Phase: common.PhaseBackingUp, BackupBusy: true}}})
			assertPlacement(t, m, "photos", "node-1", false)
			assertPlacement(t, m, "auth", "node-3", false)
			if r := desiredPlacementResponse(m, "node-1"); r.Code != http.StatusOK {
				t.Fatalf("recovered inventory still blocked: %d %s", r.Code, r.Body.String())
			}
			m.reconcileOnce(time.Now())
			assertPlacement(t, m, "auth", "node-3", false)
		})
	}
}

func TestPlacementRecoveryIsBoundedAndDeadOwnersFailOver(t *testing.T) {
	for _, knownOwner := range []bool{false, true} {
		t.Run(map[bool]string{false: "owner-never-reconnects", true: "owner-dies-later"}[knownOwner], func(t *testing.T) {
			m := placementTestManager(t, t.TempDir(), "photos")
			runningPlacementApps(t, m, "photos")
			if knownOwner {
				m.appRegistry.WithLock(func(apps map[common.AppID]*App) { apps["photos"].AssignedAgent = strPtr("dead-owner") })
				m.applyHeartbeat(HeartbeatRequest{Name: "dead-owner", IP: "10.44.0.9"})
				m.leaseManager.mu.Lock()
				m.leaseManager.agents["dead-owner"].LastSeen = time.Now().Add(-deadAgentGracePeriod - time.Second)
				m.leaseManager.mu.Unlock()
			}
			m.applyHeartbeat(HeartbeatRequest{Name: "replacement", IP: "10.44.0.2"})
			m.reconcileOnce(m.placementRecoveryUntil.Add(-time.Second))
			if !knownOwner && m.appRegistry.Get("photos").AssignedAgent != nil {
				t.Fatal("scheduled during recovery")
			}
			m.reconcileOnce(m.placementRecoveryUntil.Add(time.Second))
			assertPlacement(t, m, "photos", "replacement", false)
			if r := desiredPlacementResponse(m, "replacement"); r.Code != http.StatusOK {
				t.Fatalf("bounded recovery still blocks scheduling: %d", r.Code)
			}
		})
	}
}

func TestPersistedOwnerNeverReconnectsAfterRestart(t *testing.T) {
	repo := t.TempDir()
	m := placementTestManager(t, repo, "photos")
	runningPlacementApps(t, m, "photos")
	m.appRegistry.ReassignAgent("photos", "missing")
	m.savePlacements()
	m.Stop()
	m = placementTestManager(t, repo, "photos")
	m.applyHeartbeat(HeartbeatRequest{Name: "replacement", IP: "10.44.0.2"})
	m.reconcileOnce(time.Now())
	assertPlacement(t, m, "photos", "missing", true)
	m.reconcileOnce(m.placementRecoveryUntil.Add(time.Second))
	assertPlacement(t, m, "photos", "replacement", false)
	m.Stop()
	m = placementTestManager(t, repo, "photos")
	assertPlacement(t, m, "photos", "replacement", false)
}

func TestDiscoveryDoesNotRestoreStaleSnapshotOverLivePlacement(t *testing.T) {
	repo := t.TempDir()
	m := placementTestManager(t, repo, "photos")
	m.appRegistry.ReassignAgent("photos", "before")
	m.savePlacements()
	m.Stop()
	m = placementTestManager(t, repo, "photos")
	m.appRegistry.ReassignAgent("photos", "after")
	m.appRegistry.ClearManualAssignment("photos")
	m.savePlacements()
	m.DiscoverApps()
	assertPlacement(t, m, "photos", "after", false)
	m.Stop()
	m = placementTestManager(t, repo, "photos")
	assertPlacement(t, m, "photos", "after", false)
}

func TestDesiredStateWaitsForDiscoveryEvenAfterHeartbeat(t *testing.T) {
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	m.applyHeartbeat(HeartbeatRequest{Name: "node", IP: "10.44.0.2"})
	if r := desiredPlacementResponse(m, "node"); r.Code != http.StatusServiceUnavailable {
		t.Fatalf("desired state served before discovery: %d", r.Code)
	}
	if _, err := os.Stat(m.placementsPath()); !os.IsNotExist(err) {
		t.Fatal("early heartbeat overwrote placement file")
	}
}

func TestUnknownOwnerReportsCannotTriggerAutomaticRebalance(t *testing.T) {
	for _, state := range []common.ObservedState{"", common.ObservedState("unknown"), common.ObservedError, common.ObservedRunning} {
		apps := map[common.AppID]*App{}
		for _, id := range []common.AppID{"a", "b", "c"} {
			apps[id] = &App{ID: id, DesiredState: common.DesiredRunning, AssignedAgent: strPtr("owner"), ObservedState: state}
		}
		rebalance(apps, map[string]bool{"owner": true, "other": true}, nil, nil)
		for _, app := range apps {
			if *app.AssignedAgent != "owner" {
				t.Fatalf("%s state was treated as an explicitly stopped app", state)
			}
		}
	}
}

func TestLegacyManualPlacementsMigrateToCompleteSnapshot(t *testing.T) {
	repo := t.TempDir()
	state := filepath.Join(repo, ".homectl")
	if err := os.MkdirAll(state, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "manual-assignments.json"), []byte(`{"photos":"legacy-owner"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := placementTestManager(t, repo, "photos")
	assertPlacement(t, m, "photos", "legacy-owner", true)
	m.Stop()
	m = placementTestManager(t, repo, "photos")
	if !m.hasPlacementSnapshot {
		t.Fatal("legacy manual placement was not migrated")
	}
	assertPlacement(t, m, "photos", "legacy-owner", true)
}

func TestAllPlacementMutationPathsPersist(t *testing.T) {
	for _, action := range []string{"heartbeat-adoption", "scheduler", "force-rebalance", "manual-api"} {
		t.Run(action, func(t *testing.T) {
			repo := t.TempDir()
			m := placementTestManager(t, repo, "one", "two", "three")
			runningPlacementApps(t, m, "one", "two", "three")
			m.applyHeartbeat(HeartbeatRequest{Name: "node-a", IP: "10.44.0.2"})
			m.applyHeartbeat(HeartbeatRequest{Name: "node-b", IP: "10.44.0.3"})
			switch action {
			case "heartbeat-adoption":
				m.applyHeartbeat(HeartbeatRequest{Name: "node-a", IP: "10.44.0.2", Reports: []common.AppReport{healthyReport("one")}})
			case "scheduler":
				m.placementRecoveryUntil = time.Time{}
				m.reconcileOnce(time.Now())
			case "force-rebalance":
				m.appRegistry.WithLock(func(apps map[common.AppID]*App) {
					for _, id := range []common.AppID{"one", "two", "three"} {
						apps[id].AssignedAgent = strPtr("node-a")
						apps[id].ObservedState = common.ObservedRunning
					}
				})
				m.ForceRebalance()
			case "manual-api":
				r := httptest.NewRecorder()
				m.handleAPIAppReassign(r, httptest.NewRequest(http.MethodPost, "/api/v1/apps/one/reassign?node=node-b", nil))
				if r.Code != http.StatusOK {
					t.Fatalf("manual assignment failed: %s", r.Body.String())
				}
				data, err := os.ReadFile(m.placementsPath())
				if err != nil {
					t.Fatal(err)
				}
				var snapshot placementSnapshot
				if err := json.Unmarshal(data, &snapshot); err != nil {
					t.Fatal(err)
				}
				if !snapshot.Assignments["one"].Manual {
					t.Fatal("API manual flag not persisted")
				}
				r = httptest.NewRecorder()
				m.handleAPIAppReassign(r, httptest.NewRequest(http.MethodDelete, "/api/v1/apps/one/reassign", nil))
				if r.Code != http.StatusOK {
					t.Fatalf("clear manual assignment failed: %s", r.Body.String())
				}
			}
			before := m.appRegistry.ListAll()
			m.Stop()
			m = placementTestManager(t, repo, "one", "two", "three")
			for _, app := range before {
				if app.IsGlobal {
					continue
				}
				if app.AssignedAgent == nil {
					if m.appRegistry.Get(app.ID).AssignedAgent != nil {
						t.Fatalf("invented placement for %s", app.ID)
					}
				} else {
					assertPlacement(t, m, string(app.ID), *app.AssignedAgent, app.ManuallyAssigned)
				}
			}
		})
	}
}

func TestPlacementSnapshotsRemainAtomicUnderConcurrentWriters(t *testing.T) {
	m := placementTestManager(t, t.TempDir(), "a", "b")
	m.savePlacements()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				node := string(rune('a'+worker)) + string(rune('0'+i%2))
				m.appRegistry.WithLock(func(apps map[common.AppID]*App) {
					apps["a"].AssignedAgent = strPtr(node)
					apps["b"].AssignedAgent = strPtr(node)
				})
				m.savePlacements()
				data, err := os.ReadFile(m.placementsPath())
				if err != nil {
					t.Error(err)
					return
				}
				var snapshot placementSnapshot
				if err := json.Unmarshal(data, &snapshot); err != nil {
					t.Error(err)
				}
				if snapshot.Assignments["a"].Agent != snapshot.Assignments["b"].Agent {
					t.Error("snapshot mixed two placement decisions")
				}
			}
		}(i)
	}
	wg.Wait()
	before, err := os.Stat(m.placementsPath())
	if err != nil {
		t.Fatal(err)
	}
	m.savePlacements()
	after, err := os.Stat(m.placementsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged placement rewrote snapshot")
	}
}
