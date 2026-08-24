package manager

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func TestAppPruneAtNewerDoesNotDeadlockWhileBackupMuHeld(t *testing.T) {
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	prunedAt := time.Now()
	m.leaseManager.Heartbeat("node1", "10.0.0.1", "", []common.AppReport{
		{
			AppID:       "app1",
			LastPruneAt: prunedAt,
		},
	}, common.NodeMetrics{})

	done := make(chan bool, 1)
	go func() {
		m.backupMu.Lock()
		ok := m.appPruneAtNewer("node1", "app1", prunedAt.Add(-time.Minute))
		m.backupMu.Unlock()
		done <- ok
	}()

	select {
	case ok := <-done:
		if !ok {
			t.Fatal("expected newer prune timestamp to be detected")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("appPruneAtNewer deadlocked while backupMu was held")
	}

	m.backupMu.Lock()
	state := m.pruneState["app1"]
	m.backupMu.Unlock()
	if !state.LastPruneAt.Equal(prunedAt) {
		t.Fatalf("expected prune state %v, got %v", prunedAt, state.LastPruneAt)
	}
}

func TestBackupAssignmentUncertainStartsGracePeriod(t *testing.T) {
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.appRegistry.Register("app1", common.DesiredRunning, false)
	assignedAt := time.Now().Add(-time.Minute)
	m.leaseManager.Heartbeat("node1", "10.0.0.1", "", []common.AppReport{
		{
			AppID:         "app1",
			ObservedState: common.ObservedRunning,
			Health:        common.HealthHealthy,
			LastBackupAt:  assignedAt.Add(-time.Hour),
			BackupBusy:    false,
		},
	}, common.NodeMetrics{})
	assignment := &BackupAssignment{
		Kind:       AssignmentKindBackup,
		AgentID:    "node1",
		AppID:      "app1",
		AssignedAt: assignedAt,
		State:      AssignmentRunning,
	}
	m.backupAssignments["app1"] = assignment

	m.backupMu.Lock()
	m.resolveWorkAssignments()
	firstFailedAt := assignment.FailedAt
	m.resolveWorkAssignments()
	m.backupMu.Unlock()

	if assignment.State != AssignmentUncertain {
		t.Fatalf("state = %s, want uncertain", assignment.State)
	}
	if firstFailedAt.IsZero() {
		t.Fatal("uncertain assignment did not record the start of its grace period")
	}
	if !assignment.FailedAt.Equal(firstFailedAt) {
		t.Fatal("uncertain assignment grace period was unexpectedly reset")
	}
}

func TestPruneAssignmentUncertainStartsGracePeriod(t *testing.T) {
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.appRegistry.Register("app1", common.DesiredRunning, false)
	assignedAt := time.Now().Add(-time.Minute)
	m.leaseManager.Heartbeat("node1", "10.0.0.1", "", []common.AppReport{
		{
			AppID:         "app1",
			ObservedState: common.ObservedRunning,
			Health:        common.HealthHealthy,
			LastBackupAt:  time.Now(),
			LastPruneAt:   assignedAt.Add(-time.Hour),
			PruneBusy:     false,
		},
	}, common.NodeMetrics{})
	assignment := &BackupAssignment{
		Kind:       AssignmentKindPrune,
		AgentID:    "node1",
		AppID:      "app1",
		AssignedAt: assignedAt,
		State:      AssignmentRunning,
	}
	m.pruneAssignment = assignment

	m.backupMu.Lock()
	m.resolveWorkAssignments()
	firstFailedAt := assignment.FailedAt
	m.resolveWorkAssignments()
	m.backupMu.Unlock()

	if assignment.State != AssignmentUncertain {
		t.Fatalf("state = %s, want uncertain", assignment.State)
	}
	if firstFailedAt.IsZero() {
		t.Fatal("uncertain prune assignment did not record the start of its grace period")
	}
	if !assignment.FailedAt.Equal(firstFailedAt) {
		t.Fatal("uncertain prune assignment grace period was unexpectedly reset")
	}
}

func TestAssignmentShouldDispatchOnlyActiveStates(t *testing.T) {
	tests := []struct {
		state BackupAssignmentState
		want  bool
	}{
		{state: AssignmentPending, want: true},
		{state: AssignmentRunning, want: true},
		{state: AssignmentUncertain, want: false},
		{state: AssignmentFailed, want: false},
		{state: AssignmentCompleted, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			if got := assignmentShouldDispatch(&BackupAssignment{State: tt.state}); got != tt.want {
				t.Fatalf("assignmentShouldDispatch(%s) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
	if assignmentShouldDispatch(nil) {
		t.Fatal("nil assignment should not dispatch")
	}
}

func TestGatewayOnlyBackupPolicyUsesGatewayWithoutReplicaRestore(t *testing.T) {
	repoDir := t.TempDir()
	appDir := filepath.Join(repoDir, "docker", "mesh-app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "homectl-config.json"), []byte(`{
  "is_global": true,
  "backup_policy": "gateway_only"
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(repoDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.appRegistry.Register("mesh-app", common.DesiredRunning, true)
	m.gatewayMu.Lock()
	m.gatewayNode = "primary"
	m.gatewayMu.Unlock()

	staleReport := []common.AppReport{{
		AppID:         "mesh-app",
		ObservedState: common.ObservedRunning,
		Health:        common.HealthHealthy,
		LastBackupAt:  time.Now().Add(-4 * time.Hour),
	}}
	m.leaseManager.Heartbeat("primary", "10.0.0.1", "", staleReport, common.NodeMetrics{})
	m.leaseManager.Heartbeat("replica", "10.0.0.2", "", staleReport, common.NodeMetrics{})

	candidates := m.findStaleBackupApps()
	if len(candidates) != 1 || candidates[0].AgentID != "primary" || candidates[0].AppID != "mesh-app" {
		t.Fatalf("candidates = %#v, want mesh-app on primary only", candidates)
	}

	m.triggerGlobalRestore("primary", "mesh-app")
	if pending := m.pendingGlobalRestores["mesh-app"]; len(pending) != 0 {
		t.Fatalf("gateway-only backup queued replica restores: %#v", pending)
	}
}

func TestLegacyGlobalBackupStillQueuesReplicaRestore(t *testing.T) {
	repoDir := t.TempDir()
	appDir := filepath.Join(repoDir, "docker", "global-app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "homectl-config.json"), []byte(`{"is_global":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(repoDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	m.gatewayMu.Lock()
	m.gatewayNode = "primary"
	m.gatewayMu.Unlock()
	m.leaseManager.Heartbeat("primary", "10.0.0.1", "", nil, common.NodeMetrics{})
	m.leaseManager.Heartbeat("replica", "10.0.0.2", "", nil, common.NodeMetrics{})

	m.triggerGlobalRestore("primary", "global-app")
	if !m.pendingGlobalRestores["global-app"]["replica"] {
		t.Fatal("legacy global backup did not queue a replica restore")
	}
}

func TestGatewayOnlyBackupPolicyRejectsManualWorkOnReplica(t *testing.T) {
	repoDir := t.TempDir()
	appDir := filepath.Join(repoDir, "docker", "mesh-app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "homectl-config.json"), []byte(`{
  "is_global": true,
  "backup_policy": "gateway_only"
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(repoDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	m.gatewayMu.Lock()
	m.gatewayNode = "primary"
	m.gatewayMu.Unlock()
	m.leaseManager.Heartbeat("replica", "10.0.0.2", "", nil, common.NodeMetrics{})

	tests := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{name: "backup", path: "/api/v1/apps/mesh-app/backup?node=replica", handler: m.handleAPITriggerBackup},
		{name: "prune", path: "/api/v1/apps/mesh-app/prune?node=replica", handler: m.handleAPITriggerPrune},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tt.handler(recorder, httptest.NewRequest(http.MethodPost, tt.path, nil))
			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
			}
		})
	}
}

func TestFindStaleBackupAppsSkipsRestoreSnapshotOverride(t *testing.T) {
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	now := time.Now()
	m.appRegistry.Register("overridden-app", common.DesiredRunning, false)
	m.appRegistry.Register("normal-app", common.DesiredRunning, false)
	_ = m.appRegistry.SetRestoreSnapshotOverride("overridden-app", common.RestoreSnapshotOverride{
		SnapshotID: "snap-override",
	})

	m.leaseManager.Heartbeat("node1", "10.0.0.1", "", []common.AppReport{
		{
			AppID:           "overridden-app",
			ObservedState:   common.ObservedRunning,
			Health:          common.HealthHealthy,
			LastBackupAt:    now.Add(-2 * time.Hour),
			BackupBusy:      false,
			PruneBusy:       false,
			BackupDefer:     time.Time{},
			LastActiveAt:    time.Time{},
			PostBackupPhase: "",
		},
		{
			AppID:           "normal-app",
			ObservedState:   common.ObservedRunning,
			Health:          common.HealthHealthy,
			LastBackupAt:    now.Add(-2 * time.Hour),
			BackupBusy:      false,
			PruneBusy:       false,
			BackupDefer:     time.Time{},
			LastActiveAt:    time.Time{},
			PostBackupPhase: "",
		},
	}, common.NodeMetrics{})

	candidates := m.findStaleBackupApps()
	if len(candidates) != 1 {
		t.Fatalf("expected 1 backup candidate, got %d", len(candidates))
	}
	if candidates[0].AppID != "normal-app" {
		t.Fatalf("expected normal-app candidate, got %s", candidates[0].AppID)
	}
}
