package manager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func TestGenerateManagerCaddyfileRoutesHomelabToGateway(t *testing.T) {
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.test.example")
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	m.leaseManager.Heartbeat("node-1", "10.42.1.101", "", nil, common.NodeMetrics{})
	if err := m.saveWireGuardPeers([]wireGuardPeerState{
		{Name: "node-1", PublicKey: "test-key", Address: "10.44.0.7"},
	}); err != nil {
		t.Fatalf("saveWireGuardPeers: %v", err)
	}
	m.gatewayMu.Lock()
	m.gatewayNode = "node-1"
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
	assertContains(t, caddyfile, "output file ")
	assertContains(t, caddyfile, "format json")
	assertContains(t, caddyfile, "reverse_proxy https://10.44.0.7 {")
	assertContains(t, caddyfile, "header_up Host {host}")
	assertContains(t, caddyfile, "tls_server_name photos.example.com")
}

func TestGenerateManagerCaddyfileRoutesCustomWithoutHostOverride(t *testing.T) {
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.test.example")
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
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

func TestGenerateManagerCaddyfileRoutesWithAuthModes(t *testing.T) {
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.example.net")
	t.Setenv("AUTHENTIK_OUTPOST_URL", "https://auth.example.net")
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	m.leaseManager.Heartbeat("node-1", "10.42.1.101", "", nil, common.NodeMetrics{})
	if err := m.saveWireGuardPeers([]wireGuardPeerState{
		{Name: "node-1", PublicKey: "test-key", Address: "10.44.0.7"},
	}); err != nil {
		t.Fatalf("saveWireGuardPeers: %v", err)
	}
	m.gatewayMu.Lock()
	m.gatewayNode = "node-1"
	m.gatewayMu.Unlock()
	now := time.Now()
	m.publicRoutes = []PublicRoute{
		{ID: "grafana-sulphurgfx-com", Host: "grafana.example.net", Mode: PublicRouteModeHomelab, AuthMode: PublicRouteAuthModeAuthentik, Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "jellyfin-sulphurgfx-com", Host: "jellyfin.example.net", Mode: PublicRouteModeHomelab, AuthMode: PublicRouteAuthModeUpstream, Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "status-sulphurgfx-com", Host: "status.example.net", Mode: PublicRouteModeCustom, AuthMode: PublicRouteAuthModePublic, CustomTarget: "http://homectl-localhost:3001", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}

	caddyfile, err := m.GenerateManagerCaddyfile()
	if err != nil {
		t.Fatalf("GenerateManagerCaddyfile: %v", err)
	}

	assertContains(t, caddyfile, "(authentik_forward_auth) {")
	assertContains(t, caddyfile, "reverse_proxy /outpost.goauthentik.io/* https://auth.example.net")
	assertContains(t, caddyfile, "forward_auth https://auth.example.net {")
	assertContains(t, caddyfile, "# Auth mode: authentik\ngrafana.example.net {\n\tlog {")
	assertContains(t, caddyfile, "\t\timport authentik_forward_auth\n\t\treverse_proxy https://10.44.0.7 {")
	assertContains(t, caddyfile, "# Auth mode: upstream\njellyfin.example.net {")
	assertContains(t, caddyfile, "# Auth mode: public\nstatus.example.net {")
	if strings.Contains(caddyfile[strings.Index(caddyfile, "jellyfin.example.net"):], "import authentik_forward_auth") &&
		strings.Index(caddyfile, "jellyfin.example.net") < strings.Index(caddyfile, "import authentik_forward_auth") {
		t.Fatalf("upstream route imported authentik snippet:\n%s", caddyfile)
	}
}

func TestValidatePublicRouteRejectsAuthentikProtectingAuthentik(t *testing.T) {
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.example.net")
	err := validatePublicRoute(PublicRoute{
		Host:     "auth.example.net",
		Mode:     PublicRouteModeHomelab,
		AuthMode: PublicRouteAuthModeAuthentik,
	})
	if err == nil || !strings.Contains(err.Error(), "must not use authentik") {
		t.Fatalf("expected authentik self-protection error, got %v", err)
	}
}

func TestGenerateAgentCaddyConfigRoutesPublicAndInternalHosts(t *testing.T) {
	t.Setenv("HOMECTL_DOMAIN_SUFFIX", "example.net")
	t.Setenv("CADDY_DNS_PROVIDER", "cloudflare")
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	m.leaseManager.Heartbeat("node-1", "10.44.0.7", "", []common.AppReport{
		{
			AppID:         "photos",
			ObservedState: common.ObservedRunning,
			Health:        common.HealthHealthy,
			Port:          2283,
		},
	}, common.NodeMetrics{})

	caddyfile, err := m.GenerateCaddyConfig("node-1")
	if err != nil {
		t.Fatalf("GenerateCaddyConfig: %v", err)
	}

	assertContains(t, caddyfile, "photos.example.net {\n\treverse_proxy http://homectl-photos:2283\n")
	assertContains(t, caddyfile, "photos.internal.example.net {\n\treverse_proxy http://homectl-photos:2283\n")
	assertContains(t, caddyfile, "@homectl_health_check header X-Homectl-Health-Check true")
	assertContains(t, caddyfile, "respond @homectl_health_check 204")
}

func TestCheckPublicRouteDoesNotKeepProbeConnectionAlive(t *testing.T) {
	requestClose := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestClose = r.Close
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	result := m.checkPublicRoute(context.Background(), PublicRoute{
		Host:         "probe.example.com",
		Mode:         PublicRouteModeCustom,
		CustomTarget: server.URL,
		Enabled:      true,
	})
	if result.Status != "healthy" {
		t.Fatalf("route status = %q, error = %q", result.Status, result.LastError)
	}
	if !requestClose {
		t.Fatal("health probe did not request connection close")
	}
}

func TestGenerateAgentCaddyConfigDNSChallengeBlock(t *testing.T) {
	t.Setenv("HOMECTL_DOMAIN_SUFFIX", "example.net")
	t.Setenv("CADDY_DNS_PROVIDER", "cloudflare")
	t.Setenv("CADDY_DNS_TOKEN_ENV", "CF_DNS_API_TOKEN")
	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
	m.leaseManager.Heartbeat("node-1", "10.44.0.7", "", []common.AppReport{
		{
			AppID:         "photos",
			ObservedState: common.ObservedRunning,
			Health:        common.HealthHealthy,
			Port:          2283,
		},
	}, common.NodeMetrics{})

	caddyfile, err := m.GenerateCaddyConfig("node-1")
	if err != nil {
		t.Fatalf("GenerateCaddyConfig: %v", err)
	}

	assertContains(t, caddyfile, "photos.example.net {\n\ttls {\n\t\tdns cloudflare {env.CF_DNS_API_TOKEN}\n\t}\n\treverse_proxy http://homectl-photos:2283\n")
	assertContains(t, caddyfile, "photos.internal.example.net {\n\ttls {\n\t\tdns cloudflare {env.CF_DNS_API_TOKEN}\n\t}\n\treverse_proxy http://homectl-photos:2283\n")
}

func TestGenerateAgentCaddyConfigExposedSubdomainsUseContainerPortService(t *testing.T) {
	t.Setenv("HOMECTL_DOMAIN_SUFFIX", "example.net")

	repoDir := t.TempDir()
	appDir := filepath.Join(repoDir, "docker", "arr-stack")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "homectl-config.json"), []byte(`{
  "exposed_subdomains": {
    "torrent": "8081",
    "prowlarr": "9696",
    "sonarr": "8989",
    "radarr": "7878",
    "bazarr": "6767",
    "nzbget": "6789"
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte(`services:
  qbittorrent:
    container_name: qbittorrent
    ports:
      - 8081:8081
  nzbget:
    container_name: nzbget
    ports:
      - 6789:6789
  prowlarr:
    container_name: prowlarr
    ports:
      - 9696:9696
  sonarr:
    container_name: sonarr
    ports:
      - 8989:8989
  radarr:
    container_name: radarr
    ports:
      - 7878:7878
  bazarr:
    container_name: bazarr
    ports:
      - 6767:6767
`), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(repoDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	m.leaseManager.Heartbeat("node-1", "10.44.0.7", "", []common.AppReport{
		{
			AppID:         "arr-stack",
			ObservedState: common.ObservedRunning,
			Health:        common.HealthHealthy,
			Port:          8081,
		},
	}, common.NodeMetrics{})

	caddyfile, err := m.GenerateCaddyConfig("node-1")
	if err != nil {
		t.Fatalf("GenerateCaddyConfig: %v", err)
	}

	assertContains(t, caddyfile, "torrent.example.net, torrent.internal.example.net {\n\treverse_proxy http://homectl-arr-stack:8081\n")
	assertContains(t, caddyfile, "prowlarr.example.net, prowlarr.internal.example.net {\n\treverse_proxy http://homectl-arr-stack:9696\n")
	assertContains(t, caddyfile, "radarr.example.net, radarr.internal.example.net {\n\treverse_proxy http://homectl-arr-stack:7878\n")
	assertContains(t, caddyfile, "sonarr.example.net, sonarr.internal.example.net {\n\treverse_proxy http://homectl-arr-stack:8989\n")
	assertContains(t, caddyfile, "bazarr.example.net, bazarr.internal.example.net {\n\treverse_proxy http://homectl-arr-stack:6767\n")
	assertContains(t, caddyfile, "nzbget.example.net, nzbget.internal.example.net {\n\treverse_proxy http://homectl-arr-stack:6789\n")
}

func TestRequirePublicHostBasicAuthSkipsAPIPaths(t *testing.T) {
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "secret")
	t.Setenv("HOMECTL_PUBLIC_HOST", "homectl.example.com")

	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
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

	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
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

	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}
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

func TestParsePublicRouteAccessLogLine(t *testing.T) {
	line := []byte(`{"ts":1762324322.125,"request":{"client_ip":"203.0.113.10","proto":"HTTP/2.0","method":"GET","host":"Photos.Example.Com","uri":"/albums","headers":{"User-Agent":["curl/8.0"]}},"duration":0.034,"size":2048,"status":200}`)

	entry, ok := parsePublicRouteAccessLogLine(line)
	if !ok {
		t.Fatal("expected log line to parse")
	}
	if entry.Host != "photos.example.com" {
		t.Fatalf("expected normalized host, got %q", entry.Host)
	}
	if entry.ClientIP != "203.0.113.10" {
		t.Fatalf("expected client IP, got %q", entry.ClientIP)
	}
	if entry.Method != "GET" || entry.URI != "/albums" || entry.Status != 200 {
		t.Fatalf("unexpected request fields: %+v", entry)
	}
	if entry.Size != 2048 || entry.UserAgent != "curl/8.0" {
		t.Fatalf("unexpected response fields: %+v", entry)
	}
	if entry.Timestamp.IsZero() {
		t.Fatal("expected timestamp to parse")
	}
}

func TestGenerateAgentCaddyConfigDedupesDuplicateAppReports(t *testing.T) {
	t.Setenv("HOMECTL_DOMAIN_SUFFIX", "test.example")

	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}

	// Register two agents both reporting the same app as running.
	// node-3 is the gateway.
	m.leaseManager.Heartbeat("node-3", "10.44.0.3", "", []common.AppReport{
		{AppID: "photos", ObservedState: common.ObservedRunning, Health: common.HealthHealthy, Port: 2283},
	}, common.NodeMetrics{})
	m.leaseManager.Heartbeat("node-1", "10.44.0.7", "", []common.AppReport{
		{AppID: "photos", ObservedState: common.ObservedRunning, Health: common.HealthHealthy, Port: 2283},
	}, common.NodeMetrics{})

	// Set node-3 as gateway (via WireGuard peer + assignment).
	if err := m.saveWireGuardPeers([]wireGuardPeerState{
		{Name: "node-3", PublicKey: "gk1", Address: "10.44.0.3"},
		{Name: "node-1", PublicKey: "gk2", Address: "10.44.0.7"},
	}); err != nil {
		t.Fatalf("saveWireGuardPeers: %v", err)
	}
	m.gatewayMu.Lock()
	m.gatewayNode = "node-3"
	m.gatewayMu.Unlock()

	// Generate config for node-3 itself.
	caddyfile, err := m.GenerateCaddyConfig("node-3")
	if err != nil {
		t.Fatalf("GenerateCaddyConfig: %v", err)
	}

	// Combined block when no DNS provider is set.
	assertContains(t, caddyfile, "photos.test.example, photos.internal.test.example {")

	// Verify no duplicate hostname block appears.
	firstIdx := strings.Index(caddyfile, "photos.test.example")
	if firstIdx < 0 {
		t.Fatal("expected photos.test.example in output")
	}
	secondIdx := strings.Index(caddyfile[firstIdx+len("photos.test.example"):], "photos.test.example")
	if secondIdx >= 0 {
		t.Fatal("found duplicate photos.test.example in output, expected exactly one occurrence")
	}

	// Gateway (node-3) hosts the app and is the local node, so proxy should be local Docker alias.
	assertContains(t, caddyfile, "reverse_proxy http://homectl-photos:2283")
}

func TestGenerateAgentCaddyConfigPrefersGatewayNode(t *testing.T) {
	t.Setenv("HOMECTL_DOMAIN_SUFFIX", "test.example")

	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}

	// Register two agents both reporting the same app as running.
	// node-3 is the gateway, node-1 is not.
	m.leaseManager.Heartbeat("node-3", "10.44.0.3", "", []common.AppReport{
		{AppID: "photos", ObservedState: common.ObservedRunning, Health: common.HealthHealthy, Port: 2283},
	}, common.NodeMetrics{})
	m.leaseManager.Heartbeat("node-1", "10.44.0.7", "", []common.AppReport{
		{AppID: "photos", ObservedState: common.ObservedRunning, Health: common.HealthHealthy, Port: 2283},
	}, common.NodeMetrics{})

	if err := m.saveWireGuardPeers([]wireGuardPeerState{
		{Name: "node-3", PublicKey: "gk1", Address: "10.44.0.3"},
		{Name: "node-1", PublicKey: "gk2", Address: "10.44.0.7"},
	}); err != nil {
		t.Fatalf("saveWireGuardPeers: %v", err)
	}
	m.gatewayMu.Lock()
	m.gatewayNode = "node-3"
	m.gatewayMu.Unlock()

	// Generate config for node-1 (a non-gateway node).
	caddyfile, err := m.GenerateCaddyConfig("node-1")
	if err != nil {
		t.Fatalf("GenerateCaddyConfig: %v", err)
	}

	// Should route to gateway (node-3) since it hosts the app.
	// node-3 is remote from node-1's perspective, so uses HTTPS with TLS SNI.
	assertContains(t, caddyfile, "photos.test.example, photos.internal.test.example {")
	assertContains(t, caddyfile, "reverse_proxy https://10.44.0.3 {")
	assertContains(t, caddyfile, "tls_server_name photos.test.example")
}

func TestGenerateAgentCaddyConfigFallsBackWhenGatewayMissingApp(t *testing.T) {
	t.Setenv("HOMECTL_DOMAIN_SUFFIX", "test.example")

	m, err := NewManager(t.TempDir(), 1338)
	if err != nil {
		t.Fatal(err)
	}

	// Only node-1 reports the app. node-3 is the gateway but doesn't run it.
	m.leaseManager.Heartbeat("node-3", "10.44.0.3", "", nil, common.NodeMetrics{})
	m.leaseManager.Heartbeat("node-1", "10.44.0.7", "", []common.AppReport{
		{AppID: "photos", ObservedState: common.ObservedRunning, Health: common.HealthHealthy, Port: 2283},
	}, common.NodeMetrics{})

	if err := m.saveWireGuardPeers([]wireGuardPeerState{
		{Name: "node-3", PublicKey: "gk1", Address: "10.44.0.3"},
		{Name: "node-1", PublicKey: "gk2", Address: "10.44.0.7"},
	}); err != nil {
		t.Fatalf("saveWireGuardPeers: %v", err)
	}
	m.gatewayMu.Lock()
	m.gatewayNode = "node-3"
	m.gatewayMu.Unlock()

	// Generate config for node-3 (the gateway).
	caddyfile, err := m.GenerateCaddyConfig("node-3")
	if err != nil {
		t.Fatalf("GenerateCaddyConfig: %v", err)
	}

	// Should route to node-1 since it's the only host running the app.
	// Remote target uses HTTPS with TLS SNI.
	assertContains(t, caddyfile, "photos.test.example, photos.internal.test.example {")
	assertContains(t, caddyfile, "reverse_proxy https://10.44.0.7 {")
	assertContains(t, caddyfile, "tls_server_name photos.test.example")
}

func assertContains(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Fatalf("missing %q in:\n%s", want, s)
	}
}
