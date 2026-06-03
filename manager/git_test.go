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

func TestGitSyncResetsDirtyCheckoutToRemote(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	bare := filepath.Join(root, "source.git")
	checkout := filepath.Join(root, "checkout")

	runGit(t, root, "init", "-b", "main", source)
	runGit(t, source, "config", "user.email", "test@example.invalid")
	runGit(t, source, "config", "user.name", "Test User")
	writeFile(t, filepath.Join(source, ".gitignore"), "*.ignored\n")
	writeFile(t, filepath.Join(source, "docker", "example-app", "docker-compose.yml"), "version: first\n")
	runGit(t, source, "add", ".")
	runGit(t, source, "commit", "-m", "initial")
	runGit(t, root, "clone", "--bare", source, bare)

	t.Setenv("HOMECTL_GIT_REPO", bare)
	m := NewManager(checkout, 1338)
	if err := m.ensureGitCheckout(); err != nil {
		t.Fatalf("ensureGitCheckout: %v", err)
	}

	trackedFile := filepath.Join(checkout, "docker", "example-app", "docker-compose.yml")
	writeFile(t, trackedFile, "version: local\n")
	writeFile(t, filepath.Join(checkout, "local.txt"), "local\n")
	writeFile(t, filepath.Join(checkout, "secret.ignored"), "ignored\n")

	runGit(t, source, "remote", "add", "origin", bare)
	writeFile(t, filepath.Join(source, "docker", "example-app", "docker-compose.yml"), "version: remote\n")
	runGit(t, source, "add", ".")
	runGit(t, source, "commit", "-m", "remote update")
	runGit(t, source, "push", "origin", "main")

	if err := m.GitSync(); err != nil {
		t.Fatalf("GitSync: %v", err)
	}

	contents, err := os.ReadFile(trackedFile)
	if err != nil {
		t.Fatalf("read tracked file: %v", err)
	}
	if string(contents) != "version: remote\n" {
		t.Fatalf("expected remote contents, got %q", string(contents))
	}
	if _, err := os.Stat(filepath.Join(checkout, "local.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected untracked file to be removed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "secret.ignored")); !os.IsNotExist(err) {
		t.Fatalf("expected ignored file to be removed, got %v", err)
	}
}

func TestGitCommandUsesTokenCredentialHelper(t *testing.T) {
	t.Setenv("HOMECTL_GIT_TOKEN", "secret-token")
	cmd := gitCommand("pull", "--ff-only")

	joinedArgs := strings.Join(cmd.Args, " ")
	if !strings.Contains(joinedArgs, "credential.helper=") {
		t.Fatalf("expected credential helper in args, got %v", cmd.Args)
	}
	if strings.Contains(joinedArgs, "secret-token") {
		t.Fatalf("git args leaked token: %v", cmd.Args)
	}

	joinedEnv := strings.Join(cmd.Env, "\n")
	if !strings.Contains(joinedEnv, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("expected non-interactive git env, got %v", cmd.Env)
	}
}

func TestGitCommandMapsGitHubTokenToHomectlGitToken(t *testing.T) {
	t.Setenv("HOMECTL_GIT_TOKEN", "")
	t.Setenv("HOMECTL_GITHUB_TOKEN", "github-secret")

	cmd := gitCommand("pull", "--ff-only")
	joinedEnv := strings.Join(cmd.Env, "\n")
	if !strings.Contains(joinedEnv, "HOMECTL_GIT_TOKEN=github-secret") {
		t.Fatalf("expected HOMECTL_GITHUB_TOKEN to be mapped for git helper, got %v", cmd.Env)
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

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
