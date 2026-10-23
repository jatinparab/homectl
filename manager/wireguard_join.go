package manager

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/config"
	"github.com/jatinparab/homectl/wireguard"
)

const wireGuardPeersFile = "wireguard-peers.json"

type wireGuardPeerState struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	Address   string `json:"address"`
}

func (m *Manager) wireGuardDir() string {
	if m.stateDir != config.StateDir {
		return filepath.Join(m.stateDir, "wg")
	}
	return config.WireGuardDir
}

func (m *Manager) wireGuardPeersPath() string {
	return filepath.Join(m.wireGuardDir(), wireGuardPeersFile)
}

func joinToken() string {
	return strings.TrimSpace(os.Getenv("HOMECTL_JOIN_TOKEN"))
}

func managerEndpointIP() string {
	return strings.TrimSpace(os.Getenv("HOMECTL_WG_ENDPOINT_IP"))
}

func requireJoinToken() (string, error) {
	token := joinToken()
	if token == "" {
		return "", fmt.Errorf("HOMECTL_JOIN_TOKEN is required")
	}
	return token, nil
}

func RequireJoinTokenForStartup() (string, error) {
	token, err := requireJoinToken()
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(managerEndpointIP()); ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("HOMECTL_WG_ENDPOINT_IP must be a public IPv4 address")
	}
	if _, err := domainSuffix(); err != nil {
		return "", err
	}
	if _, err := homectlPublicHost(); err != nil {
		return "", err
	}
	return token, nil
}

func validJoinToken(got string) bool {
	want := joinToken()
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (m *Manager) handleAgentJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req common.AgentJoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid join request", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.WireGuardPublicKey = strings.TrimSpace(req.WireGuardPublicKey)
	if req.Name == "" || req.WireGuardPublicKey == "" || !validJoinToken(req.JoinToken) {
		http.Error(w, "invalid join credentials", http.StatusUnauthorized)
		return
	}

	resp, err := m.JoinAgent(req.Name, req.WireGuardPublicKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (m *Manager) JoinAgent(name, publicKey string) (common.AgentJoinResponse, error) {
	if err := validateWireGuardPeer(name, publicKey); err != nil {
		return common.AgentJoinResponse{}, err
	}
	m.wireGuardMu.Lock()
	defer m.wireGuardMu.Unlock()
	privateKey, managerPublicKey, err := wireguard.EnsureKeyPair(m.wireGuardDir())
	if err != nil {
		return common.AgentJoinResponse{}, err
	}
	endpointIP := managerEndpointIP()
	if ip := net.ParseIP(endpointIP); ip == nil || ip.To4() == nil {
		return common.AgentJoinResponse{}, fmt.Errorf("HOMECTL_WG_ENDPOINT_IP must be a public IPv4 address")
	}

	peers, err := m.loadWireGuardPeers()
	if err != nil {
		return common.AgentJoinResponse{}, err
	}
	peers, peer, err := upsertWireGuardPeer(peers, name, publicKey)
	if err != nil {
		return common.AgentJoinResponse{}, err
	}
	if err := m.saveWireGuardPeers(peers); err != nil {
		return common.AgentJoinResponse{}, err
	}
	if err := m.reconcileWireGuard(privateKey, peers); err != nil {
		return common.AgentJoinResponse{}, err
	}

	return common.AgentJoinResponse{
		Name:                  name,
		WireGuardAddress:      peer.Address,
		ManagerWireGuardIP:    wireguard.ManagerAddress,
		ManagerPublicKey:      managerPublicKey,
		ManagerEndpointIP:     endpointIP,
		ManagerEndpointPort:   wireguard.DefaultPort,
		ManagerControlBaseURL: "http://" + wireguard.ManagerAddress + ":1338",
	}, nil
}

func (m *Manager) EnsureWireGuard() error {
	m.wireGuardMu.Lock()
	defer m.wireGuardMu.Unlock()
	privateKey, _, err := wireguard.EnsureKeyPair(m.wireGuardDir())
	if err != nil {
		return err
	}
	peers, err := m.loadWireGuardPeers()
	if err != nil {
		return err
	}
	peers, repaired, err := normalizeWireGuardPeers(peers)
	if err != nil {
		return err
	}
	if repaired {
		if err := m.saveWireGuardPeers(peers); err != nil {
			return err
		}
		managerLog.Warn("Repaired conflicting or invalid WireGuard peer addresses")
	}
	return m.reconcileWireGuard(privateKey, peers)
}

func (m *Manager) reconcileWireGuard(privateKey string, peers []wireGuardPeerState) error {
	wgPeers := make([]wireguard.Peer, 0, len(peers))
	for _, peer := range peers {
		wgPeers = append(wgPeers, wireguard.Peer{
			Name:      peer.Name,
			PublicKey: peer.PublicKey,
			Address:   peer.Address,
		})
	}
	return wireguard.EnsureManager(wireguard.ManagerConfig{
		ConfigDir:  m.wireGuardDir(),
		EndpointIP: managerEndpointIP(),
		ListenPort: wireguard.DefaultPort,
		PrivateKey: privateKey,
		Peers:      wgPeers,
	})
}

func (m *Manager) loadWireGuardPeers() ([]wireGuardPeerState, error) {
	data, err := os.ReadFile(m.wireGuardPeersPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []wireGuardPeerState{}, nil
		}
		return nil, err
	}
	var peers []wireGuardPeerState
	if err := json.Unmarshal(data, &peers); err != nil {
		return nil, err
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	return peers, nil
}

func (m *Manager) saveWireGuardPeers(peers []wireGuardPeerState) error {
	if err := os.MkdirAll(m.wireGuardDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		return err
	}
	// Readers such as public-route generation do not take wireGuardMu. Rename
	// a complete, private file rather than exposing truncated/partial JSON.
	file, err := os.CreateTemp(m.wireGuardDir(), ".wireguard-peers-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), m.wireGuardPeersPath())
}

var wireGuardNetwork = netip.MustParsePrefix(wireguard.DefaultNetwork)

func validWireGuardPeerAddress(address string) bool {
	ip, err := netip.ParseAddr(address)
	return err == nil && ip.Is4() && wireGuardNetwork.Contains(ip) &&
		ip != wireGuardNetwork.Addr() && address != wireguard.ManagerAddress && ip.As4()[3] != 255
}

func nextWireGuardPeerAddress(used map[string]bool) (string, error) {
	// The /24's network, manager (.1), and broadcast addresses are reserved.
	for ip := wireGuardNetwork.Addr().Next().Next(); wireGuardNetwork.Contains(ip) && ip.As4()[3] != 255; ip = ip.Next() {
		if !used[ip.String()] {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("WireGuard address pool exhausted")
}

// Preserve all unique valid reservations before filling any gaps. This avoids
// moving a healthy peer just because an earlier invalid/duplicate entry needs
// repair. Alphabetical ordering makes legacy collision repair deterministic.
func normalizeWireGuardPeers(peers []wireGuardPeerState) ([]wireGuardPeerState, bool, error) {
	peers = append([]wireGuardPeerState(nil), peers...)
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	used := make(map[string]bool)
	names := make(map[string]bool)
	var repair []int
	for i, peer := range peers {
		if peer.Name == "" || names[peer.Name] {
			return nil, false, fmt.Errorf("empty or duplicate WireGuard peer name")
		}
		names[peer.Name] = true
		if !validWireGuardPeerAddress(peer.Address) || used[peer.Address] {
			repair = append(repair, i)
			continue
		}
		used[peer.Address] = true
	}
	for _, i := range repair {
		address, err := nextWireGuardPeerAddress(used)
		if err != nil {
			return nil, false, err
		}
		peers[i].Address = address
		used[address] = true
	}
	return peers, len(repair) != 0, nil
}

func validateWireGuardPeer(name, publicKey string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("WireGuard peer name is required")
	}
	key, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(key) != 32 || len(publicKey) != 44 {
		return fmt.Errorf("invalid WireGuard public key")
	}
	return nil
}

func upsertWireGuardPeer(peers []wireGuardPeerState, name, publicKey string) ([]wireGuardPeerState, wireGuardPeerState, error) {
	if err := validateWireGuardPeer(name, publicKey); err != nil {
		return nil, wireGuardPeerState{}, err
	}
	for _, peer := range peers {
		if peer.Name != name && peer.PublicKey == publicKey {
			return nil, wireGuardPeerState{}, fmt.Errorf("WireGuard public key already belongs to another agent")
		}
	}
	peers, _, err := normalizeWireGuardPeers(peers)
	if err != nil {
		return nil, wireGuardPeerState{}, err
	}
	used := make(map[string]bool, len(peers))
	for i := range peers {
		if peers[i].Name == name {
			peers[i].PublicKey = publicKey
			return peers, peers[i], nil
		}
		used[peers[i].Address] = true
	}
	address, err := nextWireGuardPeerAddress(used)
	if err != nil {
		return nil, wireGuardPeerState{}, err
	}
	peer := wireGuardPeerState{Name: name, PublicKey: publicKey, Address: address}
	return append(peers, peer), peer, nil
}

func (m *Manager) CleanupStaleWireGuardPeers() error {
	m.wireGuardMu.Lock()
	defer m.wireGuardMu.Unlock()
	peers, err := m.loadWireGuardPeers()
	if err != nil {
		return err
	}
	alive := make(map[string]bool)
	for _, agent := range m.leaseManager.ListAgents() {
		alive[agent.Name] = true
	}
	changed := false
	filtered := make([]wireGuardPeerState, 0, len(peers))
	for _, peer := range peers {
		if alive[peer.Name] {
			filtered = append(filtered, peer)
		} else {
			managerLog.Info("Removing stale WireGuard peer", "peer", peer.Name)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := m.saveWireGuardPeers(filtered); err != nil {
		return err
	}
	privateKey, _, err := wireguard.EnsureKeyPair(m.wireGuardDir())
	if err != nil {
		return err
	}
	return m.reconcileWireGuard(privateKey, filtered)
}
