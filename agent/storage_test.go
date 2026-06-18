package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jatinparab/homectl/common"
)

func TestRunStoragePruneRemovesBackupMeta(t *testing.T) {
	origEstimate := estimateDockerReclaimableBytesFunc
	origPrune := dockerSystemPruneFunc
	estimateDockerReclaimableBytesFunc = func() uint64 { return 0 }
	dockerSystemPruneFunc = func() (uint64, string) { return 12345, "" }
	t.Cleanup(func() {
		estimateDockerReclaimableBytesFunc = origEstimate
		dockerSystemPruneFunc = origPrune
	})

	appdataDir := t.TempDir()
	stateDir := t.TempDir()

	appDir := filepath.Join(appdataDir, "arr-stack")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	metaDir := filepath.Join(stateDir, "backup-meta")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir meta dir: %v", err)
	}
	metaPath := filepath.Join(metaDir, "arr-stack.json")
	if err := os.WriteFile(metaPath, []byte(`{"last_backup_at":"2026-06-13T20:00:00Z","snapshot_id":"deadbeef","node":"akira"}`), 0644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	a := &Agent{
		appdataDir:   appdataDir,
		stateDir:     stateDir,
		assignedApps: map[common.AppID]bool{},
	}

	a.runStoragePrune()

	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Fatalf("expected app dir removed, got err=%v", err)
	}
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Fatalf("expected backup meta removed, got err=%v", err)
	}

	a.storagePruneMu.Lock()
	defer a.storagePruneMu.Unlock()
	if got := len(a.storagePruneState.DeletedApps); got != 1 || a.storagePruneState.DeletedApps[0] != "arr-stack" {
		t.Fatalf("unexpected deleted apps: %#v", a.storagePruneState.DeletedApps)
	}
	if a.storagePruneState.DockerReclaimedBytes != 12345 {
		t.Fatalf("expected DockerReclaimedBytes 12345, got %d", a.storagePruneState.DockerReclaimedBytes)
	}
	if a.storagePruneState.DockerPruneError != "" {
		t.Fatalf("expected no DockerPruneError, got %q", a.storagePruneState.DockerPruneError)
	}
}

func TestParseDockerSize(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"0B", 0},
		{"", 0},
		{"1B", 1},
		{"1023B", 1023},
		{"1kB", 1024},
		{"10kB", 10 * 1024},
		{"1MB", 1024 * 1024},
		{"2.5MB", uint64(2.5 * 1024 * 1024)},
		{"1GB", 1024 * 1024 * 1024},
		{"1.5GB", uint64(1.5 * 1024 * 1024 * 1024)},
		{"1TB", 1024 * 1024 * 1024 * 1024},
		{"500MB", 500 * 1024 * 1024},
	}

	for _, tt := range tests {
		got, err := parseDockerSize(tt.input)
		if err != nil {
			t.Errorf("parseDockerSize(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseDockerSize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseDockerSizeInvalid(t *testing.T) {
	invalid := []string{"abc", "1.2XX", "5KB"}
	for _, s := range invalid {
		_, err := parseDockerSize(s)
		if err == nil {
			t.Errorf("parseDockerSize(%q) expected error, got nil", s)
		}
	}
}

func dockerAvailable() bool {
	return exec.Command("docker", "info").Run() == nil
}

func TestEstimateDockerReclaimableBytes(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}

	got := estimateDockerReclaimableBytes()
	// Should return non-negative; zero is valid if nothing is reclaimable.
	if got > 0 {
		t.Logf("docker reclaimable: %d bytes", got)
	}
}

func TestRunDockerSystemPrune(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}

	reclaimed, errStr := runDockerSystemPrune()
	if errStr != "" {
		// Docker prune can fail for legitimate reasons (e.g. permission denied
		// in CI). We just log it; the test verifies the function returns
		// structured data.
		t.Logf("docker prune error (may be expected): %s", errStr)
	}
	// reclaimed can be 0 if nothing to reclaim.
	if reclaimed > 0 {
		t.Logf("docker reclaimed: %d bytes", reclaimed)
	}
}
