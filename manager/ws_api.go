package manager

import (
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
	PublicRoutes    []PublicRouteStatus `json:"public_routes"`
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

func wsCheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser clients (e.g. curl, scripts) send no Origin
		return true
	}
	// Allow same-origin requests
	publicHost, err := homectlPublicHost()
	if err == nil {
		allowedOrigin := "https://" + publicHost
		if origin == allowedOrigin {
			return true
		}
		// Also allow http for local development
		allowedOrigin = "http://" + publicHost
		if origin == allowedOrigin {
			return true
		}
		// Allow manager localhost aliases
		if strings.HasPrefix(origin, "http://"+homectlLocalhostAlias) ||
			strings.HasPrefix(origin, "http://"+managerLocalhostAlias) {
			return true
		}
	}
	// Same-host fallback: if origin matches the request's Host header
	if r.Host != "" && origin == "https://"+r.Host {
		return true
	}
	return false
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     wsCheckOrigin,
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

	// Single writer goroutine.
	// stateCh is conflated (latest snapshot wins), streamCh preserves stream order.
	stateCh := make(chan any, 1)
	streamCh := make(chan any, 256)
	var writeWg sync.WaitGroup
	writeWg.Add(1)

	safeSendStream := func(msg any) bool {
		select {
		case streamCh <- msg:
			return true
		case <-ctx.Done():
			return false
		}
	}
	safeSendState := func(msg any) bool {
		select {
		case stateCh <- msg:
			return true
		default:
			// Replace queued state snapshot with the latest.
			select {
			case <-stateCh:
			default:
			}
			select {
			case stateCh <- msg:
				return true
			case <-ctx.Done():
				return false
			}
		case <-ctx.Done():
			return false
		}
	}

	go func() {
		defer writeWg.Done()
		for {
			select {
			case msg, ok := <-streamCh:
				if !ok {
					streamCh = nil
					continue
				}
				if err := conn.WriteJSON(msg); err != nil {
					cancel()
					return
				}
			case msg, ok := <-stateCh:
				if !ok {
					stateCh = nil
					continue
				}
				if err := conn.WriteJSON(msg); err != nil {
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
			if streamCh == nil && stateCh == nil {
				return
			}
		}
	}()

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
				go m.handleSubscription(subCtx, msg.ID, msg.Stream, params, safeSendStream, &subWg)
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
			close(stateCh)
			close(streamCh)
			writeWg.Wait()
			return
		case <-ticker.C:
			msg, hash, err := m.buildLiveStateMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					cancelAllSubs()
					subWg.Wait()
					close(stateCh)
					close(streamCh)
					writeWg.Wait()
					return
				}
				apiLog.Warn("Failed to build websocket state payload", "err", err)
				continue
			}
			if hash != lastHash {
				if safeSendState(msg) {
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
	case "app-logs":
		m.streamAppLogsWS(ctx, id, params, sendStream)
	case "phase-logs":
		m.streamPhaseLogsWS(ctx, id, params, sendStream)
	case "container-logs":
		m.streamContainerLogsWS(ctx, id, params, sendStream)
	case "repo-check-logs":
		m.streamRepoLogsWS(ctx, id, false, sendStream)
	case "repo-unlock-logs":
		m.streamRepoLogsWS(ctx, id, true, sendStream)
	default:
		sendStream(wsStreamMsg{Type: "stream", ID: id, Line: "unknown stream: " + stream, Done: true})
	}
}

func (m *Manager) streamAppLogsWS(ctx context.Context, id string, params map[string]string, send func(wsStreamMsg) bool) {
	appID := params["appId"]
	node := params["node"]
	if appID == "" {
		m.sendStreamError(send, id, "appId parameter is required")
		return
	}
	if err := m.ensureAgentOnline(node); err != nil {
		m.sendStreamError(send, id, err.Error())
		return
	}
	if err := m.streamAgentAppLogsPolling(ctx, node, appID, func(line string) bool {
		return send(wsStreamMsg{Type: "stream", ID: id, Line: line})
	}); err != nil && ctx.Err() == nil {
		m.sendStreamError(send, id, "stream error: "+err.Error())
		return
	}
	if ctx.Err() == nil {
		send(wsStreamMsg{Type: "stream", ID: id, Done: true})
	}
}

func (m *Manager) streamPhaseLogsWS(ctx context.Context, id string, params map[string]string, send func(wsStreamMsg) bool) {
	appID := params["appId"]
	node := params["node"]
	if appID == "" {
		m.sendStreamError(send, id, "appId parameter is required")
		return
	}
	if err := m.ensureAgentOnline(node); err != nil {
		m.sendStreamError(send, id, err.Error())
		return
	}
	payload := common.AppPhaseLogsPayload{AppID: appID}
	m.forwardAgentStream(ctx, id, node, common.LinkReqAppPhaseLogs, payload, send)
}

func (m *Manager) streamContainerLogsWS(ctx context.Context, id string, params map[string]string, send func(wsStreamMsg) bool) {
	containerID := params["containerId"]
	node := params["node"]
	if containerID == "" {
		m.sendStreamError(send, id, "containerId parameter is required")
		return
	}
	if err := m.ensureAgentOnline(node); err != nil {
		m.sendStreamError(send, id, err.Error())
		return
	}
	showAll := strings.EqualFold(params["all"], "true") || params["all"] == "1"
	payload := common.ContainerLogsPayload{ContainerID: containerID, ShowAll: showAll}
	m.forwardAgentStream(ctx, id, node, common.LinkReqContainerLogs, payload, send)
}

func (m *Manager) streamRepoLogsWS(ctx context.Context, id string, unlock bool, send func(wsStreamMsg) bool) {
	gateway := m.getGatewayNode()
	if gateway == "" {
		m.sendStreamError(send, id, "no gateway selected")
		return
	}
	if err := m.ensureAgentOnline(gateway); err != nil {
		m.sendStreamError(send, id, err.Error())
		return
	}
	reqType := common.LinkReqRepoCheckLogs
	if unlock {
		reqType = common.LinkReqRepoUnlockLogs
	}
	m.forwardAgentStream(ctx, id, gateway, reqType, nil, send)
}

func (m *Manager) ensureAgentOnline(node string) error {
	if node == "" {
		return fmt.Errorf("node parameter is required")
	}
	if !m.isAgentKnown(node) {
		return fmt.Errorf("node %s not found", node)
	}
	agent, ok := m.leaseManager.GetAgent(node)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
		return fmt.Errorf("node %s is offline", node)
	}
	return nil
}

func (m *Manager) forwardAgentStream(ctx context.Context, id, agent string, requestType common.LinkRequestType, payload any, send func(wsStreamMsg) bool) {
	stream, status, err := m.streamAgent(agent, requestType, payload)
	if err != nil {
		m.sendStreamError(send, id, "stream error: "+err.Error())
		return
	}
	if status != http.StatusOK {
		_ = stream.Close()
		m.sendStreamError(send, id, fmt.Sprintf("stream unavailable: HTTP %d", status))
		return
	}
	if err := m.pipeLineStream(ctx, stream, func(line string) bool {
		return send(wsStreamMsg{Type: "stream", ID: id, Line: line})
	}); err != nil && ctx.Err() == nil {
		m.sendStreamError(send, id, "stream error: "+err.Error())
		return
	}
	if ctx.Err() != nil {
		return
	}
	send(wsStreamMsg{Type: "stream", ID: id, Done: true})
}

func (m *Manager) sendStreamError(send func(wsStreamMsg) bool, id, msg string) {
	send(wsStreamMsg{Type: "stream", ID: id, Line: msg, Done: true})
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
	if err := m.invokeJSONEndpoint(ctx, "/api/v1/public-routes", m.handleAPIPublicRoutes, &payload.PublicRoutes); err != nil {
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
