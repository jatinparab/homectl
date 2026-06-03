package config

import (
	"os"

	"github.com/jatinparab/homectl/utils"
	"github.com/jatinparab/homectl/wireguard"
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
	if url := os.Getenv("HOMECTL_MANAGER_URL"); url != "" {
		return url
	}
	return c.managerURL
}

func (c *Config) GetAgentConfig(hostname string) *AgentConfig {
	name := envOrDefault("HOMECTL_AGENT_NAME", hostname)
	if name == "" {
		name = "agent"
	}
	ip := os.Getenv("HOMECTL_AGENT_IP")
	if ip == "" {
		ip = utils.GetLocalIP()
	}
	return &AgentConfig{
		Name:          name,
		IP:            ip,
		User:          envOrDefault("HOMECTL_USER", "root"),
		AppdataDir:    envOrDefault("HOMECTL_APPDATA_DIR", "/opt/homectl/appdata"),
		DockerRootDir: envOrDefault("HOMECTL_DOCKER_ROOT_DIR", "/opt/homectl/docker"),
	}
}

func GetGitRepoDir(hostname string) string {
	if dir := os.Getenv("HOMECTL_REPO_DIR"); dir != "" {
		return dir
	}
	return "/opt/homectl/repo"
}

func NewConfig() *Config {
	return &Config{
		managerURL: "http://" + wireguard.ManagerAddress + ":1338",
		agents:     map[string]AgentConfig{},
	}
}

var Conf = NewConfig()
