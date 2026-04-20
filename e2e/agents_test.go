package e2e_test

import (
	"fmt"
	"testing"
	"time"
)

// TestAllAgentsRegistered verifies all expected agents appear in /api/v1/agents.
func TestAllAgentsRegistered(t *testing.T) {
	agents := getAgents(t)
	for _, name := range []string{node1, node2, node3} {
		if findAgent(agents, name) == nil {
			t.Errorf("agent %q not registered; registered agents: %v", name, agentNames(agents))
		}
	}
}

// TestAllAgentsOnline verifies all expected agents are currently online.
func TestAllAgentsOnline(t *testing.T) {
	agents := getAgents(t)
	for _, name := range []string{node1, node2, node3} {
		a := findAgent(agents, name)
		if a == nil {
			t.Errorf("agent %q not found", name)
			continue
		}
		if !a.Online {
			t.Errorf("agent %q: online=false (last_seen=%s)", name, a.LastSeen)
		}
	}
}

// TestAgentMetricsReported verifies that each agent reports basic node metrics.
// We check that at least one numeric metric field is non-zero (disk at minimum,
// since DinD containers always have some disk usage).
func TestAgentMetricsReported(t *testing.T) {
	// Metrics arrive after the first heartbeat+sensor cycle. Allow up to 30 s.
	poll(t, "agents report non-zero metrics", 30*time.Second, 3*time.Second, func() string {
		agents := getAgents(t)
		for _, name := range []string{node1, node2, node3} {
			a := findAgent(agents, name)
			if a == nil {
				return fmt.Sprintf("agent %q not found", name)
			}
			if a.Metrics.DiskTotal == 0 && a.Metrics.MemoryTotal == 0 {
				return fmt.Sprintf("agent %q: no metrics yet (disk=%d, mem=%d)",
					name, a.Metrics.DiskTotal, a.Metrics.MemoryTotal)
			}
		}
		return ""
	})
}

// TestOnlyOneGateway verifies that at most one agent is marked as the gateway.
func TestOnlyOneGateway(t *testing.T) {
	agents := getAgents(t)
	gateways := 0
	for _, a := range agents {
		if a.IsGateway {
			gateways++
		}
	}
	if gateways > 1 {
		t.Errorf("expected at most 1 gateway, got %d", gateways)
	}
}

// TestAgentHeartbeatFresh verifies last_seen timestamps are recent (< 30 s).
func TestAgentHeartbeatFresh(t *testing.T) {
	agents := getAgents(t)
	for _, a := range agents {
		if !a.Online {
			t.Errorf("agent %q: not online (last_seen=%s)", a.Name, a.LastSeen)
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func agentNames(agents []APIAgentInfo) []string {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
	}
	return names
}
