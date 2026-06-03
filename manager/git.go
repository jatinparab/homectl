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

// GitSync syncs the repository checkout to its configured remote upstream.
func (m *Manager) GitSync() error {
	if err := m.ensureGitCheckout(); err != nil {
		return err
	}

	target, err := m.remoteSyncTarget()
	if err != nil {
		return err
	}

	if output, err := gitCommand("-C", m.repoDir, "fetch", "--prune").CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %s, output: %s", err, string(output))
	}
	if output, err := gitCommand("-C", m.repoDir, "reset", "--hard", target).CombinedOutput(); err != nil {
		return fmt.Errorf("git reset failed: %s, output: %s", err, string(output))
	}
	if output, err := gitCommand("-C", m.repoDir, "clean", "-fdx").CombinedOutput(); err != nil {
		return fmt.Errorf("git clean failed: %s, output: %s", err, string(output))
	}

	gitLog.Debug("Git sync completed", "target", target)

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
		cmd := gitCommand("clone", repoURL, m.repoDir)
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

func (m *Manager) remoteSyncTarget() (string, error) {
	output, err := gitCommand("-C", m.repoDir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}").CombinedOutput()
	if err == nil {
		target := strings.TrimSpace(string(output))
		if target != "" {
			return target, nil
		}
	}

	branchOutput, branchErr := gitCommand("-C", m.repoDir, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if branchErr != nil {
		return "", fmt.Errorf("git upstream lookup failed: %s, output: %s", err, string(output))
	}
	branch := strings.TrimSpace(string(branchOutput))
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("git upstream lookup failed: no upstream configured and checkout is detached")
	}

	return "origin/" + branch, nil
}

func gitCommand(args ...string) *exec.Cmd {
	gitArgs := make([]string, 0, len(args)+4)
	if gitToken() != "" {
		gitArgs = append(gitArgs,
			"-c", "credential.helper=!f() { echo username=${HOMECTL_GIT_USERNAME:-x-access-token}; echo password=$HOMECTL_GIT_TOKEN; }; f",
		)
	}
	gitArgs = append(gitArgs, args...)

	cmd := exec.Command("git", gitArgs...)
	cmd.Env = gitEnv()
	return cmd
}

func gitEnv() []string {
	env := os.Environ()
	env = append(env, "GIT_TERMINAL_PROMPT=0")

	token := gitToken()
	if token != "" && os.Getenv("HOMECTL_GIT_TOKEN") == "" {
		env = append(env, "HOMECTL_GIT_TOKEN="+token)
	}

	return env
}

func gitToken() string {
	for _, name := range []string{"HOMECTL_GIT_TOKEN", "HOMECTL_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			return token
		}
	}
	return ""
}
