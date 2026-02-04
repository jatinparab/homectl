package config

import "github.com/jatinparab/homectl/utils"

type Config struct {
	managerURL string
	agents     map[string]AgentConfig
}

type AgentConfig struct {
	Name       string
	IP         string
	AppdataDir string
}

func (c *Config) GetManagerURL() string {
	return c.managerURL
}

func (c *Config) GetAgentConfig(hostname string) *AgentConfig {
	// Special case for Local development
	if hostname == "Jatins-MacBook-Pro.local" {
		return &AgentConfig{
			Name:       "Jatins-MacBook-Pro",
			IP:         utils.GetLocalIP(),
			AppdataDir: "/Users/jatin/homectl/storage",
		}
	}

	if config, ok := c.agents[hostname]; ok {
		return &config
	}
	return nil
}

func GetGitRepoDir(hostname string) string {
	if hostname == "Jatins-MacBook-Pro.local" {
		return "/Users/jatin/Personal/homelab"
	}
	return "/home/jatin/homelab"
}

func NewConfig() *Config {
	return &Config{
		managerURL: "http://localhost:8080",
		agents: map[string]AgentConfig{
			"k8s-node-1": {
				Name:       "k8s-node-1",
				IP:         "192.168.50.101",
				AppdataDir: "/mnt/ssd-512/homectl/storage",
			},
			"rpi-1": {
				Name:       "rpi-1",
				IP:         "192.168.50.102",
				AppdataDir: "/home/jatin/homectl/storage",
			},
			"node-3": {
				Name:       "node-3",
				IP:         "192.168.50.103",
				AppdataDir: "/home/jatin/homectl/storage",
			},
		},
	}
}
