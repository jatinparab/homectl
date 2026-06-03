//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"
)

// TestManagerHealthy verifies the manager HTTP API is up.
func TestManagerHealthy(t *testing.T) {
	resp, err := http.Get(managerURL() + "/health") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health: want 200, got %d", resp.StatusCode)
	}
}

// TestDashboardStructure verifies the dashboard endpoint returns a well-formed
// response with at least the expected number of agents and apps.
func TestDashboardStructure(t *testing.T) {
	dash := apiGet[DashboardResponse](t, "/api/v1/dashboard")

	if dash.TotalAgents < expectedAgents {
		t.Errorf("dashboard.total_agents = %d, want >= %d", dash.TotalAgents, expectedAgents)
	}
	if dash.AgentsOnline < expectedAgents {
		t.Errorf("dashboard.agents_online = %d, want >= %d", dash.AgentsOnline, expectedAgents)
	}
	// 4 apps are in e2e/apps/
	if dash.TotalApps < 4 {
		t.Errorf("dashboard.total_apps = %d, want >= 4", dash.TotalApps)
	}
}

// TestDashboardAppsTotal verifies that the dashboard reports exactly 4 apps
// (matching e2e/apps/). This is a pure control-plane check; observed running
// counts depend on containers starting and are covered in lifecycle_test.go.
func TestDashboardAppsTotal(t *testing.T) {
	dash := apiGet[DashboardResponse](t, "/api/v1/dashboard")
	if dash.TotalApps != 4 {
		t.Errorf("dashboard.total_apps = %d, want 4", dash.TotalApps)
	}
}
