package manager

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/logger"
)

type liveStatePayload struct {
	Apps            []AppStatus         `json:"apps"`
	Agents          []APIAgentInfo      `json:"agents"`
	SystemLogSource []string            `json:"system_log_sources"`
	Containers      []NodeContainers    `json:"containers"`
	Storage         APIStorageResponse  `json:"storage"`
	UpcomingBackup  *UpcomingBackupInfo `json:"upcoming_backup"`
	UpcomingPrune   *UpcomingPruneInfo  `json:"upcoming_prune"`
	RepoStatus      RepoStatusResponse  `json:"repo_status"`
	Gateway         GatewayInfo         `json:"gateway"`
}

type liveStateMessage struct {
	Type    string           `json:"type"`
	Payload liveStatePayload `json:"payload"`
}

type streamLineMessage struct {
	Type string `json:"type"`
	Line string `json:"line"`
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(_ *http.Request) bool {
		return true
	},
}

// handleAPIWebSocketState streams dashboard state updates for the web UI.
// GET /api/v1/ws/state
func (m *Manager) handleAPIWebSocketState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastHash := ""
	for {
		msg, hash, err := m.buildLiveStateMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			apiLog.Warn("Failed to build websocket state payload", "err", err)
		} else if hash != lastHash {
			if err := conn.WriteJSON(msg); err != nil {
				return
			}
			lastHash = hash
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// handleAPIWebSocketStream proxies existing SSE streams over websocket.
// GET /api/v1/ws/stream?path=/api/v1/...
func (m *Manager) handleAPIWebSocketStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	streamPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if !isAllowedWSStreamPath(streamPath) {
		http.Error(w, "invalid stream path", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d%s", m.port, streamPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	if err != nil {
		return
	}

	resp, err := m.streamHTTPClient.Do(req)
	if err != nil {
		_ = conn.WriteJSON(streamLineMessage{Type: "line", Line: "Error connecting to stream: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = conn.WriteJSON(streamLineMessage{Type: "line", Line: "Stream unavailable: " + resp.Status})
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	dataLines := make([]string, 0, 4)

	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		joined := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		return conn.WriteJSON(streamLineMessage{Type: "line", Line: joined})
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			dataLines = append(dataLines, data)
		}
	}

	if err := flush(); err != nil {
		return
	}
}

type logLineMessage struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Line   string `json:"line"`
}

// handleAPIWebSocketLogs streams log lines from all sources over a single WebSocket.
// GET /api/v1/ws/logs
//
// On connect: sends snapshot from manager + current online agents.
// Stays open to stream live manager log lines.
func (m *Manager) handleAPIWebSocketLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Read loop to detect client disconnect
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	var writeMu sync.Mutex
	send := func(source, line string) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(logLineMessage{Type: "log", Source: source, Line: line}) == nil
	}

	// Send manager snapshot first
	for _, line := range logger.Buffer.Snapshot() {
		if !send("manager", line) {
			return
		}
	}

	// Fetch snapshots from all online agents in parallel
	agents := m.leaseManager.ListAgents()
	now := time.Now()
	var wg sync.WaitGroup
	for _, a := range agents {
		if now.Sub(a.LastSeen) >= 30*time.Second {
			continue
		}
		agentName := a.Name
		wg.Add(1)
		go func() {
			defer wg.Done()
			fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			status, body, err := m.requestAgentUnary(fetchCtx, agentName, common.LinkReqSystemLogs, nil)
			if err != nil || status != 200 || len(body) == 0 {
				return
			}
			for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
				if line != "" {
					send(agentName, line)
				}
			}
		}()
	}
	wg.Wait()

	// Live-stream manager logs
	ch := logger.Buffer.Follow()
	defer logger.Buffer.Unfollow(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			if !send("manager", line) {
				return
			}
		}
	}
}

func (m *Manager) buildLiveStateMessage(ctx context.Context) (liveStateMessage, string, error) {
	var payload liveStatePayload

	if err := m.invokeJSONEndpoint(ctx, "/api/v1/apps", m.handleAPIApps, &payload.Apps); err != nil {
		return liveStateMessage{}, "", err
	}
	if err := m.invokeJSONEndpoint(ctx, "/api/v1/agents", m.handleAPIAgents, &payload.Agents); err != nil {
		return liveStateMessage{}, "", err
	}
	if err := m.invokeJSONEndpoint(ctx, "/api/v1/system-logs", m.handleAPISystemLogSources, &payload.SystemLogSource); err != nil {
		return liveStateMessage{}, "", err
	}
	if err := m.invokeJSONEndpoint(ctx, "/api/v1/containers", m.handleAPIContainers, &payload.Containers); err != nil {
		return liveStateMessage{}, "", err
	}
	if err := m.invokeJSONEndpoint(ctx, "/api/v1/storage", m.handleAPIStorage, &payload.Storage); err != nil {
		return liveStateMessage{}, "", err
	}
	if err := m.invokeJSONEndpoint(ctx, "/api/v1/backup/upcoming", m.handleAPIUpcomingBackup, &payload.UpcomingBackup); err != nil {
		return liveStateMessage{}, "", err
	}
	if err := m.invokeJSONEndpoint(ctx, "/api/v1/prune/upcoming", m.handleAPIUpcomingPrune, &payload.UpcomingPrune); err != nil {
		return liveStateMessage{}, "", err
	}
	if err := m.invokeJSONEndpoint(ctx, "/api/v1/repo/status", m.handleAPIRepoStatus, &payload.RepoStatus); err != nil {
		return liveStateMessage{}, "", err
	}
	if err := m.invokeJSONEndpoint(ctx, "/api/v1/gateway", m.handleAPIGateway, &payload.Gateway); err != nil {
		return liveStateMessage{}, "", err
	}

	msg := liveStateMessage{Type: "state", Payload: payload}
	encoded, err := json.Marshal(msg)
	if err != nil {
		return liveStateMessage{}, "", err
	}
	hashBytes := sha256.Sum256(encoded)
	return msg, hex.EncodeToString(hashBytes[:]), nil
}

func (m *Manager) invokeJSONEndpoint(
	ctx context.Context,
	path string,
	handler func(http.ResponseWriter, *http.Request),
	target any,
) error {
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", path, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func isAllowedWSStreamPath(path string) bool {
	if path == "" || !strings.HasPrefix(path, "/api/v1/") {
		return false
	}
	if strings.HasPrefix(path, "/api/v1/ws/") {
		return false
	}

	if strings.HasPrefix(path, "/api/v1/system-logs/") {
		return true
	}
	if path == "/api/v1/repo/check-logs" || path == "/api/v1/repo/unlock-logs" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/apps/") && (strings.Contains(path, "/logs?") || strings.Contains(path, "/phase-logs?")) {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/containers/") && strings.Contains(path, "/logs?") {
		return true
	}
	return false
}
