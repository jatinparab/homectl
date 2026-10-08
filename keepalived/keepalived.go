package keepalived

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ContainerName = "homectl-keepalived"
const DNSContainerName = "homectl-dns-keepalived"

// EnsureDNS manages an independent election: the DNS owner need not be the
// HTTP gateway or Technitium primary. VRID 45 is distinct from gateway VRID 44.
func EnsureDNS(configDir, hostConfigDir, dnsIP, gatewayIP string) error {
	if dnsIP == "" {
		out, err := exec.Command("docker", "rm", "-f", DNSContainerName).CombinedOutput()
		if err != nil && !strings.Contains(string(out), "No such container") {
			return fmt.Errorf("remove DNS keepalived: %w: %s", err, out)
		}
		return nil
	}
	ip := net.ParseIP(dnsIP)
	if ip == nil || ip.To4() == nil || dnsIP != ip.To4().String() || !ip.IsGlobalUnicast() || ip.Equal(net.ParseIP(gatewayIP)) {
		return fmt.Errorf("DNS VIP must be a separate IPv4 address")
	}
	iface, err := hostInterface()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	// The ready port is published on host loopback. DNS election must not
	// depend on Caddy, the manager, or gateway designation.
	check := "#!/bin/sh\nwget -qO- --timeout=3 http://127.0.0.1:1337/dns-ready >/dev/null\n"
	if err := os.WriteFile(filepath.Join(configDir, "check.sh"), []byte(check), 0755); err != nil {
		return err
	}
	content := dnsConfig(iface, dnsIP)
	path := filepath.Join(configDir, "keepalived.conf")
	previous, _ := os.ReadFile(path)
	if string(previous) == content {
		out, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", DNSContainerName).Output()
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			return nil // unrelated config changes must not reset a working election
		}
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	return reconcileNamedContainer(hostConfigDir, DNSContainerName)
}

func dnsConfig(iface, ip string) string {
	return fmt.Sprintf(`global_defs {
	enable_script_security
	script_user root
}

vrrp_script dns_ready {
	script "/etc/keepalived/check.sh"
	interval 2
	timeout 4
	fall 2
	rise 3
	init_fail
}

vrrp_instance DNS_VIP {
	state BACKUP
	interface %s
	virtual_router_id 45
	priority 120
	advert_int 1
	nopreempt

	virtual_ipaddress {
		%s
	}
	track_script {
		dns_ready
	}
}
`, iface, ip)
}

// OwnsDNSVIP checks actual host addresses in the host-network VRRP container,
// rather than trusting a possibly stale notify marker. Errors fail closed.
func OwnsDNSVIP(ctx context.Context, vip string) (bool, error) {
	out, err := exec.CommandContext(ctx, "docker", "exec", DNSContainerName, "ip", "-o", "-4", "addr", "show").Output()
	if err != nil {
		return false, fmt.Errorf("inspect DNS VIP ownership: %w", err)
	}
	return hasVIP(string(out), vip), nil
}

func hasVIP(addresses, vip string) bool {
	for _, field := range strings.Fields(addresses) {
		ip, _, err := net.ParseCIDR(field)
		if err == nil && ip.Equal(net.ParseIP(vip)) {
			return true
		}
	}
	return false
}

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
	return reconcileNamedContainer(configDir, ContainerName)
}

func reconcileNamedContainer(configDir, name string) error {
	_ = exec.Command("docker", "rm", "-f", name).Run()

	args := []string{
		"run", "-d",
		"--name", name,
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
