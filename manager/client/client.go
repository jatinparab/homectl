package client

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

const managerTCPPort = "1339"

// AgentInfo represents a registered agent.
type AgentInfo struct {
	Name         string    `json:"name"`
	IP           string    `json:"ip"`
	LastSeen     time.Time `json:"last_seen"`
	RegisteredAt time.Time `json:"registered_at"`
}

// Client is a TCP client for manager-agent messaging.
type Client struct {
	managerHost string

	mu        sync.RWMutex
	conn      net.Conn
	enc       *json.Encoder
	dec       *json.Decoder
	connected bool

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan common.LinkMessage

	agentName string
	agentIP   string

	handlerMu sync.RWMutex
	handler   func(common.LinkMessage) []common.LinkMessage
}

// NewClient creates a new manager client.
func NewClient(baseURL string) *Client {
	managerHost, _ := parseManagerAddress(baseURL)
	return &Client{
		managerHost: managerHost,
		pending:     make(map[string]chan common.LinkMessage),
	}
}

func parseManagerAddress(baseURL string) (string, string) {
	u, err := url.Parse(baseURL)
	if err == nil && u.Host != "" {
		host := u.Hostname()
		port := u.Port()
		if port == "" {
			port = managerTCPPort
		}
		if host != "" {
			return host, port
		}
	}
	trimmed := strings.TrimPrefix(strings.TrimPrefix(baseURL, "http://"), "https://")
	host, port, found := strings.Cut(trimmed, ":")
	if found {
		if port == "" {
			port = managerTCPPort
		}
		return host, port
	}
	if trimmed != "" {
		return trimmed, managerTCPPort
	}
	return "127.0.0.1", managerTCPPort
}

// SetIdentity sets the local agent identity used in the hello frame.
func (c *Client) SetIdentity(name, ip string) {
	c.agentName = name
	c.agentIP = ip
}

// SetRequestHandler sets the callback used for manager-initiated requests.
func (c *Client) SetRequestHandler(fn func(common.LinkMessage) []common.LinkMessage) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.handler = fn
}

// Connect establishes a TCP session to manager.
func (c *Client) Connect(timeout time.Duration) error {
	addr := net.JoinHostPort(c.managerHost, managerTCPPort)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(bufio.NewReader(conn))

	hello := common.LinkMessage{
		Type:      common.LinkTypeHello,
		Source:    "agent",
		AgentName: c.agentName,
		AgentIP:   c.agentIP,
	}
	if err := enc.Encode(hello); err != nil {
		_ = conn.Close()
		return err
	}

	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = conn
	c.enc = enc
	c.dec = dec
	c.connected = true
	c.mu.Unlock()

	go c.readLoop(conn, dec)
	return nil
}

func (c *Client) readLoop(conn net.Conn, dec *json.Decoder) {
	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.connected = false
			_ = c.conn.Close()
			c.conn = nil
			c.enc = nil
			c.dec = nil
		}
		c.mu.Unlock()
	}()

	for {
		var msg common.LinkMessage
		if err := dec.Decode(&msg); err != nil {
			return
		}

		switch msg.Type {
		case common.LinkTypeResponse:
			c.pendingMu.Lock()
			ch := c.pending[msg.ID]
			if msg.Done {
				delete(c.pending, msg.ID)
			}
			c.pendingMu.Unlock()
			if ch != nil {
				ch <- msg
			}
		case common.LinkTypeRequest:
			c.handlerMu.RLock()
			h := c.handler
			c.handlerMu.RUnlock()
			if h == nil {
				_ = c.send(common.LinkMessage{
					Type:   common.LinkTypeResponse,
					ID:     msg.ID,
					Status: 404,
					Error:  "no handler",
					Done:   true,
				})
				continue
			}
			for _, resp := range h(msg) {
				resp.Type = common.LinkTypeResponse
				resp.ID = msg.ID
				_ = c.send(resp)
			}
		}
	}
}

func (c *Client) send(msg common.LinkMessage) error {
	c.mu.RLock()
	enc := c.enc
	connected := c.connected
	c.mu.RUnlock()
	if !connected || enc == nil {
		return fmt.Errorf("not connected")
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return enc.Encode(msg)
}

func (c *Client) request(requestType common.LinkRequestType, payload any, timeout time.Duration) (common.LinkMessage, error) {
	if !c.IsConnected() {
		return common.LinkMessage{}, fmt.Errorf("not connected")
	}
	id := fmt.Sprintf("%d-%06d", time.Now().UnixNano(), rand.Intn(1000000))

	raw, err := common.EncodePayload(payload)
	if err != nil {
		return common.LinkMessage{}, err
	}

	ch := make(chan common.LinkMessage, 32)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	if err := c.send(common.LinkMessage{
		Type:        common.LinkTypeRequest,
		ID:          id,
		Source:      "agent",
		RequestType: requestType,
		Payload:     raw,
	}); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return common.LinkMessage{}, err
	}

	select {
	case resp := <-ch:
		if resp.Error != "" {
			return resp, fmt.Errorf("%s", resp.Error)
		}
		return resp, nil
	case <-time.After(timeout):
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return common.LinkMessage{}, fmt.Errorf("request timeout")
	}
}

// Health checks if the manager link is connected.
func (c *Client) Health() error {
	if c.IsConnected() {
		return nil
	}
	return fmt.Errorf("manager link unavailable")
}

// IsConnected returns whether the TCP link is active.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected && c.conn != nil
}

// Heartbeat sends a heartbeat to the manager with app reports.
func (c *Client) Heartbeat(name, ip string, reports []common.AppReport, metrics common.NodeMetrics) error {
	_, err := c.request(common.LinkReqAgentHeartbeat, common.AgentHeartbeatPayload{Name: name, IP: ip, Reports: reports, Metrics: metrics}, 10*time.Second)
	return err
}

// ListAgents is not used by the agent runtime.
func (c *Client) ListAgents() ([]AgentInfo, error) {
	return nil, fmt.Errorf("not supported over tcp")
}

// TriggerSync triggers a git sync on the manager.
func (c *Client) TriggerSync() error {
	_, err := c.request(common.LinkReqAgentManagerSync, nil, 10*time.Second)
	return err
}

// FetchDesiredApps fetches desired apps for this agent.
func (c *Client) FetchDesiredApps(agentID string) (common.DesiredAppsResponse, error) {
	_ = agentID
	resp, err := c.request(common.LinkReqAgentDesired, nil, 10*time.Second)
	if err != nil {
		return common.DesiredAppsResponse{}, err
	}
	if resp.Status != 200 {
		return common.DesiredAppsResponse{}, fmt.Errorf("status %d", resp.Status)
	}
	var out common.DesiredAppsResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return common.DesiredAppsResponse{}, err
	}
	return out, nil
}

// FetchTraefikConfig fetches traefik config for this agent.
func (c *Client) FetchTraefikConfig(agentID string) (string, error) {
	_ = agentID
	resp, err := c.request(common.LinkReqAgentTraefik, nil, 10*time.Second)
	if err != nil {
		return "", err
	}
	if resp.Status != 200 {
		return "", fmt.Errorf("status %d", resp.Status)
	}
	return string(resp.Body), nil
}

// SyncDockerConfigs fetches docker configs from manager and extracts to local directory.
func (c *Client) SyncDockerConfigs(destDir string) error {
	resp, err := c.request(common.LinkReqAgentDockerSync, nil, 60*time.Second)
	if err != nil {
		return err
	}
	if resp.Status != 200 {
		return fmt.Errorf("docker sync failed: status %d", resp.Status)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	gr, err := gzip.NewReader(bytes.NewReader(resp.Body))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		targetPath := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)) {
			return fmt.Errorf("invalid file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
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
