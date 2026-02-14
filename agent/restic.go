package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/jatinparab/homectl/common"
)

// ProgressCallback is called with progress updates during restic operations
type ProgressCallback func(*common.ResticProgress)

// Restic interface for backup/restore operations
type Restic interface {
	Restore(appID common.AppID, onProgress ProgressCallback) error
	Backup(appID common.AppID, onProgress ProgressCallback) error
}

// RepoRestic runs restic commands against a B2 repo
type RepoRestic struct {
	repo       string
	appDataDir string
	useSudo    bool
}

// NewRepoRestic creates a new RepoRestic instance
func NewRepoRestic(appDataDir string, useSudo bool) *RepoRestic {
	return &RepoRestic{
		repo:       "b2:labctl",
		appDataDir: appDataDir,
		useSudo:    useSudo,
	}
}

// resticCmd builds a restic command, prepending sudo if configured.
// RESTIC_PROGRESS_FPS is set explicitly via env(1) so it survives sudo's env_reset.
func (r *RepoRestic) resticCmd(args ...string) *exec.Cmd {
	if r.useSudo {
		cmdArgs := append([]string{"-E", "env", "RESTIC_PROGRESS_FPS=2", "restic"}, args...)
		return exec.Command("sudo", cmdArgs...)
	}
	cmd := exec.Command("restic", args...)
	cmd.Env = append(cmd.Environ(), "RESTIC_PROGRESS_FPS=2")
	return cmd
}

// resticStatusLine represents a JSON status line from restic --json output.
type resticStatusLine struct {
	MessageType   string  `json:"message_type"`
	PercentDone   float64 `json:"percent_done"`
	TotalFiles    uint64  `json:"total_files"`
	TotalBytes    uint64  `json:"total_bytes"`
	// Restore fields
	BytesRestored uint64 `json:"bytes_restored"`
	FilesRestored uint64 `json:"files_restored"`
	// Backup fields
	BytesDone uint64 `json:"bytes_done"`
	FilesDone uint64 `json:"files_done"`
}

// runWithJSON runs a restic command with --json output, parsing status lines
// and calling onProgress. Stderr is captured and returned for error inspection.
func (r *RepoRestic) runWithJSON(prefix string, cmd *exec.Cmd, onProgress ProgressCallback) (stderrLines []string, err error) {
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
			log.Printf("[restic:%s:err] %s", prefix, line)
		}
	}()

	// Parse stdout JSON lines
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		var status resticStatusLine
		if err := json.Unmarshal([]byte(line), &status); err != nil {
			// Not valid JSON, just log it
			log.Printf("[restic:%s] %s", prefix, line)
			continue
		}

		if status.MessageType == "status" && onProgress != nil {
			// Normalize restore vs backup fields
			bytesDone := status.BytesDone
			if bytesDone == 0 {
				bytesDone = status.BytesRestored
			}
			filesDone := status.FilesDone
			if filesDone == 0 {
				filesDone = status.FilesRestored
			}

			onProgress(&common.ResticProgress{
				PercentDone: status.PercentDone,
				TotalBytes:  status.TotalBytes,
				BytesDone:   bytesDone,
				TotalFiles:  status.TotalFiles,
				FilesDone:   filesDone,
			})
		} else if status.MessageType == "summary" {
			log.Printf("[restic:%s] completed", prefix)
		} else {
			log.Printf("[restic:%s] %s", prefix, line)
		}
	}

	<-stderrDone
	return stderrLines, cmd.Wait()
}

func (r *RepoRestic) Restore(appID common.AppID, onProgress ProgressCallback) error {
	cmd := r.resticCmd(
		"-r", r.repo,
		"--json",
		"restore",
		"--target", r.appDataDir,
		"--tag", fmt.Sprintf("%s,homectl", string(appID)),
		fmt.Sprintf("--include=/%s", string(appID)),
		"latest",
	)

	stderrLines, err := r.runWithJSON("restore:"+string(appID), cmd, onProgress)

	// If restore failed, check if it's because there are no snapshots (valid case)
	if err != nil {
		for _, line := range stderrLines {
			if containsNoSnapshotError(line) {
				log.Printf("Restore for %s: no snapshots available (valid case, continuing startup)", appID)
				return nil
			}
		}
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

func (r *RepoRestic) Backup(appID common.AppID, onProgress ProgressCallback) error {
	cmd := r.resticCmd(
		"-r", r.repo,
		"--json",
		"--tag", "homectl", "--tag", string(appID),
		"backup", string(appID),
	)
	cmd.Dir = r.appDataDir
	_, err := r.runWithJSON("backup:"+string(appID), cmd, onProgress)
	return err
}
