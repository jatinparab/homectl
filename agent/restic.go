package agent

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"

	"github.com/jatinparab/homectl/common"
)

// Restic interface for backup/restore operations
type Restic interface {
	Restore(appID common.AppID) error
	Backup(appID common.AppID) error
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
func (r *RepoRestic) resticCmd(args ...string) *exec.Cmd {
	if r.useSudo {
		return exec.Command("sudo", append([]string{"-E", "restic"}, args...)...)
	}
	return exec.Command("restic", args...)
}

// runStreaming runs a command and logs stdout/stderr lines in real time.
func (r *RepoRestic) runStreaming(prefix string, cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	// Merge stdout and stderr into one reader
	merged := io.MultiReader(stdout, stderr)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	scanner := bufio.NewScanner(merged)
	for scanner.Scan() {
		log.Printf("[restic:%s] %s", prefix, scanner.Text())
	}

	return cmd.Wait()
}

func (r *RepoRestic) Restore(appId common.AppID) error {
	cmd := r.resticCmd(
		"-r", r.repo,
		"--verbose",
		"restore",
		"--target", r.appDataDir,
		"--tag", fmt.Sprintf("%s,homectl", string(appId)),
		fmt.Sprintf("--include=/%s", string(appId)),
		"latest",
	)
	return r.runStreaming("restore:"+string(appId), cmd)
}

// cmd = [
//                 "sudo",
//                 "-E",
//                 "restic",
//                 "-r",
//                 self.restic_repo,
//                 "--verbose",
//                 "--tag",
//                 "homectl",
//                 "--tag",
//                 app_name,
//                 "backup",
//                 app_name,  # Use relative path instead of absolute
//             ]

//             returncode, _, _ = await self._run_restic_command(
//                 cmd, stream_logs=True, cwd=str(self.appdata_dir), app_name=app_name
//             )

func (r *RepoRestic) Backup(appID common.AppID) error {
	cmd := r.resticCmd(
		"-r", r.repo,
		"--verbose",
		"--tag", "homectl", "--tag", string(appID),
		"backup", string(appID),
	)
	cmd.Dir = r.appDataDir
	return r.runStreaming("backup:"+string(appID), cmd)
}
