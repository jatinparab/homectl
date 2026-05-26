package config

import (
	"os"

	"github.com/jatinparab/homectl/utils"
)

type Config struct {
	managerURL string
	agents     map[string]AgentConfig
}

type AgentConfig struct {
	Name          string
	IP            string
	User          string
	AppdataDir    string
	DockerRootDir string
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (c *Config) GetManagerURL() string {
	// Env var override (for containerised / e2e deployments)
	if url := os.Getenv("HOMECTL_MANAGER_URL"); url != "" {
		return url
	}
	hostname := utils.GetHostname()
	if hostname == "Jatins-MacBook-Pro.local" {
		return "http://localhost:1338"
	}
	return c.managerURL
}

func (c *Config) GetAgentConfig(hostname string) *AgentConfig {
	// Env var override (for containerised / e2e deployments)
	if name := os.Getenv("HOMECTL_AGENT_NAME"); name != "" {
		return &AgentConfig{
			Name:          name,
			IP:            utils.GetLocalIP(),
			User:          envOrDefault("HOMECTL_USER", "root"),
			AppdataDir:    envOrDefault("HOMECTL_APPDATA_DIR", "/appdata"),
			DockerRootDir: envOrDefault("HOMECTL_DOCKER_ROOT_DIR", "/docker"),
		}
	}

	// Special case for Local development
	if hostname == "Jatins-MacBook-Pro.local" {
		return &AgentConfig{
			Name:          "Jatins-MacBook-Pro",
			IP:            utils.GetLocalIP(),
			User:          "jatin",
			AppdataDir:    "/Users/jatin/homectl/storage",
			DockerRootDir: "/Users/jatin/homectl/docker",
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
		managerURL: "http://10.42.1.100:1338",
		agents: map[string]AgentConfig{
			"akira": {
				Name:          "akira",
				IP:            "10.42.1.101",
				User:          "jatin",
				AppdataDir:    "/home/jatin/homectl/storage",
				DockerRootDir: "/home/jatin/homectl/docker",
			},
			"rpi-1": {
				Name:          "rpi-1",
				IP:            "10.42.1.102",
				User:          "jatin",
				AppdataDir:    "/home/jatin/homectl/storage",
				DockerRootDir: "/home/jatin/homectl/docker",
			},
			"windranger": {
				Name:          "windranger",
				IP:            "10.42.1.103",
				User:          "jatin",
				AppdataDir:    "/home/jatin/homectl/storage",
				DockerRootDir: "/home/jatin/homectl/docker",
			},
		},
	}
}

var Conf = NewConfig()
