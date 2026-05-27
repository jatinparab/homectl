package manager

import (
	"strings"
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func TestGenerateManagerCaddyfileRoutesHomelabToGateway(t *testing.T) {
	m := NewManager(t.TempDir(), 1338)
	m.leaseManager.Heartbeat("node-1", "10.42.1.101", nil, common.NodeMetrics{})
	m.gatewayMu.Lock()
	m.gatewayNode = "node-1"
	m.gatewayMu.Unlock()
	m.publicRoutes = []PublicRoute{
		{
			ID:        "photos-sulphurgfx-com",
			Host:      "photos.example.net",
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

	assertContains(t, caddyfile, "homectl.example.net {")
	assertContains(t, caddyfile, "photos.example.net {")
	assertContains(t, caddyfile, "reverse_proxy https://10.42.1.101 {")
	assertContains(t, caddyfile, "header_up Host {host}")
	assertContains(t, caddyfile, "tls_server_name photos.example.net")
}

func TestGenerateManagerCaddyfileRoutesCustomWithoutHostOverride(t *testing.T) {
	m := NewManager(t.TempDir(), 1338)
	m.publicRoutes = []PublicRoute{
		{
			ID:           "docs-sulphurgfx-com",
			Host:         "docs.example.net",
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

	assertContains(t, caddyfile, "docs.example.net {")
	assertContains(t, caddyfile, "reverse_proxy https://example.com {")
	if strings.Contains(caddyfile, "header_up Host {host}") {
		t.Fatalf("custom route should not override upstream Host header:\n%s", caddyfile)
	}
}

func assertContains(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Fatalf("missing %q in:\n%s", want, s)
	}
}
