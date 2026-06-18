package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

type StoragePruneState struct {
	Busy                 bool      `json:"busy"`
	StartedAt            time.Time `json:"started_at,omitempty"`
	FinishedAt           time.Time `json:"finished_at,omitempty"`
	LastError            string    `json:"last_error,omitempty"`
	DeletedApps          []string  `json:"deleted_apps,omitempty"`
	DeletedDirs          []string  `json:"deleted_dirs,omitempty"`
	DeletedBytes         uint64    `json:"deleted_bytes,omitempty"`
	DockerReclaimedBytes uint64    `json:"docker_reclaimed_bytes,omitempty"`
	DockerPruneError     string    `json:"docker_prune_error,omitempty"`
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

	reclaimable += estimateDockerReclaimableBytesFunc()

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
	a.storagePruneState.DockerReclaimedBytes = 0
	a.storagePruneState.DockerPruneError = ""
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
			if err := removePathWithSudoFallback(backupMetaPath(a.stateDir, common.AppID(name)), a.stateDir); err != nil {
				lastErr = err.Error()
			}

			deletedApps = append(deletedApps, name)
			deletedDirs = append(deletedDirs, target)
			deletedBytes += sz

			a.runtimeMu.Lock()
			delete(a.runtime, common.AppID(name))
			delete(a.assignedApps, common.AppID(name))
			a.runtimeMu.Unlock()
		}
	}

	sort.Strings(deletedApps)
	sort.Strings(deletedDirs)

	// Docker prune
	dockerReclaimed, dockerErr := dockerSystemPruneFunc()

	a.storagePruneMu.Lock()
	a.storagePruneState.Busy = false
	a.storagePruneState.FinishedAt = time.Now()
	a.storagePruneState.LastError = lastErr
	a.storagePruneState.DeletedApps = deletedApps
	a.storagePruneState.DeletedDirs = deletedDirs
	a.storagePruneState.DeletedBytes = deletedBytes
	a.storagePruneState.DockerReclaimedBytes = dockerReclaimed
	a.storagePruneState.DockerPruneError = dockerErr
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

// estimateDockerReclaimableBytesFunc and dockerSystemPruneFunc are
// package-level variables so tests can replace them with no-ops.
var (
	estimateDockerReclaimableBytesFunc = estimateDockerReclaimableBytes
	dockerSystemPruneFunc              = runDockerSystemPrune
)

// ── Docker reclaimable helpers ──────────────────────────────────────────

// parseDockerSize parses a Docker size string like "1.234GB", "500MB",
// "10kB", or "0B" into bytes. Docker uses base-2 (1024) units.
func parseDockerSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0, nil
	}

	// Find the boundary between the numeric part and the unit suffix.
	var numPart, unit string
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' {
			continue
		}
		numPart = s[:i]
		unit = s[i:]
		break
	}

	if numPart == "" {
		return 0, fmt.Errorf("invalid docker size: %q", s)
	}

	value, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid docker size %q: %w", s, err)
	}

	var multiplier uint64 = 1
	switch unit {
	case "B":
		multiplier = 1
	case "kB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	case "TB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown docker size unit: %q (from %q)", unit, s)
	}

	return uint64(value * float64(multiplier)), nil
}

// dfEntry is a single line from "docker system df --format '{{json .}}'".
type dfEntry struct {
	Type        string `json:"Type"`
	Reclaimable string `json:"Reclaimable"`
}

// estimateDockerReclaimableBytes runs docker system df and sums the
// Reclaimable sizes across all resource types. Returns 0 if Docker is
// unavailable or parsing fails.
func estimateDockerReclaimableBytes() uint64 {
	cmd := exec.Command("docker", "system", "df", "--format", "{{json .}}")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0
	}
	if err := cmd.Start(); err != nil {
		return 0
	}

	var total uint64
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var e dfEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if e.Reclaimable == "" || e.Reclaimable == "0B" {
			continue
		}
		bytes, err := parseDockerSize(e.Reclaimable)
		if err != nil {
			continue
		}
		total += bytes
	}

	_ = cmd.Wait()
	return total
}

// runDockerSystemPrune runs "docker system prune -a --volumes -f" and
// parses the "Total reclaimed space" line from the last line of stdout.
func runDockerSystemPrune() (reclaimedBytes uint64, errStr string) {
	cmd := exec.Command("docker", "system", "prune", "-a", "--volumes", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Sprintf("docker system prune failed: %s", strings.TrimSpace(string(out)))
	}

	// The last line contains the total: "Total reclaimed space: 1.234GB"
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return 0, ""
	}
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	const prefix = "Total reclaimed space: "
	if !strings.HasPrefix(lastLine, prefix) {
		return 0, ""
	}
	sizeStr := strings.TrimPrefix(lastLine, prefix)
	bytes, err := parseDockerSize(sizeStr)
	if err != nil {
		return 0, ""
	}
	return bytes, ""
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
