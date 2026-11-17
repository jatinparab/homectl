package manager

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionCookieName = "homectl_session"

var authLog = slog.With("area", "auth")

type AuthService struct {
	users    UserRepository
	sessions SessionRepository
	apiKeys  APIKeyRepository

	onboardingMu        sync.RWMutex
	onboardingRequired  bool
	onboardingTokenHash string
}

type currentUserContextKey struct{}

func NewAuthService(ctx context.Context, users UserRepository, sessions SessionRepository, apiKeys APIKeyRepository) (*AuthService, error) {
	return newAuthService(ctx, users, sessions, apiKeys, nil)
}

func newAuthService(ctx context.Context, users UserRepository, sessions SessionRepository, apiKeys APIKeyRepository, deliverOnboardingToken func(string) error) (*AuthService, error) {
	count, err := users.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	svc := &AuthService{
		users:               users,
		sessions:            sessions,
		apiKeys:             apiKeys,
		onboardingRequired:  count == 0,
		onboardingTokenHash: "",
	}
	if count == 0 {
		token, err := randomToken(32)
		if err != nil {
			return nil, err
		}
		svc.onboardingTokenHash = tokenHash(token)
		if deliverOnboardingToken != nil {
			if err := deliverOnboardingToken(token); err != nil {
				return nil, fmt.Errorf("deliver onboarding token: %w", err)
			}
		}
	}
	return svc, nil
}

func (a *AuthService) OnboardingRequired() bool {
	if a == nil {
		return false
	}
	a.onboardingMu.RLock()
	defer a.onboardingMu.RUnlock()
	return a.onboardingRequired
}

func (a *AuthService) CompleteOnboarding(ctx context.Context, token, username, password, confirm string) (User, error) {
	if a == nil {
		return User{}, fmt.Errorf("auth service unavailable")
	}
	username = normalizeUsername(username)
	if username == "" {
		return User{}, fmt.Errorf("username is required")
	}
	if len(password) < 12 {
		return User{}, fmt.Errorf("password must be at least 12 characters")
	}
	if password != confirm {
		return User{}, fmt.Errorf("password confirmation does not match")
	}

	a.onboardingMu.Lock()
	defer a.onboardingMu.Unlock()
	if !a.onboardingRequired {
		return User{}, fmt.Errorf("onboarding is not available")
	}
	if subtle.ConstantTimeCompare([]byte(tokenHash(token)), []byte(a.onboardingTokenHash)) != 1 {
		return User{}, fmt.Errorf("invalid onboarding token")
	}
	count, err := a.users.CountUsers(ctx)
	if err != nil {
		return User{}, err
	}
	if count != 0 {
		a.onboardingRequired = false
		a.onboardingTokenHash = ""
		return User{}, fmt.Errorf("onboarding is not available")
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	user, err := a.users.CreateUser(ctx, username, passwordHash)
	if err != nil {
		return User{}, err
	}
	a.onboardingRequired = false
	a.onboardingTokenHash = ""
	authLog.Info("First user onboarding completed", "username", user.Username)
	return user, nil
}

func (a *AuthService) Login(ctx context.Context, username, password string) (string, time.Time, User, error) {
	if a == nil {
		return "", time.Time{}, User{}, fmt.Errorf("auth service unavailable")
	}
	user, err := a.users.GetByUsername(ctx, normalizeUsername(username))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", time.Time{}, User{}, fmt.Errorf("invalid username or password")
		}
		return "", time.Time{}, User{}, err
	}
	if user.Disabled {
		return "", time.Time{}, User{}, fmt.Errorf("user is disabled")
	}
	ok, err := verifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		return "", time.Time{}, User{}, fmt.Errorf("invalid username or password")
	}
	token, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, User{}, err
	}
	expires := time.Now().UTC().Add(7 * 24 * time.Hour)
	if _, err := a.sessions.CreateSession(ctx, user.ID, tokenHash(token), expires); err != nil {
		return "", time.Time{}, User{}, err
	}
	return token, expires, user, nil
}

func (a *AuthService) Logout(ctx context.Context, token string) error {
	if a == nil || token == "" {
		return nil
	}
	return a.sessions.DeleteSessionByTokenHash(ctx, tokenHash(token))
}

func (a *AuthService) Authenticate(ctx context.Context, token string) (User, error) {
	if a == nil || token == "" {
		return User{}, ErrNotFound
	}
	session, err := a.sessions.GetSessionByTokenHash(ctx, tokenHash(token))
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC()
	if !session.ExpiresAt.After(now) {
		_ = a.sessions.DeleteSessionByTokenHash(ctx, session.TokenHash)
		return User{}, ErrNotFound
	}
	user, err := a.users.GetByID(ctx, session.UserID)
	if err != nil {
		return User{}, err
	}
	if user.Disabled {
		return User{}, ErrNotFound
	}
	_ = a.sessions.TouchSession(ctx, session.ID, now)
	return user, nil
}

func (a *AuthService) AuthenticateByAPIKey(ctx context.Context, rawKey string) (User, error) {
	if a == nil || rawKey == "" || a.apiKeys == nil {
		return User{}, ErrNotFound
	}
	keyHash := tokenHash(rawKey)
	apiKey, err := a.apiKeys.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		return User{}, err
	}
	user, err := a.users.GetByID(ctx, apiKey.UserID)
	if err != nil {
		return User{}, err
	}
	if user.Disabled {
		return User{}, ErrNotFound
	}
	_ = a.apiKeys.TouchAPIKey(ctx, apiKey.ID, time.Now().UTC())
	return user, nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func withCurrentUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, currentUserContextKey{}, user)
}

func currentUser(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(currentUserContextKey{}).(User)
	return user, ok
}

type authStatusResponse struct {
	Authenticated      bool   `json:"authenticated"`
	OnboardingRequired bool   `json:"onboarding_required"`
	Username           string `json:"username,omitempty"`
}

func (m *Manager) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := authStatusResponse{OnboardingRequired: m.auth.OnboardingRequired()}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if user, err := m.auth.Authenticate(r.Context(), cookie.Value); err == nil {
			resp.Authenticated = true
			resp.Username = user.Username
		}
	}
	if !resp.Authenticated {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			rawKey := strings.TrimPrefix(authHeader, "Bearer ")
			if user, err := m.auth.AuthenticateByAPIKey(r.Context(), rawKey); err == nil {
				resp.Authenticated = true
				resp.Username = user.Username
			}
		}
	}
	writeJSON(w, resp)
}

func (m *Manager) handleOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, authStatusResponse{OnboardingRequired: m.auth.OnboardingRequired()})
}

func (m *Manager) handleOnboardingComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Token           string `json:"token"`
		Username        string `json:"username"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid onboarding request", http.StatusBadRequest)
		return
	}
	user, err := m.auth.CompleteOnboarding(r.Context(), req.Token, req.Username, req.Password, req.ConfirmPassword)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	expireSessionCookie(w, r)
	if m.runtimeCtx != nil {
		authLog.Info("Starting manager runtime after first user onboarding", "username", user.Username)
		m.StartRuntimeOnce(m.runtimeCtx)
	}
	writeJSON(w, map[string]any{"username": user.Username})
}

func (m *Manager) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if m.auth.OnboardingRequired() {
		http.Error(w, "onboarding required", http.StatusConflict)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid login request", http.StatusBadRequest)
		return
	}

	// Rate limit login attempts per IP
	if !m.loginRateLimiter.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "Too many login attempts, try again later", http.StatusTooManyRequests)
		return
	}

	token, expires, user, err := m.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	setSessionCookie(w, r, token, expires)
	writeJSON(w, map[string]any{"username": user.Username})
}

func (m *Manager) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = m.auth.Logout(r.Context(), cookie.Value)
	}
	expireSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.isAuthExempt(r) {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && strings.TrimSpace(cookie.Value) != "" {
			user, err := m.auth.Authenticate(r.Context(), cookie.Value)
			if err == nil {
				m.applyRateLimit(w, r, user)
				if w.Header().Get("Retry-After") != "" {
					return
				}
				next.ServeHTTP(w, r.WithContext(withCurrentUser(r.Context(), user)))
				return
			}
		}

		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			rawKey := strings.TrimPrefix(authHeader, "Bearer ")
			if rawKey != "" {
				user, err := m.auth.AuthenticateByAPIKey(r.Context(), rawKey)
				if err == nil {
					m.applyRateLimit(w, r, user)
					if w.Header().Get("Retry-After") != "" {
						return
					}
					next.ServeHTTP(w, r.WithContext(withCurrentUser(r.Context(), user)))
					return
				}
			}
		}

		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func (m *Manager) applyRateLimit(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodGet {
		if !m.mutatingActionLimiter.Allow(user.Username) {
			w.Header().Set("Retry-After", "30")
			http.Error(w, "Too many requests, try again later", http.StatusTooManyRequests)
		}
	}
}

func (m *Manager) isAuthExempt(r *http.Request) bool {
	path := r.URL.Path
	if path == "/health" || path == "/api/join" {
		return true
	}
	if path == "/auth/login" || path == "/auth/logout" || path == "/api/v1/auth/me" {
		return true
	}
	if m.auth != nil && m.auth.OnboardingRequired() && (path == "/auth/onboarding/status" || path == "/auth/onboarding/complete") {
		return true
	}
	if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/agents") && !strings.HasPrefix(path, "/files/") && path != "/docker/sync" && path != "/sync" {
		return true
	}
	return false
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
}

func expireSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
