package docker

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestAnalyzeDockerComposeFile_Caddy(t *testing.T) {
	path := filepath.Join("..", "testdata", "docker", "caddy", "docker-compose.yml")
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
	path := filepath.Join("..", "testdata", "docker", "adguard", "docker-compose.yml")
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
	rootDir := filepath.Join("..", "testdata", "docker")
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
		filepath.Join("..", "testdata", "docker", "caddy", "docker-compose.yml"),
		filepath.Join("..", "testdata", "docker", "adguard", "docker-compose.yml"),
	}

	apps, err := DiscoverAppsFromPaths(paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 2 {
		t.Errorf("expected 2 apps, got %d", len(apps))
	}
}

func TestComposeCommandIncludesExtraArgsBeforeSubcommand(t *testing.T) {
	rootDir := filepath.Join("..", "testdata", "docker")
	d := NewDocker(rootDir, "/srv/homectl/app-data")
	d.SetComposeExtraArgs([]string{"-f", "docker-compose.gpu.yml", "--profile", "gpu"})

	cmd := d.ComposeCommand("adguard", "up", "-d")
	wantArgs := []string{
		"docker",
		"compose",
		"--project-name",
		"homectl-adguard",
		"-f",
		filepath.Join(rootDir, "adguard", "docker-compose.yml"),
		"-f",
		"docker-compose.gpu.yml",
		"--profile",
		"gpu",
		"up",
		"-d",
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, wantArgs)
	}

	wantDir := filepath.Join(rootDir, "adguard")
	if cmd.Dir != wantDir {
		t.Fatalf("cmd.Dir = %q, want %q", cmd.Dir, wantDir)
	}

	wantEnv := "APPDATA=/srv/homectl/app-data"
	if !envContains(cmd.Env, wantEnv) {
		t.Fatalf("cmd.Env missing %q in %v", wantEnv, cmd.Env)
	}
}

func envContains(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func TestComposeEnvOverridesInheritedAPPDATA(t *testing.T) {
	t.Setenv("APPDATA", "/wrong")
	d := NewDocker("/tmp/docker", "/srv/homectl/app-data")

	env := d.ComposeEnv()
	if !envContains(env, "APPDATA=/srv/homectl/app-data") {
		t.Fatalf("ComposeEnv missing resolved host APPDATA in %v", env)
	}
	for _, item := range env {
		if strings.HasPrefix(item, "APPDATA=") && item != "APPDATA=/srv/homectl/app-data" {
			t.Fatalf("ComposeEnv kept stale APPDATA entry %q; full env has %d entries, process env has %d", item, len(env), len(os.Environ()))
		}
	}
}
