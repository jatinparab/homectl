package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jatinparab/homectl/common"
)

// BackupMeta is persisted to disk alongside app data so backup timestamps
// survive agent restarts.
type BackupMeta struct {
	LastBackupAt time.Time `json:"last_backup_at"`
	SnapshotID   string    `json:"snapshot_id"`
	Node         string    `json:"node"`
}

// backupMetaPath returns the path to the metadata file for an app.
func backupMetaPath(stateDir string, appID common.AppID) string {
	return filepath.Join(stateDir, "backup-meta", string(appID)+".json")
}

// loadBackupMeta reads the metadata file. Returns nil, nil if the file does not exist.
func loadBackupMeta(stateDir string, appID common.AppID) (*BackupMeta, error) {
	data, err := os.ReadFile(backupMetaPath(stateDir, appID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read backup meta: %w", err)
	}

	var meta BackupMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse backup meta: %w", err)
	}
	return &meta, nil
}

// writeBackupMeta writes the metadata file, creating parent directories as needed.
func writeBackupMeta(stateDir string, appID common.AppID, meta *BackupMeta) error {
	p := backupMetaPath(stateDir, appID)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir for backup meta: %w", err)
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal backup meta: %w", err)
	}

	if err := os.WriteFile(p, data, 0644); err != nil {
		return fmt.Errorf("write backup meta: %w", err)
	}
	return nil
}
