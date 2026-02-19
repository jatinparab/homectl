package agent

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

type repoOpsStateFile struct {
	CheckLastAt     time.Time `json:"check_last_at,omitempty"`
	CheckLastError  string    `json:"check_last_error,omitempty"`
	UnlockLastAt    time.Time `json:"unlock_last_at,omitempty"`
	UnlockLastError string    `json:"unlock_last_error,omitempty"`
}

func (a *Agent) repoOpsStatePath() string {
	return filepath.Join(a.appdataDir, ".homectl", "repo-ops.json")
}

func (a *Agent) loadRepoOpsState() {
	data, err := os.ReadFile(a.repoOpsStatePath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Repo ops: failed to read state: %s", err)
		}
		return
	}

	var st repoOpsStateFile
	if err := json.Unmarshal(data, &st); err != nil {
		log.Printf("Repo ops: failed to parse state: %s", err)
		return
	}

	if a.repoCheck != nil {
		a.repoCheck.mu.Lock()
		a.repoCheck.LastAt = st.CheckLastAt
		a.repoCheck.LastError = st.CheckLastError
		a.repoCheck.mu.Unlock()
	}
	if a.repoUnlock != nil {
		a.repoUnlock.mu.Lock()
		a.repoUnlock.LastAt = st.UnlockLastAt
		a.repoUnlock.LastError = st.UnlockLastError
		a.repoUnlock.mu.Unlock()
	}
}

func (a *Agent) saveRepoOpsState() {
	var st repoOpsStateFile
	if a.repoCheck != nil {
		a.repoCheck.mu.Lock()
		st.CheckLastAt = a.repoCheck.LastAt
		st.CheckLastError = a.repoCheck.LastError
		a.repoCheck.mu.Unlock()
	}
	if a.repoUnlock != nil {
		a.repoUnlock.mu.Lock()
		st.UnlockLastAt = a.repoUnlock.LastAt
		st.UnlockLastError = a.repoUnlock.LastError
		a.repoUnlock.mu.Unlock()
	}

	stateDir := filepath.Dir(a.repoOpsStatePath())
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		log.Printf("Repo ops: failed to create state dir: %s", err)
		return
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		log.Printf("Repo ops: failed to marshal state: %s", err)
		return
	}

	if err := os.WriteFile(a.repoOpsStatePath(), data, 0644); err != nil {
		log.Printf("Repo ops: failed to write state: %s", err)
	}
}
