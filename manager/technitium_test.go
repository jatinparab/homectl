package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jatinparab/homectl/common"
)

func TestLoadAppConfigsIncludesFirstClassTechnitium(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(repoDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	configs, err := m.LoadAppConfigs(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := configs[string(common.DNSAppID)]
	if !ok {
		t.Fatal("first-class Technitium config missing")
	}
	if !cfg.IsGlobal || !cfg.UsesGatewayOnlyBackups() || cfg.GetBackupInterval().String() != "3h0m0s" {
		t.Fatalf("unexpected Technitium config: %#v", cfg)
	}
}

func TestManagerCreatesFirstClassTechnitiumPublicRoute(t *testing.T) {
	t.Setenv("HOMECTL_DOMAIN_SUFFIX", "example.test")
	repoDir := t.TempDir()
	m, err := NewManager(repoDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	m.publicRoutesMu.RLock()
	defer m.publicRoutesMu.RUnlock()
	for _, route := range m.publicRoutes {
		if route.Host == "dns.example.test" && route.Enabled && route.AuthMode == PublicRouteAuthModeUpstream {
			return
		}
	}
	t.Fatal("first-class Technitium public route missing")
}

func TestDiscoverAppsRegistersTechnitiumWithoutComposeDirectory(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(repoDir, 1338)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	m.DiscoverApps()
	app := m.appRegistry.Get(common.DNSAppID)
	if app == nil {
		t.Fatal("Technitium was not registered")
	}
	if !app.IsGlobal || app.DesiredState != common.DesiredRunning {
		t.Fatalf("unexpected Technitium app: %#v", app)
	}
}
