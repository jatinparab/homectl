package technitium

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	ContainerName        = "homectl-technitium"
	LegacyContainerName  = "homectl-technitium-technitium-1"
	DefaultImage         = "technitium/dns-server:15.4.0"
	DefaultClusterDomain = "cluster.home.arpa"
	SharedNetwork        = "homectl-docker-network"
	ConfigHashLabel      = "com.homectl.technitium.config-hash"
	AdminPort            = 5380
	ClusterPort          = 53443
)

type Config struct {
	NodeName      string
	NodeIP        string
	GatewayNode   string
	GatewayIP     string
	IsGateway     bool
	Password      string
	ClusterDomain string
	Image         string
	DataDir       string
	HostDataDir   string
}

type Status struct {
	Running bool
	Healthy bool
}

type commandRunner interface {
	Run(ctx context.Context, env map[string]string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, env map[string]string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = os.Environ()
	for key, value := range env {
		prefix := key + "="
		filtered := cmd.Env[:0]
		for _, entry := range cmd.Env {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		cmd.Env = append(filtered, key+"="+value)
	}
	return cmd.CombinedOutput()
}

type Controller struct {
	runner commandRunner
	client *http.Client

	mu                 sync.Mutex
	gatewayIdentity    string
	gatewaySince       time.Time
	lastGracefulFailed bool
	listeners          []net.Listener
	proxyCancel        context.CancelFunc
}

func NewController() *Controller {
	return &Controller{
		runner: execRunner{},
		client: &http.Client{Timeout: 6 * time.Minute},
	}
}

func normalizeConfig(cfg Config) (Config, error) {
	cfg.NodeName = strings.TrimSpace(cfg.NodeName)
	cfg.NodeIP = strings.TrimSpace(cfg.NodeIP)
	cfg.Password = strings.TrimSpace(cfg.Password)
	cfg.ClusterDomain = strings.TrimSuffix(strings.TrimSpace(cfg.ClusterDomain), ".")
	cfg.Image = strings.TrimSpace(cfg.Image)
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	cfg.HostDataDir = strings.TrimSpace(cfg.HostDataDir)
	if cfg.ClusterDomain == "" {
		cfg.ClusterDomain = DefaultClusterDomain
	}
	if cfg.Image == "" {
		cfg.Image = DefaultImage
	}
	if cfg.NodeName == "" || cfg.NodeIP == "" || cfg.Password == "" || cfg.DataDir == "" || cfg.HostDataDir == "" {
		return Config{}, errors.New("node name, node IP, admin password, and app data directories are required")
	}
	return cfg, nil
}

func configHash(cfg Config) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		cfg.NodeName, cfg.NodeIP, cfg.Password, cfg.ClusterDomain, cfg.Image, cfg.DataDir, cfg.HostDataDir,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (c *Controller) Ensure(ctx context.Context, raw Config) error {
	cfg, err := normalizeConfig(raw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "technitium", "config"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "technitium", "logs"), 0o755); err != nil {
		return err
	}

	existingPassword := c.containerPassword(ctx, ContainerName)
	if existingPassword == "" {
		existingPassword = c.containerPassword(ctx, LegacyContainerName)
	}
	if existingPassword != "" && existingPassword != cfg.Password {
		if err := c.rotatePassword(ctx, "http://"+ContainerName+":5380", existingPassword, cfg.Password); err != nil {
			return fmt.Errorf("rotate Technitium admin password: %w", err)
		}
	}

	_, _ = c.runner.Run(ctx, nil, "rm", "-f", LegacyContainerName)
	if _, err := c.runner.Run(ctx, nil, "network", "inspect", SharedNetwork); err != nil {
		if out, createErr := c.runner.Run(ctx, nil, "network", "create", SharedNetwork); createErr != nil {
			return fmt.Errorf("create shared network: %w: %s", createErr, strings.TrimSpace(string(out)))
		}
	}

	hash := configHash(cfg)
	out, inspectErr := c.runner.Run(ctx, nil, "inspect", "--format", "{{.State.Running}}|{{index .Config.Labels \""+ConfigHashLabel+"\"}}", ContainerName)
	if inspectErr == nil {
		parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
		if len(parts) == 2 && parts[0] == "true" && parts[1] == hash {
			return nil
		}
		_, _ = c.runner.Run(ctx, nil, "rm", "-f", ContainerName)
	}

	domain := cfg.NodeName + "." + cfg.ClusterDomain
	args := []string{
		"run", "-d", "--name", ContainerName,
		"--restart", "unless-stopped",
		"--hostname", cfg.NodeName,
		"--network", SharedNetwork,
		"--network-alias", ContainerName,
		"--label", ConfigHashLabel + "=" + hash,
		"-p", "53:53/udp", "-p", "53:53/tcp",
		"-p", "5380:5380/tcp", "-p", "53443:53443/tcp",
		"-e", "DNS_SERVER_ADMIN_PASSWORD",
		"-e", "DNS_SERVER_DOMAIN=" + domain,
		"-e", "DNS_SERVER_LOG_USING_LOCAL_TIME=true",
		"-e", "DNS_SERVER_LOG_MAX_LOG_FILE_DAYS=30",
		"-e", "DNS_SERVER_STATS_MAX_STAT_FILE_DAYS=30",
		"-v", filepath.Join(cfg.HostDataDir, "technitium", "config") + ":/etc/dns",
		"-v", filepath.Join(cfg.HostDataDir, "technitium", "logs") + ":/var/log/technitium/dns",
		cfg.Image,
	}
	out, err = c.runner.Run(ctx, map[string]string{"DNS_SERVER_ADMIN_PASSWORD": cfg.Password}, args...)
	if err != nil {
		return fmt.Errorf("start Technitium: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Controller) containerPassword(ctx context.Context, container string) string {
	out, err := c.runner.Run(ctx, nil, "inspect", "--format", "{{range .Config.Env}}{{println .}}{{end}}", container)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if value, ok := strings.CutPrefix(line, "DNS_SERVER_ADMIN_PASSWORD="); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Controller) Stop(ctx context.Context) error {
	out, err := c.runner.Run(ctx, nil, "stop", ContainerName)
	if err != nil && !strings.Contains(string(out), "No such container") {
		return fmt.Errorf("stop Technitium: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Controller) Remove(ctx context.Context) error {
	out, err := c.runner.Run(ctx, nil, "rm", "-f", ContainerName)
	if err != nil && !strings.Contains(string(out), "No such container") {
		return fmt.Errorf("remove Technitium: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Controller) Status(ctx context.Context) Status {
	out, err := c.runner.Run(ctx, nil, "inspect", "--format", "{{.State.Running}}", ContainerName)
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		out, err = c.runner.Run(ctx, nil, "inspect", "--format", "{{.State.Running}}", LegacyContainerName)
		if err != nil || strings.TrimSpace(string(out)) != "true" {
			return Status{}
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ContainerName+":5380/api/status", nil)
	if err != nil {
		return Status{Running: true}
	}
	response, err := c.client.Do(request)
	if err != nil {
		return Status{Running: true}
	}
	response.Body.Close()
	return Status{Running: true, Healthy: response.StatusCode == http.StatusOK}
}

func (c *Controller) StartProxies(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proxyCancel != nil {
		return nil
	}
	proxyCtx, cancel := context.WithCancel(ctx)
	listeners := make([]net.Listener, 0, 2)
	for _, port := range []int{AdminPort, ClusterPort} {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			for _, open := range listeners {
				open.Close()
			}
			cancel()
			return fmt.Errorf("listen for Technitium mesh proxy on %d: %w", port, err)
		}
		listeners = append(listeners, listener)
		go proxyLoop(proxyCtx, listener, fmt.Sprintf("%s:%d", ContainerName, port))
	}
	c.listeners = listeners
	c.proxyCancel = cancel
	return nil
}

func (c *Controller) StopProxies() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proxyCancel != nil {
		c.proxyCancel()
	}
	for _, listener := range c.listeners {
		listener.Close()
	}
	c.listeners = nil
	c.proxyCancel = nil
}

func proxyLoop(ctx context.Context, listener net.Listener, target string) {
	for {
		clientConn, err := listener.Accept()
		if err != nil {
			return
		}
		go proxyConn(ctx, clientConn, target)
	}
}

func proxyConn(ctx context.Context, clientConn net.Conn, target string) {
	defer clientConn.Close()
	upstream, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", target)
	if err != nil {
		return
	}
	defer upstream.Close()
	copyDone := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, clientConn); copyDone <- struct{}{} }()
	go func() { _, _ = io.Copy(clientConn, upstream); copyDone <- struct{}{} }()
	select {
	case <-ctx.Done():
	case <-copyDone:
	}
}
