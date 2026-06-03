package manager

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var gitLog = slog.With("area", "git")

// GitSync performs a git pull to sync the repository
func (m *Manager) GitSync() error {
	if err := m.ensureGitCheckout(); err != nil {
		return err
	}

	cmd := exec.Command("git", "-C", m.repoDir, "pull", "--ff-only")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %s, output: %s", err, string(output))
	}

	gitLog.Debug("Git sync completed", "output", strings.TrimSpace(string(output)))

	// Decrypt secrets after sync (.env.enc → .env files)
	decryptScript := filepath.Join(m.repoDir, "scripts", "decrypt-secrets")
	if _, err := os.Stat(decryptScript); err == nil {
		decryptCmd := exec.Command(decryptScript)
		decryptCmd.Dir = m.repoDir
		decryptOutput, err := decryptCmd.CombinedOutput()
		if err != nil {
			gitLog.Info("Warning: decrypt-secrets failed", "err", err, "output", string(decryptOutput))
		} else {
			gitLog.Debug("Secrets decrypted", "output", strings.TrimSpace(string(decryptOutput)))
		}
	}

	return nil
}

func (m *Manager) ensureGitCheckout() error {
	repoURL := strings.TrimSpace(os.Getenv("HOMECTL_GIT_REPO"))
	if repoURL == "" {
		return fmt.Errorf("HOMECTL_GIT_REPO is required")
	}
	if _, err := os.Stat(m.repoDir); os.IsNotExist(err) {
		parent := filepath.Dir(m.repoDir)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}
		cmd := exec.Command("git", "clone", repoURL, m.repoDir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git clone failed: %s, output: %s", err, string(output))
		}
		gitLog.Info("Git repository cloned", "repo", repoURL, "dir", m.repoDir)
		return nil
	}

	gitDir := filepath.Join(m.repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("not a git repository: %s", m.repoDir)
	}
	return nil
}
