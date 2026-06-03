package manager

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jatinparab/homectl/common"
)

func setupAgentLinkTest(t *testing.T) (*Manager, net.Conn, *json.Encoder, *json.Decoder) {
	t.Helper()
	t.Setenv("HOMECTL_JOIN_TOKEN", "test-token")
	mgr := NewManagerWithConfig(Config{
		RepoDir: t.TempDir(),
		Port:    0,
		Lease:   DefaultLeaseConfig(),
	})

	serverConn, clientConn := net.Pipe()
	go mgr.attachAgentLink(serverConn)

	enc := json.NewEncoder(clientConn)
	dec := json.NewDecoder(clientConn)
	if err := enc.Encode(common.LinkMessage{
		Type:      common.LinkTypeHello,
		AgentName: "agent-1",
		AgentIP:   "10.0.0.1",
		JoinToken: "test-token",
	}); err != nil {
		t.Fatalf("hello encode failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mgr.linkMu.RLock()
		peer := mgr.agentLinks["agent-1"]
		mgr.linkMu.RUnlock()
		if peer != nil {
			return mgr, clientConn, enc, dec
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent link was not attached")
	return nil, nil, nil, nil
}

func TestRequestAgentUnaryCleansPendingOnNonDoneResponse(t *testing.T) {
	mgr, conn, enc, dec := setupAgentLinkTest(t)
	t.Cleanup(func() { _ = conn.Close() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		var req common.LinkMessage
		if err := dec.Decode(&req); err != nil {
			t.Errorf("decode request failed: %v", err)
			return
		}
		if err := enc.Encode(common.LinkMessage{
			Type:   common.LinkTypeResponse,
			ID:     req.ID,
			Status: 200,
			Body:   []byte(`{"ok":true}`),
			Done:   false, // intentionally false to verify unary cleanup path
		}); err != nil {
			t.Errorf("encode response failed: %v", err)
		}
	}()

	status, body, err := mgr.requestAgentUnary(context.Background(), "agent-1", common.LinkReqContainers, nil)
	if err != nil {
		t.Fatalf("requestAgentUnary failed: %v", err)
	}
	if status != 200 {
		t.Fatalf("expected status 200, got %d", status)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("unexpected body: %s", string(body))
	}
	<-done

	mgr.linkMu.RLock()
	peer := mgr.agentLinks["agent-1"]
	mgr.linkMu.RUnlock()
	if peer == nil {
		t.Fatal("peer missing")
	}
	peer.pendingMu.Lock()
	pendingLen := len(peer.pending)
	peer.pendingMu.Unlock()
	if pendingLen != 0 {
		t.Fatalf("expected no pending entries, got %d", pendingLen)
	}
}

func TestAttachAgentLinkDoneResponseClosesChannel(t *testing.T) {
	mgr, conn, enc, _ := setupAgentLinkTest(t)
	t.Cleanup(func() { _ = conn.Close() })

	mgr.linkMu.RLock()
	peer := mgr.agentLinks["agent-1"]
	mgr.linkMu.RUnlock()
	if peer == nil {
		t.Fatal("peer missing")
	}

	ch := make(chan common.LinkMessage, 4)
	peer.pendingMu.Lock()
	peer.pending["req-1"] = ch
	peer.pendingMu.Unlock()

	if err := enc.Encode(common.LinkMessage{
		Type:   common.LinkTypeResponse,
		ID:     "req-1",
		Status: 200,
		Done:   true,
	}); err != nil {
		t.Fatalf("encode response failed: %v", err)
	}

	select {
	case msg := <-ch:
		if msg.ID != "req-1" || !msg.Done {
			t.Fatalf("unexpected message: %+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}
