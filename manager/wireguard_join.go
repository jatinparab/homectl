package manager

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
	if len(publicKey) != 44 {
		return common.AgentJoinResponse{}, fmt.Errorf("invalid wireguard public key length")
	}
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
	peers, peer := upsertWireGuardPeer(peers, name, publicKey)
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
	privateKey, _, err := wireguard.EnsureKeyPair(m.wireGuardDir())
	if err != nil {
		return err
	}
	peers, err := m.loadWireGuardPeers()
	if err != nil {
		return err
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
	return os.WriteFile(m.wireGuardPeersPath(), data, 0600)
}

func upsertWireGuardPeer(peers []wireGuardPeerState, name, publicKey string) ([]wireGuardPeerState, wireGuardPeerState) {
	for i := range peers {
		if peers[i].Name == name {
			peers[i].PublicKey = publicKey
			return peers, peers[i]
		}
	}
	if len(publicKey) != 44 {
		return peers, wireGuardPeerState{}
	}
	peer := wireGuardPeerState{
		Name:      name,
		PublicKey: publicKey,
		Address:   fmt.Sprintf("10.44.0.%d", len(peers)+2),
	}
	peers = append(peers, peer)
	return peers, peer
}

func (m *Manager) CleanupStaleWireGuardPeers() error {
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
