package manager

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitSync performs a git pull to sync the repository
func (m *Manager) GitSync() error {
	if _, err := os.Stat(m.repoDir); os.IsNotExist(err) {
		return fmt.Errorf("repository directory does not exist: %s", m.repoDir)
	}

	gitDir := filepath.Join(m.repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("not a git repository: %s", m.repoDir)
	}

	cmd := exec.Command("git", "-C", m.repoDir, "pull", "--ff-only")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %s, output: %s", err, string(output))
	}

	log.Printf("Git sync completed: %s", strings.TrimSpace(string(output)))
	return nil
}
