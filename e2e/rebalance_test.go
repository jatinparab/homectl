package e2e_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// ─── manual reassignment ─────────────────────────────────────────────────────

// TestManualReassignment verifies that an app can be manually pinned to a
// specific node via POST /api/v1/apps/{appId}/reassign?node={node} and that
// the assignment is reflected immediately.
func TestManualReassignment(t *testing.T) {
	// Make sure adguard is assigned somewhere first
	var currentNode string
	poll(t, "adguard has assigned_agent", 2*time.Minute, 5*time.Second, func() string {
		a := findApp(getApps(t), appAdguard)
		if a == nil {
			return "app not found"
		}
		if a.AssignedAgent == nil {
			return "assigned_agent is nil"
		}
		currentNode = *a.AssignedAgent
		return ""
	})

	// Pick a different target node
	targetNode := differentNode(currentNode)
	t.Logf("adguard currently on %q; moving to %q", currentNode, targetNode)

	// Reassign
	code := apiPost(t, fmt.Sprintf("/api/v1/apps/%s/reassign?node=%s", appAdguard, targetNode))
	if code != http.StatusOK {
		t.Fatalf("reassign adguard to %q: HTTP %d", targetNode, code)
	}

	// Assignment should update within the next reconcile window
	poll(t, "adguard reassigned to "+targetNode, 1*time.Minute, 5*time.Second, func() string {
		a := findApp(getApps(t), appAdguard)
		if a == nil {
			return "app not found"
		}
		if a.AssignedAgent == nil {
			return "assigned_agent is nil"
		}
		if *a.AssignedAgent != targetNode {
			return fmt.Sprintf("assigned_agent=%q, want %q", *a.AssignedAgent, targetNode)
		}
		return ""
	})

	// Clean up: clear the manual assignment so rebalancing can move it again
	t.Cleanup(func() {
		apiDelete(t, fmt.Sprintf("/api/v1/apps/%s/reassign", appAdguard))
	})
}

// TestClearManualAssignment verifies that DELETE /api/v1/apps/{appId}/reassign
// clears the sticky placement and returns HTTP 200.
func TestClearManualAssignment(t *testing.T) {
	// First pin adguard to node1
	code := apiPost(t, fmt.Sprintf("/api/v1/apps/%s/reassign?node=%s", appAdguard, node1))
	if code != http.StatusOK {
		t.Fatalf("pin adguard to %q: HTTP %d", node1, code)
	}

	// Now clear it
	code = apiDelete(t, fmt.Sprintf("/api/v1/apps/%s/reassign", appAdguard))
	if code != http.StatusOK {
		t.Fatalf("clear manual assignment: HTTP %d", code)
	}

	// After clearing, the manager may keep the current assignment but
	// manually_assigned should be false (not exposed in API directly, but
	// the app stays assigned — just no longer sticky).
	// Verify the app is still present and functional.
	app := findApp(getApps(t), appAdguard)
	if app == nil {
		t.Fatalf("adguard disappeared after clearing assignment")
	}
}

// ─── force rebalance ─────────────────────────────────────────────────────────

// TestForceRebalance verifies that POST /api/v1/rebalance returns HTTP 200 and
// the cluster apps are distributed across at most (n+1) per node for n apps on
// k nodes, i.e. the difference between max and min load is ≤ 1.
func TestForceRebalance(t *testing.T) {
	// First ensure all 4 apps are DesiredRunning so there is something to balance
	for _, id := range []string{appAdguard, appTraefik, appPaperless, appVaultwarden} {
		setDesiredState(t, id, "running")
	}
	t.Cleanup(func() {
		// Restore stopped apps to stopped
		setDesiredState(t, appPaperless, "stopped")
		setDesiredState(t, appVaultwarden, "stopped")
	})

	// Wait for all apps to be assigned
	poll(t, "all 4 apps assigned", 2*time.Minute, 5*time.Second, func() string {
		apps := getApps(t)
		for _, id := range []string{appAdguard, appTraefik, appPaperless, appVaultwarden} {
			a := findApp(apps, id)
			if a == nil {
				return fmt.Sprintf("app %q not found", id)
			}
			if a.AssignedAgent == nil {
				return fmt.Sprintf("app %q not assigned", id)
			}
		}
		return ""
	})

	// Trigger a force rebalance
	code := apiPost(t, "/api/v1/rebalance")
	if code != http.StatusOK {
		t.Fatalf("POST /api/v1/rebalance: HTTP %d", code)
	}

	// After rebalancing, the load difference between most-loaded and
	// least-loaded agent should be ≤ 1.
	poll(t, "apps distributed evenly", 1*time.Minute, 5*time.Second, func() string {
		apps := getApps(t)
		load := map[string]int{node1: 0, node2: 0, node3: 0}
		for _, a := range apps {
			if a.DesiredState != "running" || a.IsGlobal || a.AssignedAgent == nil {
				continue
			}
			load[*a.AssignedAgent]++
		}
		maxLoad, minLoad := 0, int(^uint(0)>>1)
		for _, n := range load {
			if n > maxLoad {
				maxLoad = n
			}
			if n < minLoad {
				minLoad = n
			}
		}
		if maxLoad-minLoad > 1 {
			return fmt.Sprintf("unbalanced: load=%v (diff=%d)", load, maxLoad-minLoad)
		}
		return ""
	})
}

// TestReassignToOfflineNode verifies the API rejects reassignment to an agent
// that is not currently alive.
func TestReassignToOfflineNode(t *testing.T) {
	code := apiPost(t, fmt.Sprintf("/api/v1/apps/%s/reassign?node=ghost-node", appAdguard))
	if code != http.StatusBadRequest {
		t.Errorf("expected HTTP 400 for offline node, got %d", code)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// differentNode returns one of the known nodes that is not currentNode.
func differentNode(currentNode string) string {
	for _, n := range []string{node1, node2, node3} {
		if n != currentNode {
			return n
		}
	}
	return node1 // fallback (should not happen)
}
