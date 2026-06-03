package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

const agentLinkPort = 1339

const (
	agentRequestTimeout     = 10 * time.Second
	agentStreamFirstTimeout = 10 * time.Second
	agentHeavyStreamTimeout = 2 * time.Minute
)

type agentLinkPeer struct {
	name string
	ip   string

	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan common.LinkMessage
}

func (peer *agentLinkPeer) removePending(id string, ch chan common.LinkMessage) {
	peer.pendingMu.Lock()
	defer peer.pendingMu.Unlock()
	current, ok := peer.pending[id]
	if !ok || current != ch {
		return
	}
	delete(peer.pending, id)
	close(ch)
}

func (peer *agentLinkPeer) closeAllPending() {
	peer.pendingMu.Lock()
	defer peer.pendingMu.Unlock()
	for id, ch := range peer.pending {
		delete(peer.pending, id)
		close(ch)
	}
}

func (m *Manager) StartAgentTCPServer() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", agentLinkPort))
	if err != nil {
		return err
	}
	m.tcpListener = ln

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "closed") {
					return
				}
				managerLog.Warn("Agent TCP accept failed", "err", err)
				continue
			}
			go m.attachAgentLink(conn)
		}
	}()

	managerLog.Info("Agent TCP server listening", "port", agentLinkPort)
	return nil
}

func (m *Manager) attachAgentLink(conn net.Conn) {
	peer := &agentLinkPeer{
		conn:    conn,
		enc:     json.NewEncoder(conn),
		dec:     json.NewDecoder(conn),
		pending: make(map[string]chan common.LinkMessage),
	}

	var hello common.LinkMessage
	if err := peer.dec.Decode(&hello); err != nil {
		_ = conn.Close()
		return
	}
	if hello.Type != common.LinkTypeHello || hello.AgentName == "" || !validJoinToken(hello.JoinToken) {
		_ = conn.Close()
		return
	}

	peer.name = hello.AgentName
	peer.ip = hello.AgentIP

	m.linkMu.Lock()
	m.agentLinks[peer.name] = peer
	m.linkMu.Unlock()

	defer func() {
		_ = conn.Close()
		m.linkMu.Lock()
		if current := m.agentLinks[peer.name]; current == peer {
			delete(m.agentLinks, peer.name)
		}
		m.linkMu.Unlock()
		peer.closeAllPending()
	}()

	for {
		var msg common.LinkMessage
		if err := peer.dec.Decode(&msg); err != nil {
			return
		}

		if msg.Type == common.LinkTypeResponse {
			peer.pendingMu.Lock()
			ch, ok := peer.pending[msg.ID]
			if ok {
				select {
				case ch <- msg:
				default:
				}
				if msg.Done {
					delete(peer.pending, msg.ID)
					close(ch)
				}
			}
			peer.pendingMu.Unlock()
			continue
		}

		if msg.Type == common.LinkTypeRequest {
			go m.handleAgentRequest(peer, msg)
		}
	}
}

func (m *Manager) handleAgentRequest(peer *agentLinkPeer, req common.LinkMessage) {
	resp := common.LinkMessage{Type: common.LinkTypeResponse, ID: req.ID, Done: true}

	switch req.RequestType {
	case common.LinkReqAgentHeartbeat:
		hb, err := common.DecodePayload[common.AgentHeartbeatPayload](req)
		if err != nil {
			resp.Status = http.StatusBadRequest
			resp.Error = err.Error()
			_ = peer.send(resp)
			return
		}
		if hb.Name == "" {
			hb.Name = peer.name
		}
		if hb.IP == "" {
			hb.IP = peer.ip
		}
		m.applyHeartbeat(HeartbeatRequest{
			Name:    hb.Name,
			IP:      hb.IP,
			Reports: hb.Reports,
			Metrics: hb.Metrics,
		})
		resp.Status = http.StatusOK
		resp.Body = []byte(`{"status":"ok"}`)
	case common.LinkReqAgentDesired:
		res, err := m.invokeRawEndpoint(http.MethodGet, fmt.Sprintf("/agents/%s/desired", peer.name), nil)
		if err != nil {
			resp.Status = http.StatusInternalServerError
			resp.Error = err.Error()
		} else {
			resp.Status = res.status
			resp.Body = res.body
		}
	case common.LinkReqAgentCaddy, common.LinkReqAgentTraefik:
		res, err := m.invokeRawEndpoint(http.MethodGet, fmt.Sprintf("/agents/%s/caddy-config", peer.name), nil)
		if err != nil {
			resp.Status = http.StatusInternalServerError
			resp.Error = err.Error()
		} else {
			resp.Status = res.status
			resp.Body = res.body
		}
	case common.LinkReqAgentDockerSync:
		res, err := m.invokeRawEndpoint(http.MethodGet, "/docker/sync", nil)
		if err != nil {
			resp.Status = http.StatusInternalServerError
			resp.Error = err.Error()
		} else {
			resp.Status = res.status
			resp.Body = res.body
		}
	case common.LinkReqAgentManagerSync:
		res, err := m.invokeRawEndpoint(http.MethodPost, "/sync", nil)
		if err != nil {
			resp.Status = http.StatusInternalServerError
			resp.Error = err.Error()
		} else {
			resp.Status = res.status
			resp.Body = res.body
		}
	default:
		resp.Status = http.StatusNotFound
		resp.Error = "unknown manager action"
	}

	_ = peer.send(resp)
}

func (m *Manager) applyHeartbeat(req HeartbeatRequest) {
	m.leaseManager.Heartbeat(req.Name, req.IP, req.Reports, req.Metrics)
	m.appRegistry.AdoptRunningApps(req.Name, req.Reports)
	now := time.Now()
	for _, report := range req.Reports {
		m.appRegistry.UpdateFromReport(report, req.Name, now)
	}
}

type rawResponse struct {
	status int
	body   []byte
}

func (m *Manager) invokeRawEndpoint(method, path string, body []byte) (rawResponse, error) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	rec := httptest.NewRecorder()

	switch {
	case path == "/docker/sync":
		m.handleDockerSync(rec, req)
	case path == "/sync":
		m.handleSync(rec, req)
	case strings.HasPrefix(path, "/agents/") && strings.HasSuffix(path, "/desired"):
		m.handleAgentRoutes(rec, req)
	case strings.HasPrefix(path, "/agents/") && strings.HasSuffix(path, "/caddy-config"):
		m.handleAgentRoutes(rec, req)
	case strings.HasPrefix(path, "/agents/") && strings.HasSuffix(path, "/traefik-config"):
		m.handleAgentRoutes(rec, req)
	default:
		return rawResponse{}, fmt.Errorf("unsupported path %s", path)
	}

	res := rec.Result()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return rawResponse{}, err
	}
	return rawResponse{status: res.StatusCode, body: b}, nil
}

func (peer *agentLinkPeer) send(msg common.LinkMessage) error {
	peer.writeMu.Lock()
	defer peer.writeMu.Unlock()
	return peer.enc.Encode(msg)
}

func (m *Manager) requestAgent(agentName string, req common.LinkMessage) (chan common.LinkMessage, *agentLinkPeer, string, error) {
	m.linkMu.RLock()
	peer := m.agentLinks[agentName]
	m.linkMu.RUnlock()
	if peer == nil {
		return nil, nil, "", fmt.Errorf("agent %s link unavailable", agentName)
	}

	if req.ID == "" {
		req.ID = fmt.Sprintf("%d-%06d", time.Now().UnixNano(), rand.Intn(1000000))
	}
	req.Type = common.LinkTypeRequest

	ch := make(chan common.LinkMessage, 256)
	peer.pendingMu.Lock()
	peer.pending[req.ID] = ch
	peer.pendingMu.Unlock()

	if err := peer.send(req); err != nil {
		peer.removePending(req.ID, ch)
		return nil, nil, "", err
	}

	return ch, peer, req.ID, nil
}

func (m *Manager) requestAgentUnary(ctx context.Context, agentID string, requestType common.LinkRequestType, payload any) (int, []byte, error) {
	raw, err := common.EncodePayload(payload)
	if err != nil {
		return 0, nil, err
	}
	ch, peer, reqID, err := m.requestAgent(agentID, common.LinkMessage{RequestType: requestType, Payload: raw})
	if err != nil {
		return 0, nil, err
	}
	defer peer.removePending(reqID, ch)

	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-time.After(agentRequestTimeout):
		return 0, nil, fmt.Errorf("agent request timeout")
	case msg, ok := <-ch:
		if !ok {
			return 0, nil, fmt.Errorf("agent link closed")
		}
		if msg.Error != "" {
			return msg.Status, nil, fmt.Errorf("%s", msg.Error)
		}
		return msg.Status, msg.Body, nil
	}
}

func firstMessageTimeoutForRequest(requestType common.LinkRequestType) time.Duration {
	switch requestType {
	case common.LinkReqAppLogs,
		common.LinkReqContainerLogs,
		common.LinkReqRepoCheckLogs,
		common.LinkReqRepoUnlockLogs:
		return agentHeavyStreamTimeout
	default:
		return agentStreamFirstTimeout
	}
}

func (m *Manager) streamAgent(agentID string, requestType common.LinkRequestType, payload any) (io.ReadCloser, int, error) {
	raw, err := common.EncodePayload(payload)
	if err != nil {
		return nil, 0, err
	}
	ch, peer, reqID, err := m.requestAgent(agentID, common.LinkMessage{RequestType: requestType, Payload: raw})
	if err != nil {
		return nil, 0, err
	}

	var first common.LinkMessage
	firstTimeout := firstMessageTimeoutForRequest(requestType)
	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, 0, fmt.Errorf("agent link closed")
		}
		first = msg
	case <-time.After(firstTimeout):
		peer.removePending(reqID, ch)
		return nil, 0, fmt.Errorf("agent request timeout")
	}
	if first.Status != http.StatusOK {
		peer.removePending(reqID, ch)
		if first.Error != "" {
			return nil, first.Status, fmt.Errorf("%s", first.Error)
		}
		return nil, first.Status, fmt.Errorf("status %d", first.Status)
	}

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer peer.removePending(reqID, ch)
		if len(first.Body) > 0 {
			_, _ = pw.Write(first.Body)
		}
		for msg := range ch {
			if len(msg.Body) > 0 {
				_, _ = pw.Write(msg.Body)
			}
			if msg.Done {
				return
			}
		}
	}()

	return pr, first.Status, nil
}

var _ = slog.LevelInfo
