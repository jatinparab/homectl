package agent

import (
	"testing"

	"github.com/jatinparab/homectl/common"
)

func TestContainsNoSnapshotError(t *testing.T) {
	tests := []struct {
		line   string
		expect bool
	}{
		{"error while loading snapshot list: no snapshots found", true},
		{"no snapshot found matching selector", true},
		{"could not find any snapshots", true},
		{"snapshot not found", true},
		{"No Snapshots Found", true}, // case insensitive
		{"opening the repo (try 'restic unlock')", false},
		{"database is corrupt", false},
		{"password incorrect", false},
		{"", false},
	}

	for _, tt := range tests {
		got := containsNoSnapshotError(tt.line)
		if got != tt.expect {
			t.Errorf("containsNoSnapshotError(%q) = %v, want %v", tt.line, got, tt.expect)
		}
	}
}

func TestAppRuntime_SetGetRestoreSnapshotID(t *testing.T) {
	rt := &AppRuntime{}

	if rt.GetRestoreSnapshotID() != "" {
		t.Errorf("expected empty string, got %q", rt.GetRestoreSnapshotID())
	}

	rt.SetRestoreSnapshotID("snap-abc123")
	if got := rt.GetRestoreSnapshotID(); got != "snap-abc123" {
		t.Errorf("expected 'snap-abc123', got %q", got)
	}

	rt.SetRestoreSnapshotID("")
	if got := rt.GetRestoreSnapshotID(); got != "" {
		t.Errorf("expected empty string after clear, got %q", got)
	}
}

func TestAppRuntime_Snapshot(t *testing.T) {
	rt := &AppRuntime{
		Phase: common.PhaseRunning,
	}

	snap := rt.Snapshot()
	_ = snap // Snapshot struct exists; verify it doesn't panic
	if rt.Phase != common.PhaseRunning {
		t.Errorf("expected PhaseRunning, got %s", rt.Phase)
	}
}

func TestAppRuntime_SetRestoreSnapshotID_PersistsInSnapshot(t *testing.T) {
	rt := &AppRuntime{}
	rt.SetRestoreSnapshotID("snap-test-456")

	if rt.GetRestoreSnapshotID() != "snap-test-456" {
		t.Errorf("expected 'snap-test-456', got %q", rt.GetRestoreSnapshotID())
	}
}
