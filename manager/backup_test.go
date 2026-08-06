package manager

import (
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
