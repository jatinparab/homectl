package keepalived

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ContainerName = "homectl-keepalived"

func Ensure(configDir, hostConfigDir, haIP string) error {
	if haIP == "" {
		return nil
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	iface, err := hostInterface()
	if err != nil {
		return fmt.Errorf("detect host interface: %w", err)
	}

	checkPath := filepath.Join(configDir, "check.sh")
	checkScript := `#!/bin/sh
wget -qO- --timeout=2 http://127.0.0.1/is-gateway >/dev/null
`
	if err := os.WriteFile(checkPath, []byte(checkScript), 0755); err != nil {
		return fmt.Errorf("write check script: %w", err)
	}

	containerCheckPath := "/etc/keepalived/check.sh"
	config := fmt.Sprintf(`global_defs {
	enable_script_security
	script_user root
}

vrrp_script keepalived_check {
	script "%s"
	interval 2
	fall 3
	rise 2
}

vrrp_instance VI_1 {
	state BACKUP
	interface %s
	virtual_router_id 44
	priority 120
	advert_int 1
	nopreempt

	virtual_ipaddress {
		%s
	}

	track_script {
		keepalived_check
	}
}
`, containerCheckPath, iface, haIP)

	configPath := filepath.Join(configDir, "keepalived.conf")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("write keepalived config: %w", err)
	}

	return reconcileContainer(hostConfigDir)
}

func Stop() error {
	return exec.Command("docker", "rm", "-f", ContainerName).Run()
}

func reconcileContainer(configDir string) error {
	_ = exec.Command("docker", "rm", "-f", ContainerName).Run()

	args := []string{
		"run", "-d",
		"--name", ContainerName,
		"--restart", "unless-stopped",
		"--network", "host",
		"--cap-add", "NET_ADMIN",
		"--cap-add", "NET_RAW",
		"-v", configDir + ":/etc/keepalived:ro",
	}
	args = append(args, "osixia/keepalived")

	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start keepalived container: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func RemoveInterface(ifaceName string) {
	_ = exec.Command("ip", "link", "del", ifaceName).Run()
}

func hostInterface() (string, error) {
	if iface := os.Getenv("HOMECTL_LOCAL_HA_INTERFACE"); iface != "" {
		return iface, nil
	}

	var errs []string
	for _, image := range hostProbeImages() {
		// Detect by running ip route from inside a host-network container.
		cmd := exec.Command("docker", "run", "--rm", "--network", "host",
			"--entrypoint", "/sbin/ip",
			image,
			"route", "show", "default")
		out, err := cmd.CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v: %s", image, err, strings.TrimSpace(string(out))))
			continue
		}

		if iface := defaultRouteInterface(string(out)); iface != "" {
			return iface, nil
		}
		errs = append(errs, fmt.Sprintf("%s: no default route found in output %q", image, strings.TrimSpace(string(out))))
	}

	return "", fmt.Errorf("run ip route in host network: %s", strings.Join(errs, "; "))
}

func hostProbeImages() []string {
	var images []string
	if image := strings.TrimSpace(os.Getenv("HOMECTL_AGENT_IMAGE")); image != "" {
		images = append(images, image)
	}

	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		cmd := exec.Command("docker", "inspect", "--format", "{{.Config.Image}}", hostname)
		if out, err := cmd.Output(); err == nil {
			if image := strings.TrimSpace(string(out)); image != "" && image != "<no value>" {
				images = append(images, image)
			}
		}
	}

	images = append(images, "homectl/agent:local", "homectl/agent:latest")
	return uniqueStrings(images)
}

func defaultRouteInterface(routeOutput string) string {
	for _, line := range strings.Split(routeOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		for i, field := range fields {
			if field == "dev" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	return ""
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
