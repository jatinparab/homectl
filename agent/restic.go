package agent

import (
	"log"

	"github.com/jatinparab/homectl/common"
)

// Restic interface for backup/restore operations
type Restic interface {
	Restore(appID common.AppID) error
	Backup(appID common.AppID) error
}

// DummyRestic is a stub implementation that just logs
type DummyRestic struct{}

// NewDummyRestic creates a new DummyRestic instance
func NewDummyRestic() *DummyRestic {
	return &DummyRestic{}
}

// Restore logs that it would restore and returns nil
func (r *DummyRestic) Restore(appID common.AppID) error {
	log.Printf("Restic: would restore %s (stub)", appID)
	return nil
}

// Backup logs that it would backup and returns nil
func (r *DummyRestic) Backup(appID common.AppID) error {
	log.Printf("Restic: would backup %s (stub)", appID)
	return nil
}
