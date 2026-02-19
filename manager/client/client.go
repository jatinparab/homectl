package client

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

// Client is an HTTP client for the manager API
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new manager client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Health checks if the manager is healthy
func (c *Client) Health() error {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("manager unhealthy: status %d", resp.StatusCode)
	}
	return nil
}

// ReadFile reads a file from the manager
func (c *Client) ReadFile(path string) ([]byte, error) {
	url := fmt.Sprintf("%s/files/%s", c.baseURL, strings.TrimPrefix(path, "/"))
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch file: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// ListDir lists files in a directory from the manager
func (c *Client) ListDir(path string) ([]string, error) {
	url := fmt.Sprintf("%s/files/%s", c.baseURL, strings.TrimPrefix(path, "/"))
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("directory not found: %s", path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list directory: status %d", resp.StatusCode)
	}

	var entries []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			entries = append(entries, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read directory listing: %w", err)
	}

	return entries, nil
}

// TriggerSync triggers a git sync on the manager
func (c *Client) TriggerSync() error {
	resp, err := c.httpClient.Post(c.baseURL+"/sync", "application/json", nil)
	if err != nil {
		return fmt.Errorf("sync request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sync failed: %s", string(body))
	}
	return nil
}

// HeartbeatRequest is the request body for agent heartbeat
type HeartbeatRequest struct {
	Name    string             `json:"name"`
	IP      string             `json:"ip"`
	Reports []common.AppReport `json:"reports,omitempty"`
}

// AgentInfo represents a registered agent
type AgentInfo struct {
	Name         string    `json:"name"`
	IP           string    `json:"ip"`
	LastSeen     time.Time `json:"last_seen"`
	RegisteredAt time.Time `json:"registered_at"`
}

// Heartbeat sends a heartbeat to the manager with app reports
func (c *Client) Heartbeat(name, ip string, reports []common.AppReport) error {
	req := HeartbeatRequest{Name: name, IP: ip, Reports: reports}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/agents/heartbeat", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("heartbeat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heartbeat failed: %s", string(respBody))
	}
	return nil
}

// ListAgents returns all registered agents
func (c *Client) ListAgents() ([]AgentInfo, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/agents")
	if err != nil {
		return nil, fmt.Errorf("list agents request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list agents failed: %s", string(body))
	}

	var agents []AgentInfo
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return agents, nil
}

// FetchDesiredApps fetches the desired apps for an agent from the manager
func (c *Client) FetchDesiredApps(agentID string) (common.DesiredAppsResponse, error) {
	url := fmt.Sprintf("%s/agents/%s/desired", c.baseURL, agentID)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return common.DesiredAppsResponse{}, fmt.Errorf("fetch desired apps request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return common.DesiredAppsResponse{}, fmt.Errorf("fetch desired apps failed: %s", string(body))
	}

	var response common.DesiredAppsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return common.DesiredAppsResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return response, nil
}

// FetchTraefikConfig fetches the traefik config for an agent from the manager
func (c *Client) FetchTraefikConfig(agentID string) (string, error) {
	url := fmt.Sprintf("%s/agents/%s/traefik-config", c.baseURL, agentID)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch traefik config request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("fetch traefik config failed: %s", string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(data), nil
}

// SyncDockerConfigs fetches docker configs from manager and extracts to local directory
func (c *Client) SyncDockerConfigs(destDir string) error {
	// Fetch the tar.gz archive
	resp, err := c.httpClient.Get(c.baseURL + "/docker/sync")
	if err != nil {
		return fmt.Errorf("docker sync request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker sync failed: %s", string(body))
	}

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Decompress gzip
	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	// Extract tar
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Construct target path
		targetPath := filepath.Join(destDir, header.Name)

		// Prevent directory traversal
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)) {
			return fmt.Errorf("invalid file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Create file
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", targetPath, err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to write file %s: %w", targetPath, err)
			}
			outFile.Close()
		}
	}

	return nil
}
