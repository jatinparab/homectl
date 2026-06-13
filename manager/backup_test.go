package manager

import (
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func TestAppPruneAtNewerDoesNotDeadlockWhileBackupMuHeld(t *testing.T) {
	m := NewManager(t.TempDir(), 1338)
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
