package manager

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

type repoStateFile struct {
	LastCheckAt      time.Time `json:"last_check_at,omitempty"`
	LastUnlockAt     time.Time `json:"last_unlock_at,omitempty"`
	LastCheckErr     string    `json:"last_check_err,omitempty"`
	LastUnlockErr    string    `json:"last_unlock_err,omitempty"`
	LastStatusSyncAt time.Time `json:"last_status_sync_at,omitempty"`
}

func (m *Manager) repoStatePath() string {
	return filepath.Join(m.stateDir, "repo-state.json")
}

func (m *Manager) loadRepoState() {
	data, err := os.ReadFile(m.repoStatePath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Failed to read repo-state file: %s", err)
		}
		return
	}

	var st repoStateFile
	if err := json.Unmarshal(data, &st); err != nil {
		log.Printf("Failed to parse repo-state file: %s", err)
		return
	}

	m.repoMu.Lock()
	m.repoLastCheckAt = st.LastCheckAt
	m.repoLastUnlockAt = st.LastUnlockAt
	m.repoLastCheckErr = st.LastCheckErr
	m.repoLastUnlockErr = st.LastUnlockErr
	m.repoLastStatusSyncAt = st.LastStatusSyncAt
	m.repoMu.Unlock()
}

func (m *Manager) saveRepoState() {
	m.repoMu.Lock()
	st := repoStateFile{
		LastCheckAt:      m.repoLastCheckAt,
		LastUnlockAt:     m.repoLastUnlockAt,
		LastCheckErr:     m.repoLastCheckErr,
		LastUnlockErr:    m.repoLastUnlockErr,
		LastStatusSyncAt: m.repoLastStatusSyncAt,
	}
	m.repoMu.Unlock()

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		log.Printf("Failed to create state dir: %s", err)
		return
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal repo-state: %s", err)
		return
	}

	if err := os.WriteFile(m.repoStatePath(), data, 0644); err != nil {
		log.Printf("Failed to write repo-state file: %s", err)
	}
}

func (m *Manager) isRepoBusy() bool {
	m.repoMu.Lock()
	defer m.repoMu.Unlock()
	return m.repoCheckBusy || m.repoUnlockBusy
}
