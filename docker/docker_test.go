package docker

import (
	"path/filepath"
	"sort"
	"testing"
)

func TestAnalyzeDockerComposeFile_Caddy(t *testing.T) {
	path := filepath.Join("..", "testdata", "docker", "v2", "caddy", "docker-compose.yml")
	app, err := analyzeDockerComposeFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if app.Name != "caddy" {
		t.Errorf("expected app name 'caddy', got '%s'", app.Name)
	}

	if len(app.Ports) != 0 {
		t.Errorf("expected caddy to publish ports via homectl-agent, got %v", app.Ports)
	}
}

func TestAnalyzeDockerComposeFile_Adguard(t *testing.T) {
	path := filepath.Join("..", "testdata", "docker", "v2", "adguard", "docker-compose.yml")
	app, err := analyzeDockerComposeFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if app.Name != "adguard" {
		t.Errorf("expected app name 'adguard', got '%s'", app.Name)
	}

	// Adguard has ports 3000 and 53 (with /udp suffix)
	expectedPorts := []int{53, 3000}
	sort.Ints(app.Ports)

	if len(app.Ports) != len(expectedPorts) {
		t.Errorf("expected %d ports, got %d: %v", len(expectedPorts), len(app.Ports), app.Ports)
	}

	for i, port := range expectedPorts {
		if i < len(app.Ports) && app.Ports[i] != port {
			t.Errorf("expected port %d at index %d, got %d", port, i, app.Ports[i])
		}
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"9999", 9999, false},
		{"9999:9999", 9999, false},
		{"8080:80", 8080, false},
		{"53:53/udp", 53, false},
		{"443:443/tcp", 443, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			port, err := parsePort(tt.input)
			if tt.hasError {
				if err == nil {
					t.Errorf("expected error for input '%s'", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for input '%s': %v", tt.input, err)
				}
				if port != tt.expected {
					t.Errorf("expected port %d, got %d", tt.expected, port)
				}
			}
		})
	}
}

func TestDiscoverApps(t *testing.T) {
	rootDir := filepath.Join("..", "testdata", "docker", "v2")
	apps, err := DiscoverApps(rootDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 2 {
		t.Errorf("expected 2 apps, got %d", len(apps))
	}

	// Check that all expected apps are present
	appNames := make(map[string]bool)
	for _, app := range apps {
		appNames[app.Name] = true
	}

	for _, expected := range []string{"caddy", "adguard"} {
		if !appNames[expected] {
			t.Errorf("expected to find app '%s'", expected)
		}
	}
}

func TestDiscoverAppsFromPaths(t *testing.T) {
	paths := []string{
		filepath.Join("..", "testdata", "docker", "v2", "caddy", "docker-compose.yml"),
		filepath.Join("..", "testdata", "docker", "v2", "adguard", "docker-compose.yml"),
	}

	apps, err := DiscoverAppsFromPaths(paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 2 {
		t.Errorf("expected 2 apps, got %d", len(apps))
	}
}
