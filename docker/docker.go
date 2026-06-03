package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
	"gopkg.in/yaml.v3"
)

var dockerLog = slog.With("area", "docker")

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
	rootDir          string
	appdataDir       string
	user             string
	composeExtraArgs []string
	httpClient       *http.Client
	dockerApps       map[string]DockerApp
	fileReader       FileReader
	ctx              context.Context
}

type DockerApp struct {
	Name          string
	Ports         []int
	HealthHost    string
	HealthPort    int
	HealthService string
}

func NewDocker(rootDir, appdataDir, user string) *Docker {
	return &Docker{
		rootDir:    rootDir,
		appdataDir: appdataDir,
		user:       user,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		dockerApps: make(map[string]DockerApp),
		fileReader: &LocalFileReader{},
		ctx:        context.Background(),
	}
}

// SetComposeExtraArgs configures extra docker compose options inserted before
// the subcommand, after the primary docker-compose.yml file.
func (d *Docker) SetComposeExtraArgs(args []string) {
	d.composeExtraArgs = append([]string(nil), args...)
}

// SetContext sets the context for cancellation
func (d *Docker) SetContext(ctx context.Context) {
	d.ctx = ctx
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

// parsePort extracts the host port from a port mapping string.
// Supported formats: "9999", "9999:9999", "9999:9999/tcp", "9999:9999/udp"
// Returns the host (exposed) port.
func parsePort(portStr string) (int, error) {
	hostPort, _, err := parsePortMapping(portStr)
	return hostPort, err
}

func parseContainerPort(portStr string) (int, error) {
	_, containerPort, err := parsePortMapping(portStr)
	return containerPort, err
}

func parsePortMapping(portStr string) (hostPort, containerPort int, err error) {
	// Remove protocol suffix if present (e.g., "/tcp", "/udp")
	portStr = strings.Split(portStr, "/")[0]

	// Check if it's a host:container mapping
	parts := strings.Split(portStr, ":")
	hostPortStr := parts[0]
	containerPortStr := parts[0]
	if len(parts) > 1 {
		hostPortStr = parts[len(parts)-2]
		containerPortStr = parts[len(parts)-1]
	}

	hostPortInt, err := strconv.Atoi(hostPortStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port number: %s", hostPortStr)
	}
	containerPortInt, err := strconv.Atoi(containerPortStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port number: %s", containerPortStr)
	}
	return hostPortInt, containerPortInt, nil
}

// dockerComposeFile represents the structure of a docker-compose.yml file
type dockerComposeFile struct {
	Services map[string]dockerComposeService `yaml:"services"`
}

type dockerComposeService struct {
	Ports []string `yaml:"ports"`
}

// analyzeDockerComposeContent parses docker-compose content and extracts app info.
// Ports are returned in the order they appear in the compose file (first occurrence wins).
func analyzeDockerComposeContent(appName string, content []byte) (DockerApp, error) {
	dockerApp := DockerApp{Name: appName}

	// Parse into a typed struct for service details
	var composeFile dockerComposeFile
	if err := yaml.Unmarshal(content, &composeFile); err != nil {
		return DockerApp{}, fmt.Errorf("failed to parse docker compose file: %w", err)
	}

	// Also parse into a YAML node tree so we can preserve service order
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return DockerApp{}, fmt.Errorf("failed to parse docker compose file tree: %w", err)
	}

	// Find the "services" mapping node in document order
	var servicesNode *yaml.Node
	if len(root.Content) > 0 {
		// Root should be a document node with a single mapping child
		doc := root.Content[0]
		if doc.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(doc.Content); i += 2 {
				key := doc.Content[i]
				val := doc.Content[i+1]
				if key.Value == "services" && val.Kind == yaml.MappingNode {
					servicesNode = val
					break
				}
			}
		}
	}

	// Fallback: no services node found, preserve old behavior (unordered)
	if servicesNode == nil {
		portSet := make(map[int]struct{})
		for _, service := range composeFile.Services {
			for _, portMapping := range service.Ports {
				port, err := parsePort(portMapping)
				if err != nil {
					continue
				}
				if _, exists := portSet[port]; exists {
					continue
				}
				portSet[port] = struct{}{}
				dockerApp.Ports = append(dockerApp.Ports, port)
				if dockerApp.HealthPort == 0 {
					containerPort, err := parseContainerPort(portMapping)
					if err == nil {
						dockerApp.HealthHost = HealthAlias(appName)
						dockerApp.HealthPort = containerPort
					}
				}
			}
		}
		return dockerApp, nil
	}

	// Walk services in YAML order and accumulate ports in first-seen order
	seenPorts := make(map[int]struct{})
	for i := 0; i+1 < len(servicesNode.Content); i += 2 {
		keyNode := servicesNode.Content[i]
		serviceName := keyNode.Value

		service, ok := composeFile.Services[serviceName]
		if !ok {
			continue
		}

		for _, portMapping := range service.Ports {
			port, err := parsePort(portMapping)
			if err != nil {
				continue // Skip invalid port mappings
			}
			if _, exists := seenPorts[port]; exists {
				continue // Deduplicate while preserving first occurrence order
			}
			seenPorts[port] = struct{}{}
			dockerApp.Ports = append(dockerApp.Ports, port)
			if dockerApp.HealthPort == 0 {
				containerPort, err := parseContainerPort(portMapping)
				if err == nil {
					dockerApp.HealthHost = HealthAlias(appName)
					dockerApp.HealthPort = containerPort
					dockerApp.HealthService = serviceName
				}
			}
		}
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
	// Any HTTP response means the service is accepting connections.
	// Actual health is monitored separately by the sensor loop.
	return nil
}

func waitForService(ctx context.Context, app DockerApp, tries int, interval time.Duration) error {
	if app.HealthHost == "" || app.HealthPort == 0 {
		return nil
	}
	url := fmt.Sprintf("http://%s:%d", app.HealthHost, app.HealthPort)
	for i := 0; i < tries; i++ {
		if err := ping(url); err != nil {
			dockerLog.Info("Service not ready", "url", url, "err", err, "try", i+1, "tries", tries)
		} else {
			return nil
		}

		// Wait with cancellation support
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	return fmt.Errorf("service not ready after %d tries", tries)
}

// HealthAlias returns the DNS alias Homectl uses for an app on its shared Docker network.
func HealthAlias(appID string) string {
	appID = strings.ToLower(strings.TrimSpace(appID))
	var b strings.Builder
	for _, r := range appID {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	alias := strings.Trim(b.String(), "-")
	if alias == "" {
		alias = "app"
	}
	return "homectl-" + alias
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

func (d *Docker) GetAppInfo(service string) (DockerApp, error) {
	if app, ok := d.dockerApps[service]; ok {
		return app, nil
	}
	path := filepath.Join(d.rootDir, service, "docker-compose.yml")
	content, err := d.fileReader.ReadFile(path)
	if err != nil {
		return DockerApp{}, fmt.Errorf("failed to read docker compose file: %w", err)
	}
	app, err := analyzeDockerComposeContent(service, content)
	if err != nil {
		return DockerApp{}, err
	}
	d.dockerApps[service] = app
	return app, nil
}

const HomectlDockerNetwork = "homectl-docker-network"

// EnsureHomectlDockerNetwork creates the shared app network if it is missing.
func EnsureHomectlDockerNetwork() error {
	if err := exec.Command("docker", "network", "inspect", HomectlDockerNetwork).Run(); err == nil {
		return nil
	}
	output, err := exec.Command("docker", "network", "create", HomectlDockerNetwork).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create docker network %s: %w: %s", HomectlDockerNetwork, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// ConnectContainerToHomectlNetwork connects a container to the shared app network.
func ConnectContainerToHomectlNetwork(container string, aliases ...string) error {
	if strings.TrimSpace(container) == "" {
		return nil
	}
	if err := EnsureHomectlDockerNetwork(); err != nil {
		return err
	}
	args := []string{"network", "connect"}
	for _, alias := range aliases {
		if alias = strings.TrimSpace(alias); alias != "" {
			args = append(args, "--alias", alias)
		}
	}
	args = append(args, HomectlDockerNetwork, container)
	output, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if strings.Contains(text, "already exists") || strings.Contains(text, "already connected") ||
			strings.Contains(text, "sharing network namespace") || strings.Contains(text, "cannot be connected") {
			return nil
		}
		return fmt.Errorf("connect %s to %s: %w: %s", container, HomectlDockerNetwork, err, text)
	}
	return nil
}

func (d *Docker) connectAppNetwork(app DockerApp) error {
	path := filepath.Join(d.rootDir, app.Name, "docker-compose.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("docker compose file not found: %s", path)
	}
	if err := EnsureHomectlDockerNetwork(); err != nil {
		return err
	}
	command := d.ComposeCommand(app.Name, "ps", "-q")
	output, err := command.Output()
	if err != nil {
		return err
	}
	healthContainer := ""
	if app.HealthService != "" {
		command = d.ComposeCommand(app.Name, "ps", "-q", app.HealthService)
		serviceOutput, err := command.Output()
		if err == nil {
			healthContainer = firstLine(serviceOutput)
		}
	}
	for _, container := range strings.Fields(string(output)) {
		var aliases []string
		if container == healthContainer && app.HealthHost != "" {
			aliases = append(aliases, app.HealthHost)
		}
		if err := ConnectContainerToHomectlNetwork(container, aliases...); err != nil {
			return err
		}
	}
	return nil
}

// ConnectLoadedAppsToHomectlNetwork connects all discovered managed app containers
// to the shared network used for agent health checks.
func (d *Docker) ConnectLoadedAppsToHomectlNetwork() error {
	for _, app := range d.dockerApps {
		running, err := d.IsRunning(app.Name)
		if err != nil || running == common.RunningStatusStopped {
			continue
		}
		if err := d.connectAppNetwork(app); err != nil {
			return err
		}
	}
	return nil
}

// ComposeCommand creates a docker compose command for a managed app.
func (d *Docker) ComposeCommand(appID string, subcommandArgs ...string) *exec.Cmd {
	return d.ComposeCommandContext(context.Background(), appID, subcommandArgs...)
}

// ComposeCommandContext creates a docker compose command for a managed app with
// cancellation support.
func (d *Docker) ComposeCommandContext(ctx context.Context, appID string, subcommandArgs ...string) *exec.Cmd {
	appDir := filepath.Join(d.rootDir, appID)
	args := []string{"compose", "-f", filepath.Join(appDir, "docker-compose.yml")}
	args = append(args, d.composeExtraArgs...)
	args = append(args, subcommandArgs...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = appDir
	cmd.Env = d.ComposeEnv()
	return cmd
}

// PruneStaleSharedNamespaceContainers removes containers whose network_mode
// references a container that no longer exists (e.g. caddy referencing a
// previous homectl-agent container after agent restart).
func PruneStaleSharedNamespaceContainers() {
	out, _ := exec.Command("docker", "ps", "-q", "--filter", "name=^homectl-agent$").Output()
	agentID := firstLine(out)
	if agentID == "" {
		return
	}

	output, err := exec.Command("docker", "ps", "-q", "--no-trunc").CombinedOutput()
	if err != nil {
		return
	}

	for _, cid := range strings.Fields(string(output)) {
		if cid == agentID {
			continue
		}
		inspect, err := exec.Command("docker", "inspect", "--format", "{{.HostConfig.NetworkMode}}", cid).CombinedOutput()
		if err != nil {
			continue
		}
		mode := strings.TrimSpace(string(inspect))
		if !strings.HasPrefix(mode, "container:") {
			continue
		}
		refName := strings.TrimPrefix(mode, "container:")
		if refName == "homectl-agent" {
			_ = exec.Command("docker", "rm", "-f", cid).Run()
		}
	}
}

func firstLine(b []byte) string {
	lines := strings.Fields(string(b))
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

// ComposeEnv returns the environment variables for docker compose commands
func (d *Docker) ComposeEnv() []string {
	env := withoutEnvKey(os.Environ(), "APPDATA")
	env = append(env, "APPDATA="+d.appdataDir)

	// Get current user's UID
	if u, err := user.Current(); err == nil {
		env = append(env, "LABCTL_USER="+u.Uid)
	}

	return env
}

func withoutEnvKey(env []string, key string) []string {
	prefix := key + "="
	out := env[:0]
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (d *Docker) RunDockerComposeUp(service string) error {
	path := filepath.Join(d.rootDir, service, "docker-compose.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("docker compose file not found: %s", path)
	}
	command := d.ComposeCommand(service, "up", "-d")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run docker compose up: %s", string(output))
	}

	// Wait for the service to be ready on its first port
	app, err := d.GetAppInfo(service)
	if err != nil {
		// Log but don't fail - service started successfully
		return nil
	}
	if err := d.connectAppNetwork(app); err != nil {
		return err
	}
	if err := waitForService(d.ctx, app, 5, 30*time.Second); err != nil {
		// Log but don't fail - service started, just not responding yet
		return fmt.Errorf("service not ready after %d tries: %w", 5, err)
	}

	return nil
}

// Start starts a docker compose application
func (d *Docker) Start(appID string) error {
	return d.RunDockerComposeUp(appID)
}

// streamCommand runs a command and streams stdout+stderr line-by-line via onLog.
func (d *Docker) streamCommand(cmd *exec.Cmd, onLog func(string)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	// Capture all output for error reporting
	var buf strings.Builder

	scanLines := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			buf.WriteString(line)
			buf.WriteByte('\n')
			if onLog != nil {
				onLog(line)
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanLines(stderr)
	}()
	scanLines(stdout)
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(buf.String()), err)
	}
	return nil
}

// pullTimeout is the maximum duration allowed for a docker compose pull.
const pullTimeout = 15 * time.Minute

// Pull pulls images for a docker compose service, streaming output line-by-line.
func (d *Docker) Pull(appID string, onLog func(string)) error {
	path := filepath.Join(d.rootDir, appID, "docker-compose.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("docker compose file not found: %s", path)
	}
	ctx := context.Background()
	cmd := d.ComposeCommandContext(ctx, appID, "--progress=plain", "pull")
	if err := d.streamCommand(cmd, onLog); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker compose pull timed out after %s", pullTimeout)
		}
		return fmt.Errorf("docker compose pull failed: %w", err)
	}
	return nil
}

// StartWithLogs pulls images (with streaming) then starts the service, streaming output.
func (d *Docker) StartWithLogs(appID string, onLog func(string)) error {
	// Pull images first
	if err := d.Pull(appID, onLog); err != nil {
		return err
	}

	// Then run docker compose up -d with streaming
	cmd := d.ComposeCommand(appID, "up", "-d")
	if err := d.streamCommand(cmd, onLog); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	// Wait for the service to be ready on its first port
	app, err := d.GetAppInfo(appID)
	if err != nil {
		return nil
	}
	if err := d.connectAppNetwork(app); err != nil {
		return err
	}
	if err := waitForService(d.ctx, app, 5, 30*time.Second); err != nil {
		return fmt.Errorf("service not ready after %d tries: %w", 5, err)
	}

	return nil
}

// Stop gracefully stops a docker compose application without removing containers.
func (d *Docker) Stop(appID string) error {
	path := filepath.Join(d.rootDir, appID, "docker-compose.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("docker compose file not found: %s", path)
	}
	command := d.ComposeCommand(appID, "stop", "-t", "60")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run docker compose stop: %s", string(output))
	}
	return nil
}

// Down gracefully stops and removes docker compose resources.
func (d *Docker) Down(appID string) error {
	path := filepath.Join(d.rootDir, appID, "docker-compose.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("docker compose file not found: %s", path)
	}
	command := d.ComposeCommand(appID, "down", "-t", "60")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run docker compose down: %s", string(output))
	}
	return nil
}

// IsRunning checks if a docker compose application is running
func (d *Docker) IsRunning(appID string) (common.RunningStatus, error) {
	path := filepath.Join(d.rootDir, appID, "docker-compose.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return common.RunningStatusStopped, fmt.Errorf("docker compose file not found: %s", path)
	}

	// Use docker compose ps to check if containers are running
	command := d.ComposeCommand(appID, "ps", "--status=running", "-q")
	output, err := command.Output()
	if err != nil {
		// If command fails, assume not running
		return common.RunningStatusStopped, nil
	}

	command = d.ComposeCommand(appID, "ps", "--status=restarting", "-q")
	outputRestarting, errRestarting := command.Output()
	if errRestarting == nil {
		if len(strings.TrimSpace(string(outputRestarting))) > 0 {
			return common.RunningStatusRestarting, nil
		}
	}

	// If there's any output, at least one container is running
	if len(strings.TrimSpace(string(output))) > 0 {
		return common.RunningStatusRunning, nil
	}
	return common.RunningStatusStopped, nil
}

// ListManaged returns a map of app IDs to their running status
func (d *Docker) ListManaged() (map[string]common.RunningStatus, error) {
	result := make(map[string]common.RunningStatus)

	entries, err := d.fileReader.ListDir(d.rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory %s: %w", d.rootDir, err)
	}

	for _, entry := range entries {
		// Check if it's a directory (ends with /)
		if !strings.HasSuffix(entry, "/") {
			continue
		}

		appID := strings.TrimSuffix(entry, "/")
		composePath := filepath.Join(d.rootDir, appID, "docker-compose.yml")

		// Check if docker-compose.yml exists
		if _, err := d.fileReader.ReadFile(composePath); err != nil {
			continue // No docker-compose.yml
		}

		// Check if running
		running, _ := d.IsRunning(appID)
		result[appID] = running
	}

	return result, nil
}
