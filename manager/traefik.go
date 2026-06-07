package manager

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/docker"
)

var caddyMgrLog = slog.With("area", "caddy")

// FlexPort unmarshals a port from either a JSON number or a JSON string.
type FlexPort int

func (f *FlexPort) UnmarshalJSON(data []byte) error {
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*f = FlexPort(i)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("port must be a number or string, got %s", string(data))
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("cannot convert port %q to int: %w", s, err)
	}
	*f = FlexPort(i)
	return nil
}

func domainSuffix() (string, error) {
	if suffix := normalizePublicRouteHost(os.Getenv("HOMECTL_DOMAIN_SUFFIX")); suffix != "" {
		return suffix, nil
	}
	return "", fmt.Errorf("HOMECTL_DOMAIN_SUFFIX is required")
}

// HomectlConfig represents the homectl-config.json file for an app
type HomectlConfig struct {
	ExposedSubdomains map[string]FlexPort `json:"exposed_subdomains,omitempty"`
	ExcludeNodes      []string            `json:"exclude_nodes,omitempty"`
	BackupSensitive   bool                `json:"backup_sensitive,omitempty"`
	BackupEnabled     *bool               `json:"backup_enabled,omitempty"`  // nil = true (default enabled)
	BackupInterval    string              `json:"backup_interval,omitempty"` // e.g. "72h", "168h" — overrides default
	IsGlobal          bool                `json:"is_global,omitempty"`       // run on all connected nodes
}

// IsBackupEnabled returns whether backup is enabled for this app (default true)
func (c HomectlConfig) IsBackupEnabled() bool {
	if c.BackupEnabled == nil {
		return true
	}
	return *c.BackupEnabled
}

// GetBackupInterval returns the per-app backup interval, or the default if not set.
func (c HomectlConfig) GetBackupInterval() time.Duration {
	if c.BackupInterval != "" {
		d, err := time.ParseDuration(c.BackupInterval)
		if err == nil && d > 0 {
			return d
		}
	}
	return BackupInterval
}

// loadAppConfigs loads homectl-config.json files from docker directories
func (m *Manager) loadAppConfigs() map[string]HomectlConfig {
	configs := make(map[string]HomectlConfig)

	dockerDir := filepath.Join(m.repoDir, "docker")
	entries, err := os.ReadDir(dockerDir)
	if err != nil {
		caddyMgrLog.Error("Failed to read docker directory", "err", err)
		return configs
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appName := entry.Name()
		configPath := filepath.Join(dockerDir, appName, "homectl-config.json")

		data, err := os.ReadFile(configPath)
		if err != nil {
			// Config file doesn't exist or can't be read - that's fine
			continue
		}

		var cfg HomectlConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			caddyMgrLog.Error("Failed to parse config", "path", configPath, "err", err)
			continue
		}

		configs[appName] = cfg
	}

	return configs
}

// GenerateCaddyConfig generates Caddy config for a specific node.
// For local apps (same node): use the shared Docker network alias.
// For remote apps: use the remote node's IP
func (m *Manager) GenerateCaddyConfig(forNodeID string) (string, error) {
	suffix, err := domainSuffix()
	if err != nil {
		return "", err
	}

	// Load app configs for exposed_subdomains overrides
	appConfigs := m.loadAppConfigs()

	// Get all agents and their app reports, sorted by name for deterministic output
	agents := m.leaseManager.ListAgents()
	alive := m.aliveAgents()
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })

	type route struct {
		publicHost   string
		internalHost string
		target       string
	}
	routes := make([]route, 0)

	for _, agent := range agents {
		if !alive[agent.Name] {
			continue
		}
		for _, report := range agent.AppReports {
			// Skip non-running apps
			if report.ObservedState != common.ObservedRunning {
				continue
			}

			appName := string(report.AppID)
			if report.AppID == common.GatewayProxyAppID {
				continue
			}

			// Determine target host based on local vs remote.
			// Local apps are reached via the homectl Docker network alias.
			// Remote apps are proxied to the remote agent's caddy over WireGuard.
			var targetHost string
			var defaultPort int
			if agent.Name == forNodeID {
				targetHost = docker.HealthAlias(appName)
				defaultPort = report.Port
			} else {
				targetHost = agent.IP
				defaultPort = 80
			}

			// Check if app has exposed_subdomains override
			if appCfg, ok := appConfigs[appName]; ok && len(appCfg.ExposedSubdomains) > 0 {
				// Sort subdomain keys for deterministic config output
				subdomains := make([]string, 0, len(appCfg.ExposedSubdomains))
				for sd := range appCfg.ExposedSubdomains {
					subdomains = append(subdomains, sd)
				}
				sort.Strings(subdomains)

				// Use custom subdomains from homectl-config.json
				for _, subdomain := range subdomains {
					port := appCfg.ExposedSubdomains[subdomain]

					publicDomain := fmt.Sprintf("%s.%s", subdomain, suffix)
					internalDomain := fmt.Sprintf("%s.internal.%s", subdomain, suffix)
					routes = append(routes, route{
						publicHost:   publicDomain,
						internalHost: internalDomain,
						target:       fmt.Sprintf("http://%s:%d", targetHost, int(port)),
					})
				}
			} else {
				// Default behavior: use app name as subdomain and reported port
				if report.Port == 0 {
					continue // Skip apps without exposed ports
				}

				publicDomain := fmt.Sprintf("%s.%s", appName, suffix)
				internalDomain := fmt.Sprintf("%s.internal.%s", appName, suffix)
				routes = append(routes, route{
					publicHost:   publicDomain,
					internalHost: internalDomain,
					target:       fmt.Sprintf("http://%s:%d", targetHost, defaultPort),
				})
			}
		}
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].publicHost != routes[j].publicHost {
			return routes[i].publicHost < routes[j].publicHost
		}
		return routes[i].target < routes[j].target
	})

	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("\tadmin 0.0.0.0:2019\n")
	if email := strings.TrimSpace(os.Getenv("CADDY_ACME_EMAIL")); email != "" {
		fmt.Fprintf(&b, "\temail %s\n", email)
	}
	provider := strings.TrimSpace(os.Getenv("CADDY_DNS_PROVIDER"))
	hasDNS := provider != ""
	if hasDNS {
		if acmeCA := strings.TrimSpace(os.Getenv("CADDY_ACME_CA")); acmeCA != "" {
			fmt.Fprintf(&b, "\tacme_ca %s\n", acmeCA)
		}
	}
	b.WriteString("}\n\n")
	dnsTokenEnv := strings.TrimSpace(os.Getenv("CADDY_DNS_TOKEN_ENV"))
	for _, route := range routes {
		if hasDNS {
			fmt.Fprintf(&b, "%s {\n", route.publicHost)
			if dnsTokenEnv != "" {
				fmt.Fprintf(&b, "\ttls {\n\t\tdns %s {env.%s}\n\t}\n", provider, dnsTokenEnv)
			}
			fmt.Fprintf(&b, "\treverse_proxy %s\n", route.target)
			b.WriteString("}\n\n")
			fmt.Fprintf(&b, "%s {\n", route.internalHost)
			if dnsTokenEnv != "" {
				fmt.Fprintf(&b, "\ttls {\n\t\tdns %s {env.%s}\n\t}\n", provider, dnsTokenEnv)
			}
			fmt.Fprintf(&b, "\treverse_proxy %s\n", route.target)
			b.WriteString("}\n\n")
		} else {
			fmt.Fprintf(&b, "%s, %s {\n", route.publicHost, route.internalHost)
			fmt.Fprintf(&b, "\treverse_proxy %s\n", route.target)
			b.WriteString("}\n\n")
		}
	}

	// Gateway health check route for keepalived VRRP
	b.WriteString("http:// {\n")
	b.WriteString("\thandle /is-gateway {\n")
	b.WriteString("\t\treverse_proxy 127.0.0.1:1337\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n\n")

	return b.String(), nil
}

// GenerateTraefikConfig is kept for mixed-version compatibility. It now emits
// the Caddyfile consumed by the compatibility /traefik-config endpoint.
func (m *Manager) GenerateTraefikConfig(forNodeID string) (string, error) {
	return m.GenerateCaddyConfig(forNodeID)
}
