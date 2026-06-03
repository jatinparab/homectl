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

	// Detect by running ip route from inside a host-network container
	cmd := exec.Command("docker", "run", "--rm", "--network", "host",
		"--entrypoint", "/sbin/ip",
		"homectl/agent:local",
		"route", "show", "default")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run ip route in host network: %w", err)
	}

	fields := strings.Fields(string(out))
	if len(fields) >= 5 {
		iface := strings.TrimSpace(fields[4])
		if iface != "" {
			return iface, nil
		}
	}

	return "", fmt.Errorf("no default route found from host network")
}
