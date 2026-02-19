package manager

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jatinparab/homectl/common"
)

type PruneState struct {
	LastPruneAt    time.Time `json:"last_prune_at,omitempty"`
	LastPruneError string    `json:"last_prune_error,omitempty"`
}

func (m *Manager) pruneStatePath() string {
	return filepath.Join(m.stateDir, "prune-state.json")
}

func (m *Manager) loadPruneState() {
	data, err := os.ReadFile(m.pruneStatePath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Failed to read prune-state file: %s", err)
		}
		return
	}

	var raw map[string]PruneState
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Printf("Failed to parse prune-state file: %s", err)
		return
	}

	m.backupMu.Lock()
	defer m.backupMu.Unlock()
	m.pruneState = make(map[common.AppID]PruneState, len(raw))
	for appID, st := range raw {
		m.pruneState[common.AppID(appID)] = st
	}
}

func (m *Manager) savePruneState() {
	m.backupMu.Lock()
	raw := make(map[string]PruneState, len(m.pruneState))
	for appID, st := range m.pruneState {
		raw[string(appID)] = st
	}
	m.backupMu.Unlock()

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		log.Printf("Failed to create state dir: %s", err)
		return
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal prune-state: %s", err)
		return
	}

	if err := os.WriteFile(m.pruneStatePath(), data, 0644); err != nil {
		log.Printf("Failed to write prune-state file: %s", err)
	}
}
