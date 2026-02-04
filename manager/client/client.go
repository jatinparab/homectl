package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// AgentInfo represents a registered agent
type AgentInfo struct {
	Name         string    `json:"name"`
	IP           string    `json:"ip"`
	LastSeen     time.Time `json:"last_seen"`
	RegisteredAt time.Time `json:"registered_at"`
}

// Heartbeat sends a heartbeat to the manager
func (c *Client) Heartbeat(name, ip string) error {
	req := HeartbeatRequest{Name: name, IP: ip}
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
