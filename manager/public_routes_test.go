package manager

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func TestGenerateManagerCaddyfileRoutesHomelabToGateway(t *testing.T) {
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.test.example")
	m := NewManager(t.TempDir(), 1338)
	m.leaseManager.Heartbeat("akira", "10.42.1.101", nil, common.NodeMetrics{})
	if err := m.saveWireGuardPeers([]wireGuardPeerState{
		{Name: "akira", PublicKey: "test-key", Address: "10.44.0.7"},
	}); err != nil {
		t.Fatalf("saveWireGuardPeers: %v", err)
	}
	m.gatewayMu.Lock()
	m.gatewayNode = "akira"
	m.gatewayMu.Unlock()
	m.publicRoutes = []PublicRoute{
		{
			ID:        "photos-example-com",
			Host:      "photos.example.com",
			Mode:      PublicRouteModeHomelab,
			Enabled:   true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	caddyfile, err := m.GenerateManagerCaddyfile()
	if err != nil {
		t.Fatalf("GenerateManagerCaddyfile: %v", err)
	}

	assertContains(t, caddyfile, "homectl.test.example {")
	assertContains(t, caddyfile, "photos.example.com {")
	assertContains(t, caddyfile, "reverse_proxy https://10.44.0.7 {")
	assertContains(t, caddyfile, "header_up Host {host}")
	assertContains(t, caddyfile, "tls_server_name photos.example.com")
}

func TestGenerateManagerCaddyfileRoutesCustomWithoutHostOverride(t *testing.T) {
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.test.example")
	m := NewManager(t.TempDir(), 1338)
	m.publicRoutes = []PublicRoute{
		{
			ID:           "docs-example-com",
			Host:         "docs.example.com",
			Mode:         PublicRouteModeCustom,
			CustomTarget: "https://example.com",
			Enabled:      true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	caddyfile, err := m.GenerateManagerCaddyfile()
	if err != nil {
		t.Fatalf("GenerateManagerCaddyfile: %v", err)
	}

	assertContains(t, caddyfile, "docs.example.com {")
	assertContains(t, caddyfile, "reverse_proxy https://example.com {")
	if strings.Contains(caddyfile, "header_up Host {host}") {
		t.Fatalf("custom route should not override upstream Host header:\n%s", caddyfile)
	}
}

func TestGenerateAgentCaddyConfigRoutesPublicAndInternalHosts(t *testing.T) {
	t.Setenv("HOMECTL_DOMAIN_SUFFIX", "sulphurgfx.com")
	t.Setenv("CADDY_DNS_PROVIDER", "cloudflare")
	m := NewManager(t.TempDir(), 1338)
	m.leaseManager.Heartbeat("akira", "10.44.0.7", []common.AppReport{
		{
			AppID:         "photos",
			ObservedState: common.ObservedRunning,
			Health:        common.HealthHealthy,
			Port:          2283,
		},
	}, common.NodeMetrics{})

	caddyfile, err := m.GenerateCaddyConfig("akira")
	if err != nil {
		t.Fatalf("GenerateCaddyConfig: %v", err)
	}

	assertContains(t, caddyfile, "*.sulphurgfx.com, *.internal.sulphurgfx.com {")
	assertContains(t, caddyfile, "@route0 host photos.sulphurgfx.com photos.internal.sulphurgfx.com")
	assertContains(t, caddyfile, "import homectl_tls")
	assertContains(t, caddyfile, "dns cloudflare {env.CLOUDFLARE_API_TOKEN}")
	assertContains(t, caddyfile, "reverse_proxy http://homectl-photos:2283")
}

func TestRequirePublicHostBasicAuthSkipsAPIPaths(t *testing.T) {
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "secret")
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.example.com")

	m := NewManager(t.TempDir(), 1338)
	called := false
	handler := m.requirePublicHostBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://homectl.example.com/api/v1/ws/state", nil)
	req.Host = "homectl.example.com"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("expected API request to bypass basic auth")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}

func TestRequirePublicHostBasicAuthSkipsJoinPath(t *testing.T) {
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "secret")
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.example.com")

	m := NewManager(t.TempDir(), 1338)
	called := false
	handler := m.requirePublicHostBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "https://homectl.example.com/api/join", nil)
	req.Host = "homectl.example.com"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("expected join request to bypass basic auth")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}

func TestRequirePublicHostBasicAuthProtectsUIPaths(t *testing.T) {
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "secret")
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.example.com")

	m := NewManager(t.TempDir(), 1338)
	handler := m.requirePublicHostBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("UI request should not bypass basic auth")
	}))

	req := httptest.NewRequest(http.MethodGet, "https://homectl.example.com/", nil)
	req.Host = "homectl.example.com"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if got := rr.Header().Get("WWW-Authenticate"); got != `Basic realm="homectl"` {
		t.Fatalf("expected WWW-Authenticate header, got %q", got)
	}
}

func assertContains(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Fatalf("missing %q in:\n%s", want, s)
	}
}
