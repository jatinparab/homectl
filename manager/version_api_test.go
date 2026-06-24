package manager

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jatinparab/homectl/common"
)

func TestVersionAPI(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	rec := httptest.NewRecorder()
	mgr.handleAPIVersion(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var info map[string]string
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if info["component"] != "manager" {
		t.Errorf("expected component manager, got %q", info["component"])
	}
	if info["version"] == "" {
		t.Error("expected non-empty version")
	}
	if info["commit"] == "" {
		t.Error("expected non-empty commit")
	}
	if info["build_date"] == "" {
		t.Error("expected non-empty build_date")
	}
}

func TestVersionAPI_MethodNotAllowed(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/version", nil)
	rec := httptest.NewRecorder()
	mgr.handleAPIVersion(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestAgentHeartbeatVersionPropagation(t *testing.T) {
	lm := NewLeaseManager(DefaultLeaseConfig())

	lm.Heartbeat("agent1", "10.0.0.1", "1.2.3", nil, common.NodeMetrics{})

	info, ok := lm.GetAgent("agent1")
	if !ok {
		t.Fatal("expected agent1 to exist")
	}
	if info.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %q", info.Version)
	}

	// Renew with empty version should preserve existing
	lm.Heartbeat("agent1", "10.0.0.1", "", nil, common.NodeMetrics{})
	info, ok = lm.GetAgent("agent1")
	if !ok {
		t.Fatal("expected agent1 to still exist")
	}
	if info.Version != "1.2.3" {
		t.Errorf("expected version to be preserved, got %q", info.Version)
	}

	// New agent with empty version should have empty version
	lm.Heartbeat("agent2", "10.0.0.2", "", nil, common.NodeMetrics{})
	info, ok = lm.GetAgent("agent2")
	if !ok {
		t.Fatal("expected agent2 to exist")
	}
	if info.Version != "" {
		t.Errorf("expected empty version, got %q", info.Version)
	}
}

func TestAgentInfoVersionInAPI(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate heartbeat with version
	mgr.leaseManager.Heartbeat("test-agent", "10.0.0.10", "0.1.0", nil, common.NodeMetrics{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	mgr.handleAPIAgents(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var agents []APIAgentInfo
	if err := json.NewDecoder(res.Body).Decode(&agents); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}

	if agents[0].Version != "0.1.0" {
		t.Errorf("expected version 0.1.0, got %q", agents[0].Version)
	}

	// Agent detail endpoint
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agents/test-agent", nil)
	rec = httptest.NewRecorder()
	mgr.handleAPIAgentDetail(rec, req)

	res = rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var agent APIAgentInfo
	if err := json.NewDecoder(res.Body).Decode(&agent); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if agent.Version != "0.1.0" {
		t.Errorf("expected version 0.1.0, got %q", agent.Version)
	}
}

func TestHeartbeatHandlerVersionPropagation(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(HeartbeatRequest{
		Name:    "hb-agent",
		IP:      "10.0.0.20",
		Version: "0.2.0",
	})

	req := httptest.NewRequest(http.MethodPost, "/agents/heartbeat", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	mgr.handleHeartbeat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	info, ok := mgr.leaseManager.GetAgent("hb-agent")
	if !ok {
		t.Fatal("expected hb-agent to exist")
	}
	if info.Version != "0.2.0" {
		t.Errorf("expected version 0.2.0, got %q", info.Version)
	}
}

func TestMissingAgentVersionIsEmpty(t *testing.T) {
	mgr, err := NewManager(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}

	// Heartbeat without version (old agent)
	mgr.leaseManager.Heartbeat("old-agent", "10.0.0.30", "", nil, common.NodeMetrics{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	mgr.handleAPIAgents(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	var agents []APIAgentInfo
	json.NewDecoder(res.Body).Decode(&agents)

	if len(agents) < 1 {
		t.Fatal("expected at least 1 agent")
	}

	// Find old-agent
	var found *APIAgentInfo
	for i := range agents {
		if agents[i].Name == "old-agent" {
			found = &agents[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected old-agent")
	}
	if found.Version != "" {
		t.Errorf("expected empty version for old agent, got %q", found.Version)
	}
}
