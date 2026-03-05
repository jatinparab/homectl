package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

type StoragePruneState struct {
	Busy         bool      `json:"busy"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	DeletedApps  []string  `json:"deleted_apps,omitempty"`
	DeletedDirs  []string  `json:"deleted_dirs,omitempty"`
	DeletedBytes uint64    `json:"deleted_bytes,omitempty"`
}

type StorageStatusResponse struct {
	Node        string            `json:"node"`
	AppdataPath string            `json:"appdata_path"`
	DiskTotal   uint64            `json:"disk_total"`
	DiskUsed    uint64            `json:"disk_used"`
	DiskFree    uint64            `json:"disk_free"`
	DiskPercent float64           `json:"disk_percent"`
	Reclaimable uint64            `json:"reclaimable"`
	ReclaimPct  float64           `json:"reclaim_pct"`
	Prune       StoragePruneState `json:"prune"`
}

func (a *Agent) getStorageStatus() StorageStatusResponse {
	total, used, free, pct := readDiskUsage(a.appdataDir)
	reclaimable := a.estimateReclaimableBytes()
	reclaimPct := 0.0
	if total > 0 {
		reclaimPct = float64(reclaimable) / float64(total) * 100
	}

	a.storagePruneMu.Lock()
	prune := a.storagePruneState
	a.storagePruneMu.Unlock()

	return StorageStatusResponse{
		Node:        a.name,
		AppdataPath: a.appdataDir,
		DiskTotal:   total,
		DiskUsed:    used,
		DiskFree:    free,
		DiskPercent: pct,
		Reclaimable: reclaimable,
		ReclaimPct:  reclaimPct,
		Prune:       prune,
	}
}

func (a *Agent) estimateReclaimableBytes() uint64 {
	assigned := make(map[string]bool)
	a.runtimeMu.RLock()
	for appID, isAssigned := range a.assignedApps {
		if isAssigned {
			assigned[string(appID)] = true
		}
	}
	a.runtimeMu.RUnlock()

	entries, err := os.ReadDir(a.appdataDir)
	if err != nil {
		return 0
	}

	var reclaimable uint64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || assigned[name] {
			continue
		}
		sz, _ := dirSize(filepath.Join(a.appdataDir, name))
		reclaimable += sz
	}
	return reclaimable
}

func (a *Agent) startStoragePrune() bool {
	a.storagePruneMu.Lock()
	if a.storagePruneState.Busy {
		a.storagePruneMu.Unlock()
		return false
	}
	a.storagePruneState.Busy = true
	a.storagePruneState.StartedAt = time.Now()
	a.storagePruneState.FinishedAt = time.Time{}
	a.storagePruneState.LastError = ""
	a.storagePruneState.DeletedApps = nil
	a.storagePruneState.DeletedDirs = nil
	a.storagePruneState.DeletedBytes = 0
	a.storagePruneMu.Unlock()

	go a.runStoragePrune()
	return true
}

func (a *Agent) runStoragePrune() {
	var (
		deletedApps  []string
		deletedDirs  []string
		deletedBytes uint64
		lastErr      string
	)

	assigned := make(map[string]bool)
	a.runtimeMu.RLock()
	for appID, isAssigned := range a.assignedApps {
		if isAssigned {
			assigned[string(appID)] = true
		}
	}
	a.runtimeMu.RUnlock()

	entries, err := os.ReadDir(a.appdataDir)
	if err != nil {
		lastErr = err.Error()
	} else {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if assigned[name] {
				continue
			}

			target := filepath.Join(a.appdataDir, name)
			sz, _ := dirSize(target)
			if err := removePathWithSudoFallback(target, a.appdataDir); err != nil {
				lastErr = err.Error()
				continue
			}

			deletedApps = append(deletedApps, name)
			deletedDirs = append(deletedDirs, target)
			deletedBytes += sz

			_ = removePathWithSudoFallback(backupMetaPath(a.appdataDir, common.AppID(name)), filepath.Join(a.appdataDir, ".homectl-meta"))

			a.runtimeMu.Lock()
			delete(a.runtime, common.AppID(name))
			delete(a.assignedApps, common.AppID(name))
			a.runtimeMu.Unlock()
		}
	}

	sort.Strings(deletedApps)
	sort.Strings(deletedDirs)

	a.storagePruneMu.Lock()
	a.storagePruneState.Busy = false
	a.storagePruneState.FinishedAt = time.Now()
	a.storagePruneState.LastError = lastErr
	a.storagePruneState.DeletedApps = deletedApps
	a.storagePruneState.DeletedDirs = deletedDirs
	a.storagePruneState.DeletedBytes = deletedBytes
	a.storagePruneMu.Unlock()
}

func removePathWithSudoFallback(targetPath string, allowedRoot string) error {
	if err := os.RemoveAll(targetPath); err == nil {
		return nil
	}

	cleanTarget := filepath.Clean(targetPath)
	cleanRoot := filepath.Clean(allowedRoot)
	if cleanTarget != cleanRoot && !strings.HasPrefix(cleanTarget, cleanRoot+string(filepath.Separator)) {
		return fmt.Errorf("refusing to delete outside allowed root: %s", cleanTarget)
	}

	cmd := exec.Command("sudo", "-n", "rm", "-rf", "--", cleanTarget)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("sudo delete failed for %s: %s", cleanTarget, msg)
	}

	return nil
}

func dirSize(root string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += uint64(info.Size())
		return nil
	})
	return total, err
}

// handleStorageStatus returns current disk and prune status.
// GET /storage/status
func (rs *ReadyServer) handleStorageStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rs.agent.getStorageStatus())
}

// handleStoragePrune triggers async prune of unassigned appdata directories.
// POST /storage/prune
func (rs *ReadyServer) handleStoragePrune(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !rs.agent.startStoragePrune() {
		http.Error(w, "prune already running", http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "prune_started"})
}
