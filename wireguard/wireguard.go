package wireguard

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	DefaultPort    = 51844
	DefaultNetwork = "10.44.0.0/24"
	ManagerAddress = "10.44.0.1"
	InterfaceName  = "wg0"
)

type Peer struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	Address   string `json:"address"`
}

type ManagerConfig struct {
	ConfigDir  string
	EndpointIP string
	ListenPort int
	PrivateKey string
	Peers      []Peer
}

type AgentConfig struct {
	ConfigDir        string
	Address          string
	PrivateKey       string
	ManagerPublicKey string
	EndpointIP       string
	EndpointPort     int
}

func RegenerateKeyPair(dir string) (privateKey, publicKey string, err error) {
	if err := os.RemoveAll(dir); err != nil {
		return "", "", err
	}
	return EnsureKeyPair(dir)
}

func EnsureKeyPair(dir string) (privateKey, publicKey string, err error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	privatePath := filepath.Join(dir, "privatekey")
	publicPath := filepath.Join(dir, "publickey")

	privateBytes, err := os.ReadFile(privatePath)
	if err == nil {
		privateKey = strings.TrimSpace(string(privateBytes))
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	if privateKey == "" {
		privateKey, err = generatePrivateKey()
		if err != nil {
			return "", "", err
		}
		if err := os.WriteFile(privatePath, []byte(privateKey+"\n"), 0600); err != nil {
			return "", "", err
		}
	}

	publicBytes, err := os.ReadFile(publicPath)
	if err == nil && strings.TrimSpace(string(publicBytes)) != "" {
		return privateKey, strings.TrimSpace(string(publicBytes)), nil
	}
	publicKey, err = PublicKey(privateKey)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(publicPath, []byte(publicKey+"\n"), 0644); err != nil {
		return "", "", err
	}
	return privateKey, publicKey, nil
}

func PublicKey(privateKey string) (string, error) {
	cmd := exec.Command("wg", "pubkey")
	cmd.Stdin = strings.NewReader(strings.TrimSpace(privateKey) + "\n")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("wg pubkey: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func EnsureManager(cfg ManagerConfig) error {
	if cfg.ListenPort == 0 {
		cfg.ListenPort = DefaultPort
	}
	if strings.TrimSpace(cfg.EndpointIP) == "" {
		return errors.New("manager endpoint IP is required")
	}
	if ip := net.ParseIP(strings.TrimSpace(cfg.EndpointIP)); ip == nil || ip.To4() == nil {
		return fmt.Errorf("invalid manager endpoint IP %q", cfg.EndpointIP)
	}
	if err := os.MkdirAll(cfg.ConfigDir, 0700); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\n")
	fmt.Fprintf(&b, "Address = %s/24\n", ManagerAddress)
	fmt.Fprintf(&b, "ListenPort = %d\n", cfg.ListenPort)
	fmt.Fprintf(&b, "PrivateKey = %s\n\n", strings.TrimSpace(cfg.PrivateKey))
	for _, peer := range cfg.Peers {
		if peer.PublicKey == "" || peer.Address == "" {
			continue
		}
		fmt.Fprintf(&b, "[Peer]\n")
		fmt.Fprintf(&b, "# %s\n", peer.Name)
		fmt.Fprintf(&b, "PublicKey = %s\n", strings.TrimSpace(peer.PublicKey))
		fmt.Fprintf(&b, "AllowedIPs = %s/32\n\n", strings.TrimSpace(peer.Address))
	}
	return applyConfig(cfg.ConfigDir, b.String())
}

func EnsureAgent(cfg AgentConfig) error {
	if cfg.EndpointPort == 0 {
		cfg.EndpointPort = DefaultPort
	}
	if ip := net.ParseIP(strings.TrimSpace(cfg.EndpointIP)); ip == nil || ip.To4() == nil {
		return fmt.Errorf("invalid manager endpoint IP %q", cfg.EndpointIP)
	}
	address := net.ParseIP(strings.TrimSpace(cfg.Address))
	if address == nil || address.To4() == nil {
		return fmt.Errorf("invalid agent WireGuard IPv4 address %q", cfg.Address)
	}
	cfg.Address = address.To4().String()
	if err := os.MkdirAll(cfg.ConfigDir, 0700); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\n")
	fmt.Fprintf(&b, "Address = %s/24\n", strings.TrimSpace(cfg.Address))
	fmt.Fprintf(&b, "PrivateKey = %s\n\n", strings.TrimSpace(cfg.PrivateKey))
	fmt.Fprintf(&b, "[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", strings.TrimSpace(cfg.ManagerPublicKey))
	fmt.Fprintf(&b, "AllowedIPs = %s\n", DefaultNetwork)
	fmt.Fprintf(&b, "Endpoint = %s:%d\n", strings.TrimSpace(cfg.EndpointIP), cfg.EndpointPort)
	fmt.Fprintf(&b, "PersistentKeepalive = 25\n")
	if err := applyConfig(cfg.ConfigDir, b.String()); err != nil {
		return err
	}
	return syncAgentAddress(strings.TrimSpace(cfg.Address))
}

// wg syncconf deliberately ignores wg-quick's Address directive. A rejoining
// agent can receive a different reservation while wg0 still exists, so update
// the kernel address as well as the config and reported identity.
func syncAgentAddress(address string) error {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "dev", InterfaceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect agent WireGuard address: %w: %s", err, strings.TrimSpace(string(out)))
	}
	want := address + "/24"
	var addresses []string
	fields := strings.Fields(string(out))
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "inet" {
			addresses = append(addresses, fields[i+1])
		}
	}
	if len(addresses) == 1 && addresses[0] == want {
		return nil
	}
	// wg0 belongs exclusively to Homectl. Flush stale IPv4 addresses before
	// adding the assignment; deleting a primary after adding a secondary can
	// otherwise cause Linux to remove both addresses in the same subnet.
	if out, err := exec.Command("ip", "-4", "addr", "flush", "dev", InterfaceName).CombinedOutput(); err != nil {
		return fmt.Errorf("clear stale agent WireGuard address: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("ip", "-4", "addr", "add", want, "dev", InterfaceName).CombinedOutput(); err != nil {
		return fmt.Errorf("assign agent WireGuard address: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func IsActive() bool {
	cmd := exec.Command("wg", "show", InterfaceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "interface: "+InterfaceName)
}

func Teardown() {
	_ = exec.Command("wg-quick", "down", InterfaceName).Run()
}

func Syncconf(configDir string) error {
	conf, err := readWGConfig(configDir)
	if err != nil {
		return err
	}
	conf = stripWGQuickDirectives(conf)
	cmd := exec.Command("wg", "syncconf", InterfaceName, "/dev/stdin")
	cmd.Stdin = strings.NewReader(conf)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wg syncconf: %w: %s", err, strings.TrimSpace(string(output)))
	}
	// Update the kernel route so that agents can reach the full overlay
	// network through the manager hub (not just the manager's own IP).
	_ = exec.Command("ip", "route", "replace", DefaultNetwork, "dev", InterfaceName).Run()
	return nil
}

func stripWGQuickDirectives(config string) string {
	lines := strings.Split(config, "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Address") ||
			strings.HasPrefix(trimmed, "DNS") ||
			strings.HasPrefix(trimmed, "MTU") ||
			strings.HasPrefix(trimmed, "Table") ||
			strings.HasPrefix(trimmed, "PreUp") ||
			strings.HasPrefix(trimmed, "PostUp") ||
			strings.HasPrefix(trimmed, "PreDown") ||
			strings.HasPrefix(trimmed, "PostDown") ||
			strings.HasPrefix(trimmed, "SaveConfig") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func applyConfig(configDir, content string) error {
	if err := writeWGConfig(configDir, content); err != nil {
		return err
	}
	confPath := filepath.Join(configDir, "wg0.conf")
	cmd := exec.Command("wg-quick", "up", confPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "already exists") {
			return fmt.Errorf("wg-quick up: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	return Syncconf(configDir)
}

func writeWGConfig(configDir, content string) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	path := filepath.Join(configDir, "wg0.conf")
	return os.WriteFile(path, []byte(content), 0600)
}

func readWGConfig(configDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(configDir, "wg0.conf"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func generatePrivateKey() (string, error) {
	cmd := exec.Command("wg", "genkey")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("wg genkey: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
