package e2e_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// ─── discovery ───────────────────────────────────────────────────────────────

// TestAppDiscovery verifies the manager discovers all four apps from e2e/apps/.
func TestAppDiscovery(t *testing.T) {
	apps := getApps(t)
	for _, id := range []string{appAdguard, appTraefik, appPaperless, appVaultwarden} {
		if findApp(apps, id) == nil {
			t.Errorf("app %q not discovered; found: %v", id, appIDs(apps))
		}
	}
}

// TestDefaultDesiredState verifies the manager sets the correct initial desired
// state: traefik and adguard are DesiredRunning; paperless and vaultwarden are
// DesiredStopped (as per DiscoverApps defaults in manager.go).
func TestDefaultDesiredState(t *testing.T) {
	apps := getApps(t)

	runByDefault := map[string]bool{
		appAdguard: true,
		appTraefik: true,
	}

	for _, id := range []string{appAdguard, appTraefik, appPaperless, appVaultwarden} {
		app := findApp(apps, id)
		if app == nil {
			t.Errorf("app %q not found", id)
			continue
		}
		want := "stopped"
		if runByDefault[id] {
			want = "running"
		}
		if app.DesiredState != want {
			t.Errorf("app %q: desired_state=%q, want %q", id, app.DesiredState, want)
		}
	}
}

// ─── assignment ──────────────────────────────────────────────────────────────

// TestRunningAppsAssigned verifies that apps with DesiredRunning get an
// assigned_agent after the manager reconcile loop runs (≤ 2 min).
func TestRunningAppsAssigned(t *testing.T) {
	poll(t, "running apps have assigned_agent", 2*time.Minute, 5*time.Second, func() string {
		apps := getApps(t)
		for _, id := range []string{appAdguard, appTraefik} {
			app := findApp(apps, id)
			if app == nil {
				return fmt.Sprintf("app %q not found", id)
			}
			if app.AssignedAgent == nil {
				return fmt.Sprintf("app %q: assigned_agent is nil", id)
			}
		}
		return ""
	})
}

// TestStoppedAppsNotAssigned verifies that apps with DesiredStopped are not
// assigned by default (they may be nil or assigned but still stopped).
// The manager may assign them to a node; what matters is desired_state=stopped.
func TestStoppedAppsDesiredState(t *testing.T) {
	apps := getApps(t)
	for _, id := range []string{appPaperless, appVaultwarden} {
		app := findApp(apps, id)
		if app == nil {
			t.Errorf("app %q not found", id)
			continue
		}
		if app.DesiredState != "stopped" {
			t.Errorf("app %q: desired_state=%q, want stopped", id, app.DesiredState)
		}
	}
}

// ─── desired state API ───────────────────────────────────────────────────────

// TestSetDesiredStateRunning verifies that setting an app's desired state to
// running via the API is reflected immediately in /api/v1/apps, and that the
// app gets assigned to an agent within the reconcile window.
func TestSetDesiredStateRunning(t *testing.T) {
	const app = appVaultwarden

	// Restore state on exit
	t.Cleanup(func() {
		setDesiredState(t, app, "stopped")
	})

	setDesiredState(t, app, "running")

	// Desired state should flip immediately
	poll(t, app+" desired_state=running", 10*time.Second, 1*time.Second, func() string {
		a := findApp(getApps(t), app)
		if a == nil {
			return "app not found"
		}
		if a.DesiredState != "running" {
			return fmt.Sprintf("desired_state=%q", a.DesiredState)
		}
		return ""
	})

	// App should get assigned within the reconcile window (≤ 1 min)
	poll(t, app+" assigned_agent set", 1*time.Minute, 5*time.Second, func() string {
		a := findApp(getApps(t), app)
		if a == nil {
			return "app not found"
		}
		if a.AssignedAgent == nil {
			return "assigned_agent is nil"
		}
		return ""
	})
}

// TestSetDesiredStateStopped verifies that setting a running app back to stopped
// is reflected immediately in /api/v1/apps.
func TestSetDesiredStateStopped(t *testing.T) {
	const app = appVaultwarden

	// First enable it so the test is meaningful
	setDesiredState(t, app, "running")
	t.Cleanup(func() {
		setDesiredState(t, app, "stopped")
	})

	// Flip back to stopped
	setDesiredState(t, app, "stopped")

	poll(t, app+" desired_state=stopped", 10*time.Second, 1*time.Second, func() string {
		a := findApp(getApps(t), app)
		if a == nil {
			return "app not found"
		}
		if a.DesiredState != "stopped" {
			return fmt.Sprintf("desired_state=%q", a.DesiredState)
		}
		return ""
	})
}

// TestInvalidDesiredState verifies the API rejects unknown state values.
func TestInvalidDesiredState(t *testing.T) {
	code := apiPut(t, fmt.Sprintf("/api/v1/apps-desired-state/%s?state=flying", appPaperless))
	if code != http.StatusBadRequest {
		t.Errorf("expected HTTP 400 for invalid state, got %d", code)
	}
}

// TestDesiredStateOnUnknownApp verifies the API returns 404 for unknown apps.
func TestDesiredStateOnUnknownApp(t *testing.T) {
	code := apiPut(t, "/api/v1/apps-desired-state/no-such-app?state=running")
	if code != http.StatusNotFound {
		t.Errorf("expected HTTP 404 for unknown app, got %d", code)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func appIDs(apps []AppStatus) []string {
	ids := make([]string, len(apps))
	for i, a := range apps {
		ids[i] = a.AppID
	}
	return ids
}
