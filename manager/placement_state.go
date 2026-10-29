package manager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

type appPlacement struct {
	Agent  string `json:"agent"`
	Manual bool   `json:"manual,omitempty"`
}

type placementSnapshot struct {
	Version     int                     `json:"version"`
	Assignments map[string]appPlacement `json:"assignments"`
}

func (m *Manager) placementsPath() string {
	return filepath.Join(m.stateDir, "placements.json")
}

// Load before opening the agent listener. Discovery applies entries only to
// newly registered singleton apps; later syncs must not undo live decisions.
func (m *Manager) loadPlacements() {
	data, err := os.ReadFile(m.placementsPath())
	if err != nil {
		if !os.IsNotExist(err) {
			managerLog.Error("Failed to read placements; recovering from agent reports", "err", err)
		}
		return
	}
	var snapshot placementSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil || snapshot.Version != 1 || snapshot.Assignments == nil {
		managerLog.Error("Invalid placement snapshot; recovering from agent reports", "err", err)
		return
	}
	for app, placement := range snapshot.Assignments {
		if strings.TrimSpace(app) == "" || strings.TrimSpace(placement.Agent) == "" {
			managerLog.Error("Invalid placement snapshot entry; recovering from agent reports")
			return
		}
	}
	m.restoredPlacements = snapshot.Assignments
	m.hasPlacementSnapshot = true
}

func (m *Manager) recoveringPlacements(now time.Time) bool {
	return now.Before(m.placementRecoveryUntil)
}

func (m *Manager) hasUnplacedRunningApps() bool {
	for _, app := range m.appRegistry.ListAll() {
		if !app.IsGlobal && app.DesiredState == common.DesiredRunning && app.AssignedAgent == nil {
			return true
		}
	}
	return false
}

// Reports from a backup/restore interrupted by a manager restart still identify
// an owner, even though its containers are temporarily stopped. Empty initial
// reports and old local data alone must never establish ownership.
func reportOwnsWorkload(report common.AppReport) bool {
	if report.ObservedState == common.ObservedRunning || report.BackupBusy || report.PruneBusy || report.PostBackupPhase != "" {
		return true
	}
	switch report.Phase {
	case common.PhaseBackupStopping, common.PhaseBackupStopped, common.PhaseBackingUp, common.PhaseRestoring, common.PhasePulling:
		return true
	}
	return false
}

// Snapshot all placement mutation paths (scheduler, heartbeat adoption, API).
// Serializing before taking a fresh registry snapshot prevents a slower writer
// from overwriting a newer decision. Unchanged heartbeats do not write to disk.
func (m *Manager) savePlacements() {
	if !m.appsDiscovered.Load() {
		return // never replace a complete snapshot with partial discovery
	}
	m.placementSaveMu.Lock()
	defer m.placementSaveMu.Unlock()
	snapshot := placementSnapshot{Version: 1, Assignments: make(map[string]appPlacement)}
	for _, app := range m.appRegistry.ListAll() {
		if !app.IsGlobal && app.AssignedAgent != nil {
			snapshot.Assignments[string(app.ID)] = appPlacement{Agent: *app.AssignedAgent, Manual: app.ManuallyAssigned}
		}
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err == nil && bytes.Equal(data, m.lastPlacementData) {
		return
	}
	if err == nil {
		err = writePlacementSnapshot(m.placementsPath(), data)
	}
	if err != nil {
		managerLog.Error("Failed to persist app placements", "err", err)
		return
	}
	m.lastPlacementData = data
}

func writePlacementSnapshot(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".placements-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace placement snapshot: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
