package manager

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleFiles_NormalFileServed(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodGet, "/files/test.txt", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "hello world" {
		t.Fatalf("expected 'hello world', got %q", w.Body.String())
	}
}

func TestHandleFiles_NonExistentFileReturns404(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodGet, "/files/nope.txt", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleFiles_DotdotTraversalBlocked(t *testing.T) {
	tmpDir := t.TempDir()

	outsideFile := filepath.Join(tmpDir, "..", "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("should not be served"), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodGet, "/files/../outside.txt", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Fatalf("expected 403, 404, or 400 for traversal attempt, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleFiles_SymlinkToOutsideRepoBlocked(t *testing.T) {
	tmpDir := t.TempDir()

	outsideDir := filepath.Join(tmpDir, "..", "outside_data")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	linkPath := filepath.Join(tmpDir, "leak")
	if err := os.Symlink(secretFile, linkPath); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodGet, "/files/leak", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (symlink escape blocked), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleFiles_SymlinkToInsideRepoAllowed(t *testing.T) {
	tmpDir := t.TempDir()

	realFile := filepath.Join(tmpDir, "real.txt")
	if err := os.WriteFile(realFile, []byte("via symlink"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink("real.txt", linkPath); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodGet, "/files/link.txt", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid symlink inside repo, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "via symlink" {
		t.Fatalf("expected 'via symlink', got %q", w.Body.String())
	}
}

func TestHandleFiles_WrongMethodReturns405(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodPost, "/files/test.txt", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", w.Code)
	}
}

func TestHandleFiles_EmptyPathReturns400(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodGet, "/files/", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty path, got %d", w.Code)
	}
}

func TestHandleFiles_SymlinkChainEscapeBlocked(t *testing.T) {
	tmpDir := t.TempDir()

	outsideDir := filepath.Join(tmpDir, "..", "escape_chain")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("very secret"), 0644); err != nil {
		t.Fatal(err)
	}

	link1Path := filepath.Join(tmpDir, "link1")
	link2Path := filepath.Join(tmpDir, "link2")
	if err := os.Symlink(link2Path, link1Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretFile, link2Path); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodGet, "/files/link1", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (chained symlink escape blocked), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleFiles_SymlinkDirTraversalBlocked(t *testing.T) {
	tmpDir := t.TempDir()

	outsideDir := filepath.Join(tmpDir, "..", "outside_dir")
	if err := os.MkdirAll(filepath.Join(outsideDir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(outsideDir, "nested", "secret.txt")
	if err := os.WriteFile(secretFile, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	linkDir := filepath.Join(tmpDir, "outside_link")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodGet, "/files/outside_link/nested/secret.txt", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (dir symlink escape blocked), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleFiles_ValidDirSymlinkAllowed(t *testing.T) {
	tmpDir := t.TempDir()

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("in subdir"), 0644); err != nil {
		t.Fatal(err)
	}

	linkDir := filepath.Join(tmpDir, "sub_link")
	if err := os.Symlink("subdir", linkDir); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	req := httptest.NewRequest(http.MethodGet, "/files/sub_link/file.txt", nil)
	w := httptest.NewRecorder()
	m.handleFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid dir symlink, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "in subdir" {
		t.Fatalf("expected 'in subdir', got %q", w.Body.String())
	}
}

func TestHandleFiles_MethodNotAllowedForMutation(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/files/test.txt", bytes.NewReader([]byte("data")))
		w := httptest.NewRecorder()
		m.handleFiles(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("handleFiles(%s) = %d, want %d", method, w.Code, http.StatusMethodNotAllowed)
		}
	}
}

// --- Auth requirement tests ---

// TestIsAuthExempt verifies the isAuthExempt logic for various paths.
func TestIsAuthExempt(t *testing.T) {
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	tests := []struct {
		path   string
		exempt bool
	}{
		{"/health", true},
		{"/api/join", true},
		{"/auth/login", true},
		{"/auth/logout", true},
		{"/api/v1/auth/me", true},
		{"/api/v1/agents", false},
		{"/agents/sync", false},
		{"/docker/sync", false},
		{"/sync", false},
		{"/files/", false},
		{"/files/test.txt", false},
		{"/static/app.js", true},
		{"/css/style.css", true},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		got := m.isAuthExempt(req)
		if got != tt.exempt {
			t.Errorf("isAuthExempt(%q) = %v, want %v", tt.path, got, tt.exempt)
		}
	}
}

// TestIsAuthExemptOnboarding verifies onboarding paths are exempt when onboarding is required.
func TestIsAuthExemptOnboarding(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// Setup onboarding so OnboardingRequired() returns true
	m.auth.onboardingMu.Lock()
	m.auth.onboardingTokenHash = tokenHash("token")
	m.auth.onboardingMu.Unlock()

	tests := []struct {
		path   string
		exempt bool
	}{
		{"/auth/onboarding/status", true},
		{"/auth/onboarding/complete", true},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		got := m.isAuthExempt(req)
		if got != tt.exempt {
			t.Errorf("isAuthExempt(%q) = %v, want %v", tt.path, got, tt.exempt)
		}
	}
}

// TestRequireAuthBlocksNonExempt verifies that non-exempt paths require auth.
func TestRequireAuthBlocksNonExempt(t *testing.T) {
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// /api/ should require auth
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	m.requireAuth(http.NewServeMux()).ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatal("expected /api/ to require authentication")
	}

	// /files/ should require auth
	req = httptest.NewRequest(http.MethodGet, "/files/test.txt", nil)
	w = httptest.NewRecorder()
	m.requireAuth(http.NewServeMux()).ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatal("expected /files/ to require authentication")
	}
}

// TestRequireAuthExemptPaths verifies that exempt paths bypass auth.
func TestRequireAuthExemptPaths(t *testing.T) {
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// Exempt paths should NOT be redirected to login (i.e. should not get 401/302)
	// Since the underlying mux has no handler, we expect 404, NOT 401/302.
	exemptPaths := []string{"/health", "/auth/login", "/auth/logout"}
	for _, path := range exemptPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		m.requireAuth(http.NewServeMux()).ServeHTTP(w, req)
		if w.Code == http.StatusUnauthorized || w.Code == http.StatusFound {
			t.Errorf("exempt path %s should not require auth (got %d)", path, w.Code)
		}
	}
}

// TestHandleFiles_RequiresAuthentication verifies /files/ returns 401 without a session.
func TestHandleFiles_RequiresAuthentication(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	auth, err := NewAuthService(context.Background(), store.Users, store.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	m := &Manager{
		auth: auth,
	}

	req := httptest.NewRequest(http.MethodGet, "/files/test.txt", nil)
	w := httptest.NewRecorder()
	m.requireAuth(http.NewServeMux()).ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("expected /files/ to require authentication, got 200")
	}
}

// TestHandleFiles_AuthenticatedAccessAllowed verifies /files/ works with a valid session.
func TestHandleFiles_AuthenticatedAccessAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	auth, err := NewAuthService(context.Background(), store.Users, store.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Create a user directly (bypassing onboarding, which needs an onboarding token)
	pwHash, err := hashPassword("very-secure-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	user, err := store.Users.CreateUser(context.Background(), "admin", pwHash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_ = user

	// Login to get a session
	token, _, _, err := auth.Login(context.Background(), "admin", "very-secure-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(tmpDir, "data.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a minimal manager with auth
	m := &Manager{
		auth:    auth,
		repoDir: tmpDir,
	}

	req := httptest.NewRequest(http.MethodGet, "/files/data.txt", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	w := httptest.NewRecorder()

	// Use requireAuth wrapping the file handler
	handler := m.requireAuth(http.HandlerFunc(m.handleFiles))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated access, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "hello" {
		t.Fatalf("expected 'hello', got %q", w.Body.String())
	}
}
