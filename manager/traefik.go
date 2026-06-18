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
// agentRoute describes one reverse-proxy route in an agent's Caddyfile.
type agentRoute struct {
	publicHost   string
	internalHost string
	target       string // simple http://host:port for local targets
	isLocal      bool   // true when the winning candidate runs on this node
	remoteHost   string // WireGuard IP of the remote agent (empty if local)
}

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

func backupEnabledForApp(appID common.AppID, configs map[string]HomectlConfig) bool {
	cfg, ok := configs[string(appID)]
	if ok && cfg.BackupEnabled != nil {
		return *cfg.BackupEnabled
	}
	return appID != common.GatewayProxyAppID
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
// When multiple agents report the same app, only one route is emitted per
// public hostname. The winner is chosen with deterministic priority:
// current gateway > local node being rendered > alphabetical by agent name.
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

	gatewayNode := m.getGatewayNode()

	// Collect app report candidates per app, so we can pick one winner
	type candidate struct {
		agentName string
		agentIP   string
		report    common.AppReport
		isGateway bool
		isLocal   bool
	}
	appCandidates := make(map[string][]candidate)

	for _, agent := range agents {
		if !alive[agent.Name] {
			continue
		}
		for _, report := range agent.AppReports {
			if report.ObservedState != common.ObservedRunning {
				continue
			}
			if report.AppID == common.GatewayProxyAppID {
				continue
			}

			appName := string(report.AppID)
			appCandidates[appName] = append(appCandidates[appName], candidate{
				agentName: agent.Name,
				agentIP:   agent.IP,
				report:    report,
				isGateway: gatewayNode != "" && agent.Name == gatewayNode,
				isLocal:   agent.Name == forNodeID,
			})
		}
	}

	// Pick one winning candidate per app: gateway > local > alphabetical
	type appRouting struct {
		appName   string
		target    string
		host      string
		candidate candidate
	}
	routingMap := make(map[string]appRouting, len(appCandidates))

	for appName, candidates := range appCandidates {
		sort.Slice(candidates, func(i, j int) bool {
			// Gateway first
			if candidates[i].isGateway != candidates[j].isGateway {
				return candidates[i].isGateway
			}
			// Local node second
			if candidates[i].isLocal != candidates[j].isLocal {
				return candidates[i].isLocal
			}
			// Deterministic by agent name
			return candidates[i].agentName < candidates[j].agentName
		})

		winner := candidates[0]

		// Determine target host based on local vs remote.
		// Local apps are reached via the homectl Docker network alias.
		// Remote apps are proxied to the remote agent's caddy over WireGuard.
		var targetHost string
		var upstreamPort int
		if winner.isLocal {
			targetHost = docker.HealthAlias(appName)
			upstreamPort = winner.report.Port
		} else {
			targetHost = winner.agentIP
			upstreamPort = 80
		}

		routingMap[appName] = appRouting{
			appName:   appName,
			target:    fmt.Sprintf("http://%s:%d", targetHost, upstreamPort),
			host:      targetHost,
			candidate: winner,
		}
	}

	// Build routes from winning candidates
	routes := make([]agentRoute, 0)
	for _, routing := range routingMap {
		appName := routing.appName
		baseTarget := routing.target
		report := routing.candidate.report

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

				// When routing to a remote agent, proxy to the agent's Caddy
				// on port 80 (the WireGuard overlay port), not the app's
				// internal port. The remote Caddy handles TLS and routes to
				// the actual app port internally.
				targetPort := int(port)
				if !routing.candidate.isLocal {
					targetPort = 80
				}

				publicDomain := fmt.Sprintf("%s.%s", subdomain, suffix)
				internalDomain := fmt.Sprintf("%s.internal.%s", subdomain, suffix)
				routes = append(routes, agentRoute{
					publicHost:   publicDomain,
					internalHost: internalDomain,
					target:       fmt.Sprintf("http://%s:%d", routing.host, targetPort),
					isLocal:      routing.candidate.isLocal,
					remoteHost:   routing.host,
				})
			}
		} else {
			// Default behavior: use app name as subdomain and reported port
			if report.Port == 0 {
				continue // Skip apps without exposed ports
			}

			publicDomain := fmt.Sprintf("%s.%s", appName, suffix)
			internalDomain := fmt.Sprintf("%s.internal.%s", appName, suffix)
			routes = append(routes, agentRoute{
				publicHost:   publicDomain,
				internalHost: internalDomain,
				target:       baseTarget,
				isLocal:      routing.candidate.isLocal,
				remoteHost:   routing.host,
			})
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
	if email := strings.TrimSpace(m.configValue(common.ConfigKeyCaddyACMEEmail)); email != "" {
		fmt.Fprintf(&b, "\temail %s\n", email)
	}
	provider := strings.TrimSpace(m.configValue(common.ConfigKeyCaddyDNSProvider))
	hasDNS := provider != ""
	if hasDNS {
		if acmeCA := strings.TrimSpace(m.configValue(common.ConfigKeyCaddyACMECA)); acmeCA != "" {
			fmt.Fprintf(&b, "\tacme_ca %s\n", acmeCA)
		}
	}
	b.WriteString("}\n\n")
	dnsTokenEnv := strings.TrimSpace(m.configValue(common.ConfigKeyCaddyDNSTokenEnv))
	for _, r := range routes {
		if hasDNS {
			fmt.Fprintf(&b, "%s {\n", r.publicHost)
			if dnsTokenEnv != "" {
				fmt.Fprintf(&b, "\ttls {\n\t\tdns %s {env.%s}\n\t}\n", provider, dnsTokenEnv)
			}
			writeRouteProxy(&b, r)
			b.WriteString("}\n\n")
			fmt.Fprintf(&b, "%s {\n", r.internalHost)
			if dnsTokenEnv != "" {
				fmt.Fprintf(&b, "\ttls {\n\t\tdns %s {env.%s}\n\t}\n", provider, dnsTokenEnv)
			}
			writeRouteProxy(&b, r)
			b.WriteString("}\n\n")
		} else {
			fmt.Fprintf(&b, "%s, %s {\n", r.publicHost, r.internalHost)
			writeRouteProxy(&b, r)
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

// writeRouteProxy emits a reverse_proxy directive for a route block.
// Local apps (Docker aliases) use plain HTTP. Remote agents use HTTPS
// with TLS SNI and Host header forwarding, matching how the manager
// proxies to agents over WireGuard.
func writeRouteProxy(b *strings.Builder, r agentRoute) {
	if r.isLocal {
		fmt.Fprintf(b, "\treverse_proxy %s\n", r.target)
		return
	}
	fmt.Fprintf(b, "\treverse_proxy https://%s {\n", r.remoteHost)
	fmt.Fprintf(b, "\t\theader_up Host {host}\n")
	fmt.Fprintf(b, "\t\ttransport http {\n")
	fmt.Fprintf(b, "\t\t\ttls_server_name %s\n", r.publicHost)
	fmt.Fprintf(b, "\t\t}\n")
	fmt.Fprintf(b, "\t}\n")
}
