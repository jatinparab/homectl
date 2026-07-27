package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

var errBackupProbe = errors.New("backup probe stopped")

type deadlineProbeRestic struct {
	remaining time.Duration
}

func (r *deadlineProbeRestic) Backup(ctx context.Context, _ common.AppID, _ ProgressCallback, _ LogCallback) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("backup context has no deadline")
	}
	r.remaining = time.Until(deadline)
	return errBackupProbe
}

func (*deadlineProbeRestic) Restore(context.Context, common.AppID, string, ProgressCallback, LogCallback) error {
	panic("unexpected Restore call")
}

func (*deadlineProbeRestic) Prune(context.Context, common.AppID, LogCallback) error {
	panic("unexpected Prune call")
}

func (*deadlineProbeRestic) Check(context.Context, LogCallback) error {
	panic("unexpected Check call")
}

func (*deadlineProbeRestic) Unlock(context.Context, LogCallback) error {
	panic("unexpected Unlock call")
}

func (*deadlineProbeRestic) ForceUnlock(context.Context, LogCallback) error {
	panic("unexpected ForceUnlock call")
}

func (*deadlineProbeRestic) LatestSnapshot(context.Context, common.AppID) (time.Time, string, error) {
	panic("unexpected LatestSnapshot call")
}

func (*deadlineProbeRestic) Snapshot(context.Context, common.AppID, string) (time.Time, string, error) {
	panic("unexpected Snapshot call")
}

func (*deadlineProbeRestic) ListSnapshots(context.Context, common.AppID) ([]Snapshot, error) {
	panic("unexpected ListSnapshots call")
}

func (*deadlineProbeRestic) ListSnapshotFiles(context.Context, string, string) ([]SnapshotEntry, error) {
	panic("unexpected ListSnapshotFiles call")
}

func TestExecuteBackupAppliesTimeoutAndResetsState(t *testing.T) {
	appID := common.AppID("timeout-test")
	rt := NewAppRuntime(appID)
	probe := &deadlineProbeRestic{}
	agent := &Agent{
		name:    "test-agent",
		runtime: map[common.AppID]*AppRuntime{appID: rt},
	}
	agent.backupCond = sync.NewCond(&agent.backupMu)
	executor := &Executor{
		restic: probe,
		agent:  agent,
		ctx:    context.Background(),
	}

	err := executor.executeBackup(appID, rt)
	if !errors.Is(err, errBackupProbe) {
		t.Fatalf("executeBackup error = %v, want %v", err, errBackupProbe)
	}
	if probe.remaining < BackupTimeout-time.Second || probe.remaining > BackupTimeout {
		t.Fatalf("backup deadline remaining = %v, want approximately %v", probe.remaining, BackupTimeout)
	}
	if got := rt.GetPhase(); got != common.PhaseEmpty {
		t.Fatalf("phase after failed backup = %q, want %q", got, common.PhaseEmpty)
	}
	if rt.Backup.InProgress {
		t.Fatal("backup remained in progress after failure")
	}
}
