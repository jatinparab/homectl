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
	"net/url"
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

// wsClientMsg is a message sent from the browser to the server.
type wsClientMsg struct {
	Type   string            `json:"type"`
	ID     string            `json:"id"`
	Stream string            `json:"stream,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// wsStreamMsg is a stream data message sent from server to browser.
type wsStreamMsg struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Line   string `json:"line,omitempty"`
	Source string `json:"source,omitempty"` // set for system-logs subscriptions
	Done   bool   `json:"done,omitempty"`
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(_ *http.Request) bool {
		return true
	},
}

// handleAPIWebSocketState streams dashboard state updates and multiplexes log streams.
// GET /api/v1/ws/state
//
// Inbound messages:
//
//	{ type: "subscribe",   id: "s1", stream: "phase-logs", params: { appId: "x", node: "y" } }
//	{ type: "unsubscribe", id: "s1" }
//
// Outbound messages:
//
//	{ type: "state",  payload: { ... } }          — periodic state snapshot
//	{ type: "stream", id: "s1", line: "...",  }   — stream line
//	{ type: "stream", id: "s1", done: true    }   — stream ended
//	{ type: "stream", id: "s1", line: "...", source: "agent-name" }  — for system-logs
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

	// Single writer goroutine — all writes go through writeCh.
	writeCh := make(chan any, 256)
	var writeWg sync.WaitGroup
	writeWg.Add(1)
	go func() {
		defer writeWg.Done()
		for msg := range writeCh {
			if err := conn.WriteJSON(msg); err != nil {
				cancel()
				return
			}
		}
	}()

	safeSend := func(msg any) bool {
		select {
		case writeCh <- msg:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// Subscription registry.
	type subEntry struct{ cancel context.CancelFunc }
	var subsMu sync.Mutex
	subs := make(map[string]subEntry)
	var subWg sync.WaitGroup

	addSub := func(id string, cancel context.CancelFunc) {
		subsMu.Lock()
		if e, ok := subs[id]; ok {
			e.cancel() // replace existing with same ID
		}
		subs[id] = subEntry{cancel}
		subsMu.Unlock()
	}
	cancelSub := func(id string) {
		subsMu.Lock()
		if e, ok := subs[id]; ok {
			e.cancel()
			delete(subs, id)
		}
		subsMu.Unlock()
	}
	cancelAllSubs := func() {
		subsMu.Lock()
		for _, e := range subs {
			e.cancel()
		}
		subs = make(map[string]subEntry)
		subsMu.Unlock()
	}

	// Read loop: detect disconnect and handle subscribe/unsubscribe.
	go func() {
		defer cancel()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg wsClientMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "subscribe":
				if msg.ID == "" || msg.Stream == "" {
					continue
				}
				params := msg.Params
				if params == nil {
					params = make(map[string]string)
				}
				subCtx, subCancel := context.WithCancel(ctx)
				addSub(msg.ID, subCancel)
				subWg.Add(1)
				go m.handleSubscription(subCtx, msg.ID, msg.Stream, params, safeSend, &subWg)
			case "unsubscribe":
				if msg.ID != "" {
					cancelSub(msg.ID)
				}
			}
		}
	}()

	// State ticker.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastHash := ""
	for {
		select {
		case <-ctx.Done():
			cancelAllSubs()
			subWg.Wait()
			close(writeCh)
			writeWg.Wait()
			return
		case <-ticker.C:
			msg, hash, err := m.buildLiveStateMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					cancelAllSubs()
					subWg.Wait()
					close(writeCh)
					writeWg.Wait()
					return
				}
				apiLog.Warn("Failed to build websocket state payload", "err", err)
				continue
			}
			if hash != lastHash {
				if safeSend(msg) {
					lastHash = hash
				}
			}
		}
	}
}

// handleSubscription dispatches a subscription to the correct stream handler.
func (m *Manager) handleSubscription(ctx context.Context, id, stream string, params map[string]string, send func(any) bool, wg *sync.WaitGroup) {
	defer wg.Done()

	sendStream := func(msg wsStreamMsg) bool {
		return send(msg)
	}

	switch stream {
	case "system-logs":
		m.streamSystemLogs(ctx, id, sendStream)
	default:
		path := buildStreamPath(stream, params)
		if path == "" {
			sendStream(wsStreamMsg{Type: "stream", ID: id, Line: "unknown stream: " + stream, Done: true})
			return
		}
		m.proxySSEStream(ctx, id, path, sendStream)
	}
}

// buildStreamPath converts a stream type + params to an internal SSE path.
func buildStreamPath(stream string, params map[string]string) string {
	switch stream {
	case "app-logs":
		appID, node := params["appId"], params["node"]
		if appID == "" || node == "" {
			return ""
		}
		return fmt.Sprintf("/api/v1/apps/%s/logs?node=%s",
			url.PathEscape(appID), url.QueryEscape(node))
	case "phase-logs":
		appID, node := params["appId"], params["node"]
		if appID == "" || node == "" {
			return ""
		}
		return fmt.Sprintf("/api/v1/apps/%s/phase-logs?node=%s",
			url.PathEscape(appID), url.QueryEscape(node))
	case "container-logs":
		containerID, node := params["containerId"], params["node"]
		if containerID == "" || node == "" {
			return ""
		}
		all := params["all"]
		return fmt.Sprintf("/api/v1/containers/%s/logs?node=%s&all=%s",
			url.PathEscape(containerID), url.QueryEscape(node), url.QueryEscape(all))
	case "system-log-source":
		source := params["source"]
		if source == "" {
			return ""
		}
		return fmt.Sprintf("/api/v1/system-logs/%s", url.PathEscape(source))
	case "repo-check-logs":
		return "/api/v1/repo/check-logs"
	case "repo-unlock-logs":
		return "/api/v1/repo/unlock-logs"
	}
	return ""
}

// proxySSEStream fetches an internal SSE endpoint and forwards lines as wsStreamMsgs.
func (m *Manager) proxySSEStream(ctx context.Context, id, sseRelPath string, send func(wsStreamMsg) bool) {
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d%s", m.port, sseRelPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	if err != nil {
		send(wsStreamMsg{Type: "stream", ID: id, Line: "stream error: " + err.Error(), Done: true})
		return
	}
	resp, err := m.streamHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			send(wsStreamMsg{Type: "stream", ID: id, Line: "stream error: " + err.Error(), Done: true})
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		send(wsStreamMsg{Type: "stream", ID: id, Line: "stream unavailable: " + resp.Status, Done: true})
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	pending := make([]string, 0, 4)

	flush := func() bool {
		if len(pending) == 0 {
			return true
		}
		joined := strings.Join(pending, "\n")
		pending = pending[:0]
		return send(wsStreamMsg{Type: "stream", ID: id, Line: joined})
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !flush() {
				return
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			pending = append(pending, data)
		}
	}
	flush()
	send(wsStreamMsg{Type: "stream", ID: id, Done: true})
}

// streamSystemLogs sends a snapshot of manager + agent logs then live-follows the manager log.
func (m *Manager) streamSystemLogs(ctx context.Context, id string, send func(wsStreamMsg) bool) {
	// Manager snapshot
	for _, line := range logger.Buffer.Snapshot() {
		if !send(wsStreamMsg{Type: "stream", ID: id, Line: line, Source: "manager"}) {
			return
		}
	}

	// Agent snapshots (parallel fetch)
	agents := m.leaseManager.ListAgents()
	now := time.Now()
	type agentLine struct{ source, line string }
	var mu sync.Mutex
	var collected []agentLine
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
					mu.Lock()
					collected = append(collected, agentLine{agentName, line})
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if ctx.Err() != nil {
		return
	}
	for _, al := range collected {
		if !send(wsStreamMsg{Type: "stream", ID: id, Line: al.line, Source: al.source}) {
			return
		}
	}

	// Live-follow manager log (never sends Done — runs until unsubscribe/disconnect)
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
			if !send(wsStreamMsg{Type: "stream", ID: id, Line: line, Source: "manager"}) {
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
