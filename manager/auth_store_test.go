package manager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestAuthServiceOnboardingLoginAndSession(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	auth, err := NewAuthService(context.Background(), store.Users, store.Sessions)
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	if !auth.OnboardingRequired() {
		t.Fatal("expected onboarding before first user")
	}
	if _, err := auth.CompleteOnboarding(context.Background(), "bad-token", "admin", "very-secure-password", "very-secure-password"); err == nil {
		t.Fatal("expected invalid onboarding token to fail")
	}

	auth.onboardingMu.Lock()
	auth.onboardingTokenHash = tokenHash("setup-token")
	auth.onboardingMu.Unlock()

	if _, err := auth.CompleteOnboarding(context.Background(), "setup-token", "Admin", "very-secure-password", "very-secure-password"); err != nil {
		t.Fatalf("CompleteOnboarding: %v", err)
	}
	if auth.OnboardingRequired() {
		t.Fatal("expected onboarding to be disabled")
	}
	if _, _, _, err := auth.Login(context.Background(), "admin", "wrong-password"); err == nil {
		t.Fatal("expected wrong password to fail")
	}
	sessionToken, expires, user, err := auth.Login(context.Background(), "admin", "very-secure-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if sessionToken == "" || !expires.After(time.Now()) || user.Username != "admin" {
		t.Fatalf("unexpected login result: token=%q expires=%v user=%+v", sessionToken, expires, user)
	}
	got, err := auth.Authenticate(context.Background(), sessionToken)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("expected user %d, got %d", user.ID, got.ID)
	}
	if err := auth.Logout(context.Background(), sessionToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := auth.Authenticate(context.Background(), sessionToken); err == nil {
		t.Fatal("expected logged out session to fail")
	}
}

func TestAgentConfigServiceEncryptsAtRest(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	t.Setenv("HOMECTL_CONFIG_ENCRYPTION_KEY", base64.RawStdEncoding.EncodeToString(key))
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	svc, err := NewAgentConfigService(store.AgentConfigs)
	if err != nil {
		t.Fatalf("NewAgentConfigService: %v", err)
	}

	plain := json.RawMessage(`{"feature":true}`)
	rec, err := svc.Upsert(context.Background(), "node-1", plain)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if string(rec.Ciphertext) == string(plain) {
		t.Fatal("expected ciphertext at rest")
	}
	resp, err := svc.GetPlaintext(context.Background(), "node-1")
	if err != nil {
		t.Fatalf("GetPlaintext: %v", err)
	}
	if resp.Version != 1 || string(resp.Config) != string(plain) {
		t.Fatalf("unexpected config response: %+v", resp)
	}
}
