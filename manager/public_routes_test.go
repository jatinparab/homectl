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
	m := NewManager(t.TempDir(), 1338)
	m.leaseManager.Heartbeat("akira", "10.42.1.101", nil, common.NodeMetrics{})
	m.gatewayMu.Lock()
	m.gatewayNode = "akira"
	m.gatewayMu.Unlock()
	m.publicRoutes = []PublicRoute{
		{
			ID:        "photos-sulphurgfx-com",
			Host:      "photos.sulphurgfx.com",
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

	assertContains(t, caddyfile, "homectl.sulphurgfx.com {")
	assertContains(t, caddyfile, "photos.sulphurgfx.com {")
	assertContains(t, caddyfile, "reverse_proxy https://10.42.1.101 {")
	assertContains(t, caddyfile, "header_up Host {host}")
	assertContains(t, caddyfile, "tls_server_name photos.sulphurgfx.com")
}

func TestGenerateManagerCaddyfileRoutesCustomWithoutHostOverride(t *testing.T) {
	m := NewManager(t.TempDir(), 1338)
	m.publicRoutes = []PublicRoute{
		{
			ID:           "docs-sulphurgfx-com",
			Host:         "docs.sulphurgfx.com",
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

	assertContains(t, caddyfile, "docs.sulphurgfx.com {")
	assertContains(t, caddyfile, "reverse_proxy https://example.com {")
	if strings.Contains(caddyfile, "header_up Host {host}") {
		t.Fatalf("custom route should not override upstream Host header:\n%s", caddyfile)
	}
}

func TestRequirePublicHostBasicAuthSkipsAPIPaths(t *testing.T) {
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "secret")

	m := NewManager(t.TempDir(), 1338)
	called := false
	handler := m.requirePublicHostBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://homectl.sulphurgfx.com/api/v1/ws/state", nil)
	req.Host = "homectl.sulphurgfx.com"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("expected API request to bypass basic auth")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}

func TestRequirePublicHostBasicAuthProtectsUIPaths(t *testing.T) {
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "secret")

	m := NewManager(t.TempDir(), 1338)
	handler := m.requirePublicHostBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("UI request should not bypass basic auth")
	}))

	req := httptest.NewRequest(http.MethodGet, "https://homectl.sulphurgfx.com/", nil)
	req.Host = "homectl.sulphurgfx.com"
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
