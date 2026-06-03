package manager

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitCheckoutRequiresRepoURL(t *testing.T) {
	t.Setenv("HOMECTL_GIT_REPO", "")
	m := NewManager(filepath.Join(t.TempDir(), "repo"), 1338)

	err := m.ensureGitCheckout()
	if err == nil || !strings.Contains(err.Error(), "HOMECTL_GIT_REPO is required") {
		t.Fatalf("expected missing repo error, got %v", err)
	}
}

func TestEnsureGitCheckoutClonesMissingRepoDir(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	bare := filepath.Join(root, "source.git")
	checkout := filepath.Join(root, "checkout")

	runGit(t, root, "init", source)
	runGit(t, source, "config", "user.email", "test@example.invalid")
	runGit(t, source, "config", "user.name", "Test User")
	if err := os.MkdirAll(filepath.Join(source, "docker", "example-app"), 0755); err != nil {
		t.Fatalf("mkdir source app dir: %v", err)
	}
	runGit(t, source, "add", ".")
	runGit(t, source, "commit", "--allow-empty", "-m", "initial")
	runGit(t, root, "clone", "--bare", source, bare)

	t.Setenv("HOMECTL_GIT_REPO", bare)
	m := NewManager(checkout, 1338)
	if err := m.ensureGitCheckout(); err != nil {
		t.Fatalf("ensureGitCheckout: %v", err)
	}
	if _, err := exec.Command("git", "-C", checkout, "rev-parse", "--is-inside-work-tree").Output(); err != nil {
		t.Fatalf("checkout is not a git worktree: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
