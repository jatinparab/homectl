package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/wireguard"
)

type wireGuardBootstrapResult struct {
	AgentName  string
	AgentIP    string
	ManagerURL string
}

func bootstrapWireGuard(agentName, bootstrapManagerURL string) (wireGuardBootstrapResult, error) {
	joinToken := strings.TrimSpace(os.Getenv("HOMECTL_JOIN_TOKEN"))
	if joinToken == "" {
		return wireGuardBootstrapResult{}, fmt.Errorf("HOMECTL_JOIN_TOKEN is required")
	}
	if bootstrapManagerURL == "" {
		return wireGuardBootstrapResult{}, fmt.Errorf("HOMECTL_MANAGER_URL is required")
	}

	configDir := os.Getenv("HOMECTL_WG_CONFIG_DIR")
	if configDir == "" {
		configDir = "/opt/homectl/wireguard"
	}
	keyDir := filepath.Join(configDir, "keys")
	privateKey, publicKey, err := wireguard.EnsureKeyPair(keyDir)
	if err != nil {
		return wireGuardBootstrapResult{}, err
	}

	reqBody, err := json.Marshal(common.AgentJoinRequest{
		Name:               agentName,
		JoinToken:          joinToken,
		WireGuardPublicKey: publicKey,
	})
	if err != nil {
		return wireGuardBootstrapResult{}, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(strings.TrimRight(bootstrapManagerURL, "/")+"/api/join", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return wireGuardBootstrapResult{}, fmt.Errorf("join manager: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return wireGuardBootstrapResult{}, fmt.Errorf("join manager: status %d", resp.StatusCode)
	}
	var join common.AgentJoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&join); err != nil {
		return wireGuardBootstrapResult{}, err
	}

	if err := wireguard.EnsureAgent(wireguard.AgentConfig{
		ConfigDir:        configDir,
		Address:          join.WireGuardAddress,
		PrivateKey:       privateKey,
		ManagerPublicKey: join.ManagerPublicKey,
		EndpointIP:       join.ManagerEndpointIP,
		EndpointPort:     join.ManagerEndpointPort,
	}); err != nil {
		return wireGuardBootstrapResult{}, err
	}

	return wireGuardBootstrapResult{
		AgentName:  join.Name,
		AgentIP:    join.WireGuardAddress,
		ManagerURL: join.ManagerControlBaseURL,
	}, nil
}
