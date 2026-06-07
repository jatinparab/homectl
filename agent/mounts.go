package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type containerMount struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

type inspectedContainer struct {
	Mounts []containerMount `json:"Mounts"`
}

func resolveSelfBindMountSource(destination string) (string, error) {
	candidates := []string{"homectl-agent"}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		candidates = append(candidates, strings.TrimSpace(hostname))
	}

	var lastErr error
	for _, candidate := range uniqueStrings(candidates) {
		source, err := resolveBindMountSource(candidate, destination)
		if err == nil {
			return source, nil
		}
		lastErr = err
	}
	if isLocalDockerInDocker() {
		return destination, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("%s must be mounted as a host bind mount", destination)
}

func resolveBindMountSource(containerName, destination string) (string, error) {
	out, err := exec.Command("docker", "inspect", containerName).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect container %q: %w: %s", containerName, err, strings.TrimSpace(string(out)))
	}
	return bindMountSourceFromInspect(out, destination)
}

func bindMountSourceFromInspect(data []byte, destination string) (string, error) {
	var containers []inspectedContainer
	if err := json.Unmarshal(data, &containers); err != nil {
		return "", fmt.Errorf("parse docker inspect output: %w", err)
	}
	for _, container := range containers {
		for _, mount := range container.Mounts {
			if mount.Destination != destination {
				continue
			}
			if mount.Type != "bind" {
				return "", fmt.Errorf("%s must be a host bind mount, got %s", destination, mount.Type)
			}
			if strings.TrimSpace(mount.Source) == "" {
				return "", fmt.Errorf("%s bind mount source is empty", destination)
			}
			return mount.Source, nil
		}
	}
	return "", fmt.Errorf("%s must be mounted as a host bind mount", destination)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func isLocalDockerInDocker() bool {
	if _, err := os.Stat("/var/lib/docker"); err != nil {
		return false
	}
	out, err := exec.Command("docker", "info", "--format", "{{.DockerRootDir}}").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "/var/lib/docker"
}
