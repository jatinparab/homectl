//go:build e2e

package e2e_test

import (
	"fmt"
	"testing"
	"time"
)

// containerStartTimeout is the time we allow for an agent to pull the image and
// start a container inside its DinD environment. Image pulls on first run can
// take several minutes depending on network speed.
const containerStartTimeout = 8 * time.Minute

// containerStopTimeout is the time we allow for an agent to stop a container.
const containerStopTimeout = 2 * time.Minute

// ─── default running apps ────────────────────────────────────────────────────

// TestCaddyRunningOnAgent verifies that the caddy container eventually
// reaches observed_state=running on whichever agent it was assigned to.
// This tests the full pipeline: manager assigns → agent syncs configs → agent
// runs docker compose up → agent sensor detects running → agent reports via
// heartbeat → manager shows observed_state=running.
func TestCaddyRunningOnAgent(t *testing.T) {
	waitForAppRunning(t, appCaddy)
}

// TestAdguardRunningOnAgent verifies that adguard reaches observed_state=running.
func TestAdguardRunningOnAgent(t *testing.T) {
	waitForAppRunning(t, appAdguard)
}

// ─── start / stop cycle ──────────────────────────────────────────────────────

// TestStartStopCycle performs a full lifecycle round-trip for vaultwarden:
//  1. Set desired_state=running
//  2. Wait for observed_state=running on the assigned agent
//  3. Set desired_state=stopped
//  4. Wait for observed_state=stopped (or no report) on the agent
func TestStartStopCycle(t *testing.T) {
	const app = appVaultwarden

	t.Cleanup(func() {
		setDesiredState(t, app, "stopped")
	})

	// ── start ──
	setDesiredState(t, app, "running")

	// Wait for assignment
	var assignedNode string
	poll(t, app+" assigned to a node", 2*time.Minute, 5*time.Second, func() string {
		a := findApp(getApps(t), app)
		if a == nil {
			return "app not found"
		}
		if a.AssignedAgent == nil {
			return "assigned_agent is nil"
		}
		assignedNode = *a.AssignedAgent
		return ""
	})
	t.Logf("vaultwarden assigned to %q", assignedNode)

	// Wait for container to be running
	waitForAppRunning(t, app)

	// ── stop ──
	setDesiredState(t, app, "stopped")

	// Wait for container to be stopped
	poll(t, app+" observed_state=stopped", containerStopTimeout, 5*time.Second, func() string {
		apps := getApps(t)
		app := findApp(apps, app)
		if app == nil {
			return "app not found"
		}
		// observed_state is consolidated worst-case; once stopped it shows "stopped" or "unknown"
		state := app.ObservedState
		if state == "stopped" || state == "unknown" || state == "" {
			return ""
		}
		// Also accept: no node_reports (agent hasn't reported yet after stop)
		if len(app.NodeReports) == 0 {
			return ""
		}
		return fmt.Sprintf("observed_state=%q", state)
	})
	t.Logf("vaultwarden stopped successfully")
}

// TestPaperlessStartStopCycle verifies that paperless (multi-service compose:
// paperless-ngx + redis) can be started and stopped correctly.
func TestPaperlessStartStopCycle(t *testing.T) {
	const app = appPaperless

	t.Cleanup(func() {
		setDesiredState(t, app, "stopped")
	})

	setDesiredState(t, app, "running")

	// paperless has multiple services; allow extra time for broker + app to start
	waitForAppRunning(t, app)

	setDesiredState(t, app, "stopped")

	poll(t, app+" observed_state=stopped", containerStopTimeout, 5*time.Second, func() string {
		a := findApp(getApps(t), app)
		if a == nil {
			return "app not found"
		}
		state := a.ObservedState
		if state == "stopped" || state == "unknown" || state == "" || len(a.NodeReports) == 0 {
			return ""
		}
		return fmt.Sprintf("observed_state=%q", state)
	})
}

// ─── agent-reported state ────────────────────────────────────────────────────

// TestAgentReportsAppState verifies that after caddy starts, the assigned
// agent includes it in its app_reports with observed_state=running.
func TestAgentReportsAppState(t *testing.T) {
	// Ensure caddy is running before we check agent reports
	waitForAppRunning(t, appCaddy)

	// Find the assigned agent for caddy
	apps := getApps(t)
	caddy := findApp(apps, appCaddy)
	if caddy == nil || caddy.AssignedAgent == nil {
		t.Skip("caddy not assigned to any agent")
	}
	assignedNode := *caddy.AssignedAgent

	// The assigned agent should report caddy as running in its app_reports
	poll(t, "agent "+assignedNode+" reports caddy running", 1*time.Minute, 5*time.Second, func() string {
		agents := getAgents(t)
		agent := findAgent(agents, assignedNode)
		if agent == nil {
			return fmt.Sprintf("agent %q not found", assignedNode)
		}
		for _, r := range agent.AppReports {
			if r.AppID == appCaddy {
				if r.ObservedState == "running" {
					return ""
				}
				return fmt.Sprintf("app_report observed_state=%q", r.ObservedState)
			}
		}
		return "caddy not in agent app_reports"
	})
}

// ─── helper ──────────────────────────────────────────────────────────────────

// waitForAppRunning polls until the named app shows observed_state=running on
// any of its node reports.
func waitForAppRunning(t *testing.T, appID string) {
	t.Helper()
	poll(t, appID+" observed_state=running", containerStartTimeout, 10*time.Second, func() string {
		apps := getApps(t)
		app := findApp(apps, appID)
		if app == nil {
			return "app not found"
		}
		if app.ObservedState == "running" {
			return ""
		}
		// Check node reports directly
		for _, nr := range app.NodeReports {
			if nr.ObservedState == "running" {
				return ""
			}
		}
		return fmt.Sprintf("observed_state=%q (node_reports=%d)", app.ObservedState, len(app.NodeReports))
	})
}
