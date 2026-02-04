package config

import "github.com/jatinparab/homectl/utils"

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

func (c *Config) GetManagerURL() string {
	hostname := utils.GetHostname()
	if hostname == "Jatins-MacBook-Pro.local" {
		return "http://localhost:1338"
	}
	return c.managerURL
}

func (c *Config) GetAgentConfig(hostname string) *AgentConfig {
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
		managerURL: "http://192.168.1.11:1338",
		agents: map[string]AgentConfig{
			"k8s-node-1": {
				Name:          "k8s-node-1",
				IP:            "192.168.1.101",
				User:          "jatin",
				AppdataDir:    "/mnt/ssd-512/homectl/storage",
				DockerRootDir: "/mnt/ssd-512/homectl/docker",
			},
			"rpi-1": {
				Name:          "rpi-1",
				IP:            "192.168.1.102",
				User:          "jatin",
				AppdataDir:    "/home/jatin/homectl/storage",
				DockerRootDir: "/home/jatin/homectl/docker",
			},
			"windranger": {
				Name:          "windranger",
				IP:            "192.168.1.103",
				User:          "jatin",
				AppdataDir:    "/home/jatin/homectl/storage",
				DockerRootDir: "/home/jatin/homectl/docker",
			},
		},
	}
}

var Conf = NewConfig()
