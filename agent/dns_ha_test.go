package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func dnsTestAgent() *Agent {
	rt := NewAppRuntime(common.DNSAppID)
	rt.SetRunning(common.RunningStatusRunning)
	rt.SetHealth(common.HealthHealthy)
	rt.SetPhase(common.PhaseRunning)
	return &Agent{runtime: map[common.AppID]*AppRuntime{common.DNSAppID: rt}, runtimeConfig: map[string]string{common.ConfigKeyDNSHAIP: "192.168.50.251"}}
}

func TestDNSEligibilityIndependentOfGatewayAndManager(t *testing.T) {
	a := dnsTestAgent() // disconnected, non-gateway secondary
	if !a.dnsEligible() {
		t.Fatal("healthy secondary should be eligible offline")
	}
	a.dnsDraining.Store(true)
	if a.dnsEligible() {
		t.Fatal("draining node eligible")
	}
	a.dnsDraining.Store(false)
	for _, phase := range []common.Phase{common.PhaseBackupStopping, common.PhaseBackupStopped, common.PhaseBackingUp, common.PhaseRestoring, common.PhasePulling, common.PhaseError} {
		a.runtime[common.DNSAppID].SetPhase(phase)
		if a.dnsEligible() {
			t.Errorf("phase %s eligible", phase)
		}
	}
	a.runtime[common.DNSAppID].SetPhase(common.PhaseRunning)
	a.runtime[common.DNSAppID].SetHealth(common.HealthUnhealthy)
	if a.dnsEligible() {
		t.Fatal("unhealthy node eligible")
	}
	a.runtime[common.DNSAppID].SetHealth(common.HealthHealthy)
	a.runtime[common.DNSAppID].SetQuarantined(true)
	if a.dnsEligible() {
		t.Fatal("quarantined node eligible")
	}
}

func TestDNSReadyFailsClosedWithoutHostDNSConfig(t *testing.T) {
	a := dnsTestAgent()
	w := httptest.NewRecorder()
	NewReadyServer(a).handleDNSReady(w, httptest.NewRequest(http.MethodGet, "/dns-ready", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
}

func TestDNSHandoffRequiresStableReplacement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	states := []bool{true, true, false, true, true, true}
	calls := 0
	err := waitDNSHandoff(ctx, time.Millisecond, func(context.Context) (bool, error) {
		state := states[calls]
		calls++
		return state, nil
	})
	if err != nil || calls != len(states) {
		t.Fatalf("handoff: calls=%d err=%v", calls, err)
	}
}

func TestDNSHandoffRefusesNoReplacement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := waitDNSHandoff(ctx, time.Millisecond, func(context.Context) (bool, error) { return false, nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

type dnsBackupProbe struct {
	Restic
	backupErr error
}

func (p dnsBackupProbe) Backup(context.Context, common.AppID, ProgressCallback, LogCallback) error {
	return p.backupErr
}

func (dnsBackupProbe) ListSnapshots(context.Context, common.AppID) ([]Snapshot, error) {
	return nil, nil
}

func TestDNSBackupCompletionDoesNotReplayPendingRequest(t *testing.T) {
	for _, backupErr := range []error{nil, errors.New("upload failed")} {
		a := dnsTestAgent()
		a.stateDir = t.TempDir()
		a.backupCond = sync.NewCond(&a.backupMu)
		a.snapshotCache = make(map[common.AppID]*SnapshotCache)
		a.snapshotCancelFns = make(map[common.AppID]context.CancelFunc)
		probe := dnsBackupProbe{backupErr: backupErr}
		a.restic = probe
		rt := a.runtime[common.DNSAppID]
		err := (&Executor{agent: a, restic: probe, ctx: context.Background()}).executeBackup(common.DNSAppID, rt)
		if !errors.Is(err, backupErr) {
			t.Fatalf("backup error = %v, want %v", err, backupErr)
		}
		if a.dnsBackupRetryAt.Load() < time.Now().Add(50*time.Second).UnixNano() {
			t.Fatal("completed backup has no replay cooldown")
		}
		if backupErr != nil && a.dnsBackupRetryAt.Load() < rt.Backup.BackupDefer.UnixNano() {
			t.Fatal("upload failure backoff not preserved")
		}
		rt.SetPhase(common.PhaseRunning) // resolver has recovered
		app := common.DesiredApp{AppID: common.DNSAppID, DesiredState: common.DesiredRunning, BackupEnabled: true, BackupNow: true}
		if action := a.reconcileApp(app, rt); action.Type != ActionTypeNone {
			t.Fatalf("completed backup repeated pending request: %v", action)
		}
		if !a.dnsEligible() {
			t.Fatal("cooldown must leave recovered DNS eligible")
		}
	}
}

func TestDNSBackupFailureKeepsResolverRunningAndDefers(t *testing.T) {
	a := dnsTestAgent()
	rt := a.runtime[common.DNSAppID]
	rt.SetPhase(common.PhaseBackupStopping)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // ownership inspection fails closed, without stopping Docker
	err := (&Executor{agent: a, ctx: ctx}).executeStop(common.DNSAppID, rt)
	if err == nil {
		t.Fatal("backup stop succeeded without replacement")
	}
	if rt.GetPhase() != common.PhaseRunning || rt.RunningStatus() != common.RunningStatusRunning || a.dnsDraining.Load() {
		t.Fatal("failed drain did not restore eligibility")
	}
	if !rt.Backup.BackupDefer.After(time.Now()) {
		t.Fatal("backup retry not deferred")
	}
	// The manager's pending assignment remains BackupNow until it expires.
	// Do not withdraw DNS again on every agent reconciliation while deferred.
	app := common.DesiredApp{AppID: common.DNSAppID, DesiredState: common.DesiredRunning, BackupEnabled: true, BackupNow: true}
	if action := a.reconcileApp(app, rt); action.Type != ActionTypeNone {
		t.Fatalf("pending backup retried during cooldown: %v", action)
	}
	if !a.dnsEligible() {
		t.Fatal("backup cooldown must not withdraw functioning DNS")
	}
	a.dnsBackupRetryAt.Store(time.Now().Add(-time.Second).UnixNano())
	if action := a.reconcileApp(app, rt); action.Type != ActionTypeStop {
		t.Fatalf("backup not retried after cooldown: %v", action)
	}
}
