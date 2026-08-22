package agent

import (
	"testing"

	"github.com/jatinparab/homectl/common"
)

func TestSuccessfulStartPreventsDuplicateStartBeforeSensorTick(t *testing.T) {
	rt := NewAppRuntime("test-app")
	markAppStarted(rt, common.PhaseRunning)
	rt.SetHealth(common.HealthHealthy)

	a := &Agent{}
	action := a.reconcileApp(common.DesiredApp{
		AppID:        "test-app",
		DesiredState: common.DesiredRunning,
	}, rt)
	if action.Type != ActionTypeNone {
		t.Fatalf("reconcile action = %v, want none", action.Type)
	}
}
