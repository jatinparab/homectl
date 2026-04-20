// Package e2e contains end-to-end tests for homectl.
// The tests run against a live cluster started with `make e2e` (or `e2e/run.sh up`).
//
// Usage:
//
//	# Start cluster first
//	make e2e
//
//	# Then run tests (from repo root)
//	go test -v -timeout 15m ./e2e/
//
//	# Or run with a non-default manager URL
//	HOMECTL_MANAGER_URL=http://localhost:1338 go test -v -timeout 15m ./e2e/
package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// ─── cluster config ──────────────────────────────────────────────────────────

const (
	expectedAgents = 3

	// node names as set in docker-compose.yml HOMECTL_AGENT_NAME env vars
	node1 = "e2e-node-1"
	node2 = "e2e-node-2"
	node3 = "e2e-node-3"

	// apps present in e2e/apps/ — traefik+adguard are DesiredRunning by default
	appAdguard    = "adguard"
	appTraefik    = "traefik"
	appPaperless  = "paperless"
	appVaultwarden = "vaultwarden"
)

func managerURL() string {
	if u := os.Getenv("HOMECTL_MANAGER_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:1338"
}

// ─── API response types (mirrors manager/api.go) ─────────────────────────────

type DashboardResponse struct {
	TotalAgents     int `json:"total_agents"`
	AgentsOnline    int `json:"agents_online"`
	TotalApps       int `json:"total_apps"`
	AppsRunning     int `json:"apps_running"`
	AppsStopped     int `json:"apps_stopped"`
	AppsError       int `json:"apps_error"`
	AppsQuarantined int `json:"apps_quarantined"`
}

type NodeMetrics struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryTotal   uint64  `json:"memory_total"`
	MemoryUsed    uint64  `json:"memory_used"`
	MemoryPercent float64 `json:"memory_percent"`
	DiskTotal     uint64  `json:"disk_total"`
	DiskUsed      uint64  `json:"disk_used"`
	DiskFree      uint64  `json:"disk_free"`
	DiskPercent   float64 `json:"disk_percent"`
}

type AppReport struct {
	AppID         string  `json:"app_id"`
	ObservedState string  `json:"observed_state"`
	Health        string  `json:"health"`
	Phase         string  `json:"phase"`
	LastError     string  `json:"last_error"`
	RestartCount  int     `json:"restart_count"`
	Quarantined   bool    `json:"quarantined"`
	Port          int     `json:"port"`
}

type APIAgentInfo struct {
	Name         string      `json:"name"`
	IP           string      `json:"ip"`
	LastSeen     string      `json:"last_seen"`
	RegisteredAt string      `json:"registered_at"`
	Online       bool        `json:"online"`
	IsGateway    bool        `json:"is_gateway"`
	Metrics      NodeMetrics `json:"metrics"`
	AppReports   []AppReport `json:"app_reports"`
}

type NodeReport struct {
	Node          string `json:"node"`
	NodeOnline    bool   `json:"node_online"`
	ObservedState string `json:"observed_state"`
	Health        string `json:"health"`
	Phase         string `json:"phase"`
	LastError     string `json:"last_error"`
	RestartCount  int    `json:"restart_count"`
	Quarantined   bool   `json:"quarantined"`
	Port          int    `json:"port"`
}

type AppStatus struct {
	AppID         string      `json:"app_id"`
	DesiredState  string      `json:"desired_state"`
	AssignedAgent *string     `json:"assigned_agent"`
	IsGlobal      bool        `json:"is_global"`
	ObservedState string      `json:"observed_state"`
	Health        string      `json:"health"`
	Phase         string      `json:"phase"`
	NodeReports   []NodeReport `json:"node_reports"`
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func apiGet[T any](t *testing.T, path string) T {
	t.Helper()
	u := managerURL() + path
	resp, err := http.Get(u) //nolint:noctx
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: HTTP %d — %s", u, resp.StatusCode, body)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("GET %s: decode: %v", u, err)
	}
	return out
}

func apiPut(t *testing.T, path string) int {
	t.Helper()
	u := managerURL() + path
	req, _ := http.NewRequest(http.MethodPut, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", u, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func apiPost(t *testing.T, path string) int {
	t.Helper()
	u := managerURL() + path
	resp, err := http.Post(u, "application/json", nil) //nolint:noctx
	if err != nil {
		t.Fatalf("POST %s: %v", u, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func apiDelete(t *testing.T, path string) int {
	t.Helper()
	u := managerURL() + path
	req, _ := http.NewRequest(http.MethodDelete, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", u, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// ─── polling helpers ─────────────────────────────────────────────────────────

// poll calls cond every interval until it returns ("", nil reason) or the
// timeout elapses, at which point it calls t.Fatalf with the last reason.
func poll(t *testing.T, desc string, timeout, interval time.Duration, cond func() string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var reason string
	for time.Now().Before(deadline) {
		reason = cond()
		if reason == "" {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("timeout after %s waiting for %q: %s", timeout, desc, reason)
}

// ─── query helpers ────────────────────────────────────────────────────────────

func getAgents(t *testing.T) []APIAgentInfo {
	t.Helper()
	return apiGet[[]APIAgentInfo](t, "/api/v1/agents")
}

func getApps(t *testing.T) []AppStatus {
	t.Helper()
	return apiGet[[]AppStatus](t, "/api/v1/apps")
}

func findApp(apps []AppStatus, id string) *AppStatus {
	for i := range apps {
		if apps[i].AppID == id {
			return &apps[i]
		}
	}
	return nil
}

func findAgent(agents []APIAgentInfo, name string) *APIAgentInfo {
	for i := range agents {
		if agents[i].Name == name {
			return &agents[i]
		}
	}
	return nil
}

func setDesiredState(t *testing.T, appID, state string) {
	t.Helper()
	code := apiPut(t, fmt.Sprintf("/api/v1/apps-desired-state/%s?state=%s", appID, state))
	if code != http.StatusOK {
		t.Fatalf("set desired state %q for %q: HTTP %d", state, appID, code)
	}
}

func countOnlineAgents(agents []APIAgentInfo) int {
	n := 0
	for _, a := range agents {
		if a.Online {
			n++
		}
	}
	return n
}

// observedStateOnAnyNode returns the observed_state that the named app shows on
// any of its node_reports, or "" if none found.
func observedStateOnAnyNode(apps []AppStatus, appID string) string {
	app := findApp(apps, appID)
	if app == nil {
		return ""
	}
	for _, nr := range app.NodeReports {
		if nr.ObservedState != "" && nr.ObservedState != "unknown" {
			return nr.ObservedState
		}
	}
	return app.ObservedState
}

// ─── TestMain: wait for cluster before running any test ──────────────────────

func TestMain(m *testing.M) {
	fmt.Printf("[e2e] Manager: %s\n", managerURL())
	fmt.Printf("[e2e] Waiting for cluster (3 agents) to be ready …\n")

	if err := waitForCluster(3 * time.Minute); err != nil {
		fmt.Fprintf(os.Stderr, "[e2e] cluster not ready: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[e2e] Cluster ready. Running tests …\n\n")
	os.Exit(m.Run())
}

func waitForCluster(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// 1. Manager health check
		resp, err := http.Get(managerURL() + "/health") //nolint:noctx
		if err != nil || resp.StatusCode != http.StatusOK {
			time.Sleep(3 * time.Second)
			continue
		}
		resp.Body.Close()

		// 2. All 3 agents online
		agentsResp, err := http.Get(managerURL() + "/api/v1/agents") //nolint:noctx
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		var agents []APIAgentInfo
		json.NewDecoder(agentsResp.Body).Decode(&agents) //nolint:errcheck
		agentsResp.Body.Close()

		if countOnlineAgents(agents) >= expectedAgents {
			return nil
		}
		fmt.Printf("[e2e]   … %d/%d agents online\n", countOnlineAgents(agents), expectedAgents)
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("waited %s but cluster never became ready", timeout)
}
