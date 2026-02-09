package manager

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jatinparab/homectl/common"
	"gopkg.in/yaml.v3"
)

const (
	// DomainSuffix is the base domain for traefik routes
	DomainSuffix = "sulphurgfx.com"
)

// HomectlConfig represents the homectl-config.json file for an app
type HomectlConfig struct {
	ExposedSubdomains map[string]int `json:"exposed_subdomains,omitempty"`
	ExcludeNodes      []string       `json:"exclude_nodes,omitempty"`
}

// TraefikConfig represents the traefik dynamic configuration
type TraefikConfig struct {
	HTTP TraefikHTTP `yaml:"http"`
}

// TraefikHTTP contains routers and services
type TraefikHTTP struct {
	Routers  map[string]TraefikRouter  `yaml:"routers,omitempty"`
	Services map[string]TraefikService `yaml:"services,omitempty"`
}

// TraefikRouter defines a traefik router
type TraefikRouter struct {
	Rule        string   `yaml:"rule"`
	Service     string   `yaml:"service"`
	EntryPoints []string `yaml:"entryPoints"`
}

// TraefikService defines a traefik service
type TraefikService struct {
	LoadBalancer TraefikLoadBalancer `yaml:"loadBalancer"`
}

// TraefikLoadBalancer defines load balancer settings
type TraefikLoadBalancer struct {
	Servers []TraefikServer `yaml:"servers"`
}

// TraefikServer defines a backend server
type TraefikServer struct {
	URL string `yaml:"url"`
}

// loadAppConfigs loads homectl-config.json files from docker/v2 directories
func (m *Manager) loadAppConfigs() map[string]HomectlConfig {
	configs := make(map[string]HomectlConfig)

	dockerV2Dir := filepath.Join(m.repoDir, "docker", "v2")
	entries, err := os.ReadDir(dockerV2Dir)
	if err != nil {
		log.Printf("Failed to read docker/v2 directory: %s", err)
		return configs
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appName := entry.Name()
		configPath := filepath.Join(dockerV2Dir, appName, "homectl-config.json")

		data, err := os.ReadFile(configPath)
		if err != nil {
			// Config file doesn't exist or can't be read - that's fine
			continue
		}

		var cfg HomectlConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			log.Printf("Failed to parse %s: %s", configPath, err)
			continue
		}

		configs[appName] = cfg
	}

	return configs
}

// GenerateTraefikConfig generates traefik config for a specific node
// For local apps (same node): use 172.17.0.1 (docker gateway)
// For remote apps: use the remote node's IP
func (m *Manager) GenerateTraefikConfig(forNodeID string) (string, error) {
	config := TraefikConfig{
		HTTP: TraefikHTTP{
			Routers:  make(map[string]TraefikRouter),
			Services: make(map[string]TraefikService),
		},
	}

	// Load app configs for exposed_subdomains overrides
	appConfigs := m.loadAppConfigs()

	// Get all agents and their app reports
	agents := m.leaseManager.ListAgents()

	for _, agent := range agents {
		for _, report := range agent.AppReports {
			// Skip non-running apps
			if report.ObservedState != common.ObservedRunning {
				continue
			}

			appName := string(report.AppID)

			// Determine IP based on local vs remote
			var ip string
			if agent.Name == forNodeID {
				ip = "172.17.0.1" // Docker gateway for local
			} else {
				ip = agent.IP // Remote node IP
			}

			// Check if app has exposed_subdomains override
			if appCfg, ok := appConfigs[appName]; ok && len(appCfg.ExposedSubdomains) > 0 {
				// Use custom subdomains from homectl-config.json
				for subdomain, port := range appCfg.ExposedSubdomains {
					routerName := fmt.Sprintf("%s-%s", appName, subdomain)
					serviceName := fmt.Sprintf("%s-%s", appName, subdomain)

					publicDomain := fmt.Sprintf("%s.%s", subdomain, DomainSuffix)
					internalDomain := fmt.Sprintf("%s.internal.%s", subdomain, DomainSuffix)

					config.HTTP.Routers[routerName] = TraefikRouter{
						Rule:        fmt.Sprintf("Host(`%s`) || Host(`%s`)", internalDomain, publicDomain),
						Service:     serviceName,
						EntryPoints: []string{"web", "websecure"},
					}

					config.HTTP.Services[serviceName] = TraefikService{
						LoadBalancer: TraefikLoadBalancer{
							Servers: []TraefikServer{
								{URL: fmt.Sprintf("http://%s:%d", ip, port)},
							},
						},
					}
				}
			} else {
				// Default behavior: use app name as subdomain and reported port
				if report.Port == 0 {
					continue // Skip apps without exposed ports
				}

				publicDomain := fmt.Sprintf("%s.%s", appName, DomainSuffix)
				internalDomain := fmt.Sprintf("%s.internal.%s", appName, DomainSuffix)

				config.HTTP.Routers[appName] = TraefikRouter{
					Rule:        fmt.Sprintf("Host(`%s`) || Host(`%s`)", internalDomain, publicDomain),
					Service:     appName,
					EntryPoints: []string{"web", "websecure"},
				}

				config.HTTP.Services[appName] = TraefikService{
					LoadBalancer: TraefikLoadBalancer{
						Servers: []TraefikServer{
							{URL: fmt.Sprintf("http://%s:%d", ip, report.Port)},
						},
					},
				}
			}
		}
	}

	// Marshal to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal traefik config: %w", err)
	}

	return string(data), nil
}
