package config

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/jatinparab/homectl/utils"
	"github.com/jatinparab/homectl/wireguard"
)

type Config struct {
	managerURL string
	agents     map[string]AgentConfig
}

type AgentConfig struct {
	Name             string
	IP               string
	User             string
	AppdataDir       string
	DockerRootDir    string
	ComposeExtraArgs []string
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
	composeExtraArgs, err := parseComposeExtraArgs(os.Getenv("HOMECTL_COMPOSE_EXTRA_ARGS"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid HOMECTL_COMPOSE_EXTRA_ARGS: %v\n", err)
		composeExtraArgs = nil
	}
	return &AgentConfig{
		Name:             name,
		IP:               ip,
		User:             envOrDefault("HOMECTL_USER", "root"),
		AppdataDir:       envOrDefault("HOMECTL_APPDATA_DIR", "/opt/homectl/appdata"),
		DockerRootDir:    envOrDefault("HOMECTL_DOCKER_ROOT_DIR", "/opt/homectl/docker"),
		ComposeExtraArgs: composeExtraArgs,
	}
}

func parseComposeExtraArgs(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	var args []string
	var b strings.Builder
	var quote rune
	escaped := false
	inToken := false

	flush := func() {
		if inToken {
			args = append(args, b.String())
			b.Reset()
			inToken = false
		}
	}

	for _, r := range value {
		switch {
		case escaped:
			b.WriteRune(r)
			inToken = true
			escaped = false
		case r == '\\':
			escaped = true
			inToken = true
		case quote != 0:
			if r == quote {
				quote = 0
				inToken = true
			} else {
				b.WriteRune(r)
				inToken = true
			}
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case unicode.IsSpace(r):
			flush()
		default:
			b.WriteRune(r)
			inToken = true
		}
	}

	if escaped {
		return nil, fmt.Errorf("unterminated escape in HOMECTL_COMPOSE_EXTRA_ARGS")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in HOMECTL_COMPOSE_EXTRA_ARGS")
	}
	flush()
	return args, nil
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
