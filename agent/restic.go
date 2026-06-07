package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

var backupLog = slog.With("area", "backup")

// ProgressCallback is called with progress updates during restic operations
type ProgressCallback func(*common.ResticProgress)

// LogCallback is called with log lines during restic operations
type LogCallback func(string)

// Snapshot represents a restic snapshot
type Snapshot struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	Tags     []string  `json:"tags"`
}

// SnapshotCache holds cached snapshot data for an app
type SnapshotCache struct {
	Snapshots []Snapshot `json:"snapshots"`
	CachedAt  time.Time  `json:"cached_at"`
	Loading   bool       `json:"loading"`
	Error     string     `json:"error,omitempty"`
}

// SnapshotEntry represents a file or directory inside a snapshot
type SnapshotEntry struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // "file" or "dir"
	Path  string `json:"path"`
	Size  uint64 `json:"size,omitempty"`
	MTime string `json:"mtime,omitempty"`
}

// Restic interface for backup/restore operations
type Restic interface {
	Restore(ctx context.Context, appID common.AppID, onProgress ProgressCallback, onLog LogCallback) error
	Backup(ctx context.Context, appID common.AppID, onProgress ProgressCallback, onLog LogCallback) error
	Prune(ctx context.Context, appID common.AppID, onLog LogCallback) error
	Check(ctx context.Context, onLog LogCallback) error
	Unlock(ctx context.Context, onLog LogCallback) error
	ForceUnlock(ctx context.Context, onLog LogCallback) error
	LatestSnapshot(ctx context.Context, appID common.AppID) (snapshotTime time.Time, snapshotID string, err error)
	ListSnapshots(ctx context.Context, appID common.AppID) ([]Snapshot, error)
	ListSnapshotFiles(ctx context.Context, snapshotID, path string) ([]SnapshotEntry, error)
}

// RepoRestic runs restic commands against a B2 repo
type RepoRestic struct {
	appDataDir string
	useSudo    bool
	valueFunc  func(string) string
}

// NewRepoRestic creates a new RepoRestic instance.
func NewRepoRestic(appDataDir string, useSudo bool, valueFunc func(string) string) *RepoRestic {
	return &RepoRestic{
		appDataDir: appDataDir,
		useSudo:    useSudo,
		valueFunc:  valueFunc,
	}
}

func (r *RepoRestic) configValue(key string) string {
	if r.valueFunc != nil {
		return strings.TrimSpace(r.valueFunc(key))
	}
	return ""
}

func (r *RepoRestic) repoURL() string {
	if repo := r.configValue(common.ConfigKeyResticRepository); repo != "" {
		return repo
	}
	return "restic-repository-not-configured"
}

func (r *RepoRestic) resticRuntimeEnv() []string {
	env := []string{"RESTIC_PROGRESS_FPS=2"}
	for _, key := range []string{common.ConfigKeyB2AccountID, common.ConfigKeyB2AccountKey, common.ConfigKeyResticPassword} {
		if val := r.configValue(key); val != "" {
			env = append(env, fmt.Sprintf("%s=%s", key, val))
		}
	}
	return env
}

// resticCmdCtx builds a context-aware restic command (killed when ctx is cancelled).
// Required env vars are passed explicitly via env(1) so sudo env_reset cannot strip them.
func (r *RepoRestic) resticCmdCtx(ctx context.Context, args ...string) *exec.Cmd {
	runtimeEnv := r.resticRuntimeEnv()
	if r.useSudo {
		cmdArgs := []string{
			"--preserve-env=B2_ACCOUNT_ID,B2_ACCOUNT_KEY,RESTIC_PASSWORD,RESTIC_PROGRESS_FPS",
			"env",
			"RESTIC_PROGRESS_FPS=2",
			"restic",
		}
		cmdArgs = append(cmdArgs, args...)
		cmd := exec.CommandContext(ctx, "sudo", cmdArgs...)
		// Provide manager-synced vars for sudo to preserve from the caller env.
		cmd.Env = append(sanitizedAgentEnviron(), runtimeEnv...)
		return cmd
	}
	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = append(sanitizedAgentEnviron(), runtimeEnv...)
	return cmd
}

// resticStatusLine represents a JSON status line from restic --json output.
type resticStatusLine struct {
	MessageType string  `json:"message_type"`
	PercentDone float64 `json:"percent_done"`
	TotalFiles  uint64  `json:"total_files"`
	TotalBytes  uint64  `json:"total_bytes"`
	// Restore fields
	BytesRestored uint64 `json:"bytes_restored"`
	FilesRestored uint64 `json:"files_restored"`
	// Backup fields
	BytesDone uint64 `json:"bytes_done"`
	FilesDone uint64 `json:"files_done"`
}

// runWithJSON runs a restic command with --json output, parsing status lines
// and calling onProgress. Stderr is captured and returned for error inspection.
func (r *RepoRestic) runWithJSON(prefix string, cmd *exec.Cmd, onProgress ProgressCallback, onLog LogCallback) (stderrLines []string, err error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	// Read stderr in background
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrLines = append(stderrLines, line)
			backupLog.Debug(line, "op", prefix)
			if onLog != nil {
				onLog(line)
			}
		}
	}()

	// Parse stdout JSON lines
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		var status resticStatusLine
		if err := json.Unmarshal([]byte(line), &status); err != nil {
			// Not valid JSON, just log it
			backupLog.Debug(line, "op", prefix)
			if onLog != nil {
				onLog(line)
			}
			continue
		}

		if status.MessageType == "status" {
			// Normalize restore vs backup fields
			bytesDone := status.BytesDone
			if bytesDone == 0 {
				bytesDone = status.BytesRestored
			}
			filesDone := status.FilesDone
			if filesDone == 0 {
				filesDone = status.FilesRestored
			}

			progress := &common.ResticProgress{
				PercentDone: status.PercentDone,
				TotalBytes:  status.TotalBytes,
				BytesDone:   bytesDone,
				TotalFiles:  status.TotalFiles,
				FilesDone:   filesDone,
			}
			if onProgress != nil {
				onProgress(progress)
			}
			if onLog != nil {
				progressJSON, _ := json.Marshal(progress)
				onLog("__PROGRESS__:" + string(progressJSON))
			}
		} else if status.MessageType == "summary" {
			backupLog.Info("restic completed", "op", prefix)
			if onLog != nil {
				onLog("Completed")
			}
		} else {
			backupLog.Debug(line, "op", prefix)
			if onLog != nil {
				onLog(line)
			}
		}
	}

	<-stderrDone
	return stderrLines, cmd.Wait()
}

func (r *RepoRestic) Restore(ctx context.Context, appID common.AppID, onProgress ProgressCallback, onLog LogCallback) error {
	cmd := r.resticCmdCtx(ctx,
		"-r", r.repoURL(),
		"--retry-lock", "2m",
		"--json",
		"restore",
		"--target", r.appDataDir,
		"--delete",
		"--tag", fmt.Sprintf("%s,homectl", string(appID)),
		fmt.Sprintf("--include=/%s", string(appID)),
		"latest",
	)

	stderrLines, err := r.runWithJSON("restore:"+string(appID), cmd, onProgress, onLog)

	// If restore failed, check if it's because there are no snapshots (valid case)
	if err != nil {
		for _, line := range stderrLines {
			if containsNoSnapshotError(line) {
				backupLog.Info("No snapshots available for restore (valid case, continuing startup)", "app", appID)
				return nil
			}
		}
		r.tryUnlockOnLockConflict(ctx, "restore", err, onLog)
		return err
	}

	return nil
}

// containsNoSnapshotError checks if a line contains "no snapshots found" or similar restic error
func containsNoSnapshotError(line string) bool {
	noSnapshotPatterns := []string{
		"no snapshots",
		"no snapshot",
		"could not find a snapshot",
		"snapshot not found",
	}

	for _, pattern := range noSnapshotPatterns {
		if strings.Contains(strings.ToLower(line), strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

func (r *RepoRestic) Backup(ctx context.Context, appID common.AppID, onProgress ProgressCallback, onLog LogCallback) error {
	cmd := r.resticCmdCtx(ctx,
		"-r", r.repoURL(),
		"--retry-lock", "2m",
		"--json",
		"--tag", "homectl", "--tag", string(appID),
		"backup", string(appID),
	)
	cmd.Dir = r.appDataDir
	_, err := r.runWithJSON("backup:"+string(appID), cmd, onProgress, onLog)
	if err != nil {
		r.tryUnlockOnLockConflict(ctx, "backup", err, onLog)
	}
	return err
}

// runLines streams stdout and stderr of cmd line-by-line to onLog.
func (r *RepoRestic) runLines(prefix string, cmd *exec.Cmd, onLog LogCallback) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	scan := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			backupLog.Debug(line, "op", prefix)
			if onLog != nil {
				onLog(line)
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); scan(stdout) }()
	go func() { defer wg.Done(); scan(stderr) }()
	wg.Wait()

	return cmd.Wait()
}

// Prune removes old snapshots for an app using restic forget --prune.
// Retention policy: 6 hourly, 7 daily, 5 weekly, 12 monthly, 3 yearly.
// --retry-lock handles B2 eventual-consistency: the previous backup's lock may
// still be visible in the backend for a short period after the backup exits.
func (r *RepoRestic) Prune(ctx context.Context, appID common.AppID, onLog LogCallback) error {
	cmd := r.resticCmdCtx(ctx,
		"-r", r.repoURL(),
		"--retry-lock", "5m",
		"forget", "--prune",
		"--group-by", "tags",
		"--tag", fmt.Sprintf("%s,homectl", string(appID)),
		"--keep-hourly", "6",
		"--keep-daily", "7",
		"--keep-weekly", "5",
		"--keep-monthly", "12",
		"--keep-yearly", "3",
	)
	err := r.runLines("prune:"+string(appID), cmd, onLog)
	if err != nil {
		r.tryUnlockOnLockConflict(ctx, "prune", err, onLog)
	}
	return err
}

// Check verifies the integrity of the restic repository.
// --retry-lock handles transient lock conflicts from concurrent operations.
func (r *RepoRestic) Check(ctx context.Context, onLog LogCallback) error {
	cmd := r.resticCmdCtx(ctx, "-r", r.repoURL(), "--retry-lock", "5m", "check", "--read-data-subset=1%")
	err := r.runLines("check", cmd, onLog)
	if err != nil {
		r.tryUnlockOnLockConflict(ctx, "check", err, onLog)
	}
	return err
}

func (r *RepoRestic) tryUnlockOnLockConflict(ctx context.Context, op string, runErr error, onLog LogCallback) {
	if !isExitCode11(runErr) {
		return
	}
	msg := fmt.Sprintf("Restic %s failed with lock conflict (exit code 11); running restic unlock once", op)
	backupLog.Info("restic lock conflict, attempting unlock", "op", op)
	if onLog != nil {
		onLog(msg)
	}
	if err := r.Unlock(ctx, onLog); err != nil {
		backupLog.Info("restic unlock failed after lock conflict", "op", op, "err", err)
		if onLog != nil {
			onLog("restic unlock failed after lock conflict: " + err.Error())
		}
	}
}

func isExitCode11(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == 11
}

// Unlock removes stale locks from the restic repository.
func (r *RepoRestic) Unlock(ctx context.Context, onLog LogCallback) error {
	cmd := r.resticCmdCtx(ctx, "-r", r.repoURL(), "unlock")
	return r.runLines("unlock", cmd, onLog)
}

// ForceUnlock removes all locks from the restic repository, including locks held by other clients.
func (r *RepoRestic) ForceUnlock(ctx context.Context, onLog LogCallback) error {
	cmd := r.resticCmdCtx(ctx, "-r", r.repoURL(), "unlock", "--remove-all")
	return r.runLines("force-unlock", cmd, onLog)
}

// resticSnapshot represents a snapshot entry from restic snapshots --json
type resticSnapshot struct {
	ID   string    `json:"short_id"`
	Time time.Time `json:"time"`
}

// LatestSnapshot returns the timestamp and ID of the latest snapshot for an app.
// Returns zero time + empty string if no snapshots exist (not an error).
func (r *RepoRestic) LatestSnapshot(ctx context.Context, appID common.AppID) (time.Time, string, error) {
	cmd := r.resticCmdCtx(ctx,
		"-r", r.repoURL(),
		"snapshots",
		"--json",
		"--latest", "1",
		"--tag", fmt.Sprintf("%s,homectl", string(appID)),
	)

	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, "", fmt.Errorf("restic snapshots: %w", err)
	}

	var snapshots []resticSnapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return time.Time{}, "", fmt.Errorf("parse snapshots json: %w", err)
	}

	if len(snapshots) == 0 {
		return time.Time{}, "", nil
	}

	// --latest 1 returns one snapshot per host, so sort by time descending
	// and return the newest one.
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Time.After(snapshots[j].Time)
	})

	return snapshots[0].Time, snapshots[0].ID, nil
}

// ListSnapshots returns all snapshots for an app, sorted newest-first.
func (r *RepoRestic) ListSnapshots(ctx context.Context, appID common.AppID) ([]Snapshot, error) {
	cmd := r.resticCmdCtx(ctx,
		"-r", r.repoURL(),
		"snapshots",
		"--json",
		"--tag", fmt.Sprintf("%s,homectl", string(appID)),
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("restic snapshots: %w", err)
	}

	var snapshots []Snapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return nil, fmt.Errorf("parse snapshots json: %w", err)
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Time.After(snapshots[j].Time)
	})

	return snapshots, nil
}

// resticLsEntry is the raw JSON output from restic ls --json
type resticLsEntry struct {
	Type  string `json:"type"` // "file", "dir", "snapshot"
	Path  string `json:"path"`
	Name  string `json:"name"`
	Size  uint64 `json:"size"`
	MTime string `json:"mtime"`
}

// ListSnapshotFiles lists files in a snapshot at the given path, returning direct children only.
func (r *RepoRestic) ListSnapshotFiles(ctx context.Context, snapshotID, path string) ([]SnapshotEntry, error) {
	if path == "" {
		path = "/"
	}
	// Ensure path ends with /
	if path[len(path)-1] != '/' {
		path += "/"
	}

	cmd := r.resticCmdCtx(ctx,
		"-r", r.repoURL(),
		"ls",
		"--json",
		snapshotID,
		path,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("restic ls: %w", err)
	}

	// restic ls outputs one JSON object per line (not a JSON array)
	seen := make(map[string]bool)
	var entries []SnapshotEntry

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		var entry resticLsEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		// Skip the snapshot metadata line
		if entry.Type == "snapshot" {
			continue
		}
		// Filter to direct children of path
		if !strings.HasPrefix(entry.Path, path) {
			continue
		}
		relative := strings.TrimPrefix(entry.Path, path)
		// Direct child: no slashes (or a single trailing slash for dirs)
		relative = strings.TrimSuffix(relative, "/")
		if relative == "" || strings.Contains(relative, "/") {
			continue
		}
		if seen[relative] {
			continue
		}
		seen[relative] = true

		entryType := "file"
		if entry.Type == "dir" {
			entryType = "dir"
		}
		entries = append(entries, SnapshotEntry{
			Name:  relative,
			Type:  entryType,
			Path:  entry.Path,
			Size:  entry.Size,
			MTime: entry.MTime,
		})
	}

	// Sort: dirs first, then alphabetical
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "dir"
		}
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}
