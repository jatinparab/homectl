package docker

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileReader is an interface for reading files and listing directories
type FileReader interface {
	ReadFile(path string) ([]byte, error)
	ListDir(path string) ([]string, error)
}

// LocalFileReader reads files from the local filesystem
type LocalFileReader struct{}

func (l *LocalFileReader) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (l *LocalFileReader) ListDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	return names, nil
}

type Docker struct {
	rootDir    string
	httpClient *http.Client
	dockerApps map[string]DockerApp
	fileReader FileReader
}

type DockerApp struct {
	Name  string
	Ports []int
}

func NewDocker(rootDir string) *Docker {
	return &Docker{
		rootDir: rootDir,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		dockerApps: make(map[string]DockerApp),
		fileReader: &LocalFileReader{},
	}
}

// NewDockerWithFileReader creates a Docker instance with a custom FileReader
func NewDockerWithFileReader(rootDir string, reader FileReader) *Docker {
	return &Docker{
		rootDir: rootDir,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		dockerApps: make(map[string]DockerApp),
		fileReader: reader,
	}
}

// dockerComposeFile represents the structure of a docker-compose.yml file
type dockerComposeFile struct {
	Services map[string]dockerComposeService `yaml:"services"`
}

type dockerComposeService struct {
	Ports []string `yaml:"ports"`
}

// parsePort extracts the host port from a port mapping string.
// Supported formats: "9999", "9999:9999", "9999:9999/tcp", "9999:9999/udp"
// Returns the host (exposed) port.
func parsePort(portStr string) (int, error) {
	// Remove protocol suffix if present (e.g., "/tcp", "/udp")
	portStr = strings.Split(portStr, "/")[0]

	// Check if it's a host:container mapping
	parts := strings.Split(portStr, ":")
	hostPort := parts[0]

	port, err := strconv.Atoi(hostPort)
	if err != nil {
		return 0, fmt.Errorf("invalid port number: %s", hostPort)
	}
	return port, nil
}

// analyzeDockerComposeContent parses docker-compose content and extracts app info
func analyzeDockerComposeContent(appName string, content []byte) (DockerApp, error) {
	dockerApp := DockerApp{Name: appName}

	var composeFile dockerComposeFile
	if err := yaml.Unmarshal(content, &composeFile); err != nil {
		return DockerApp{}, fmt.Errorf("failed to parse docker compose file: %w", err)
	}

	// Collect all unique ports from all services
	portSet := make(map[int]struct{})
	for _, service := range composeFile.Services {
		for _, portMapping := range service.Ports {
			port, err := parsePort(portMapping)
			if err != nil {
				continue // Skip invalid port mappings
			}
			portSet[port] = struct{}{}
		}
	}

	// Convert set to slice
	for port := range portSet {
		dockerApp.Ports = append(dockerApp.Ports, port)
	}

	return dockerApp, nil
}

func analyzeDockerComposeFile(path string) (DockerApp, error) {
	// Use the parent directory name as the app name
	appName := filepath.Base(filepath.Dir(path))

	content, err := os.ReadFile(path)
	if err != nil {
		return DockerApp{}, fmt.Errorf("failed to read docker compose file: %w", err)
	}

	return analyzeDockerComposeContent(appName, content)
}

// DiscoverApps scans a directory for docker-compose.yml files and returns all discovered apps.
// It expects the directory structure: rootDir/<app-name>/docker-compose.yml
func DiscoverApps(rootDir string) ([]DockerApp, error) {
	var apps []DockerApp

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", rootDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		composePath := filepath.Join(rootDir, entry.Name(), "docker-compose.yml")
		if _, err := os.Stat(composePath); os.IsNotExist(err) {
			continue // No docker-compose.yml in this directory
		}

		app, err := analyzeDockerComposeFile(composePath)
		if err != nil {
			continue // Skip apps that fail to parse
		}

		apps = append(apps, app)
	}

	return apps, nil
}

// DiscoverAppsFromPaths analyzes docker-compose files from a configurable list of paths.
// Each path should be a full path to a docker-compose.yml file.
func DiscoverAppsFromPaths(paths []string) ([]DockerApp, error) {
	var apps []DockerApp

	for _, path := range paths {
		app, err := analyzeDockerComposeFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to analyze %s: %w", path, err)
		}
		apps = append(apps, app)
	}

	return apps, nil
}

// LoadApps loads docker apps into the Docker instance from the configured rootDir.
func (d *Docker) LoadApps() error {
	entries, err := d.fileReader.ListDir(d.rootDir)
	if err != nil {
		return fmt.Errorf("failed to list directory %s: %w", d.rootDir, err)
	}

	for _, entry := range entries {
		// Check if it's a directory (ends with /)
		if !strings.HasSuffix(entry, "/") {
			continue
		}

		appName := strings.TrimSuffix(entry, "/")
		composePath := filepath.Join(d.rootDir, appName, "docker-compose.yml")

		content, err := d.fileReader.ReadFile(composePath)
		if err != nil {
			continue // No docker-compose.yml or can't read it
		}

		app, err := analyzeDockerComposeContent(appName, content)
		if err != nil {
			continue // Skip apps that fail to parse
		}

		d.dockerApps[app.Name] = app
	}

	return nil
}

// GetApp returns a discovered docker app by name.
func (d *Docker) GetApp(name string) (DockerApp, bool) {
	app, ok := d.dockerApps[name]
	return app, ok
}

// ListApps returns all discovered docker apps.
func (d *Docker) ListApps() []DockerApp {
	apps := make([]DockerApp, 0, len(d.dockerApps))
	for _, app := range d.dockerApps {
		apps = append(apps, app)
	}
	return apps
}

func ping(url string) error {
	httpClient := &http.Client{
		Timeout: 3 * time.Second,
	}
	response, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("service returned status code %d", response.StatusCode)
	}
	return nil
}

func waitForService(port int, tries int, interval time.Duration) error {
	url := fmt.Sprintf("http://localhost:%d", port)
	for i := 0; i < tries; i++ {
		if err := ping(url); err == nil {
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("service not ready after %d tries", tries)
}

func (d *Docker) GetPorts(service string) ([]int, error) {
	// First check if we have the app cached
	if app, ok := d.dockerApps[service]; ok {
		return app.Ports, nil
	}

	// Otherwise, read and analyze the docker-compose file
	path := filepath.Join(d.rootDir, service, "docker-compose.yml")
	content, err := d.fileReader.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read docker compose file: %w", err)
	}

	app, err := analyzeDockerComposeContent(service, content)
	if err != nil {
		return nil, err
	}

	// Cache the result
	d.dockerApps[service] = app

	return app.Ports, nil
}

func (d *Docker) RunDockerComposeUp(service string) error {
	path := filepath.Join(d.rootDir, service, "docker-compose.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("docker compose file not found: %s", path)
	}
	command := exec.Command("docker", "compose", "-f", path, "up", "-d")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run docker compose up: %s", string(output))
	}

	return nil
}
