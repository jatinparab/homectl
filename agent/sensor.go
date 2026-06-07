package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

var sensorLog = slog.With("area", "sensor")

const (
	// SensorInterval is how often the sensor samples docker state
	SensorInterval = 2 * time.Second
	// HealthCheckTimeout is the timeout for health check HTTP requests
	HealthCheckTimeout = 3 * time.Second
)

// appStatus holds the sampled status for an app
type appStatus struct {
	appID   string
	running common.RunningStatus
	health  common.HealthStatus
	port    int
}

// sensorLoop runs the sensor loop that observes docker container state
func (a *Agent) sensorLoop(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(SensorInterval)
	defer ticker.Stop()

	// Initial sample
	a.sample()

	for {
		select {
		case <-ticker.C:
			if a.IsConnected() && a.docker != nil {
				a.sample()
			}
		case <-ctx.Done():
			return
		case <-a.stopChan:
			return
		}
	}
}

// sample checks docker status and health for all apps in parallel
func (a *Agent) sample() {
	if a.docker == nil {
		return
	}

	// Get list of managed apps
	entries, err := a.docker.ListManaged()
	if err != nil {
		sensorLog.Error("Failed to list managed apps", "err", err)
		return
	}

	// Collect all app IDs to check
	appIDs := make([]string, 0, len(entries))
	for appID := range entries {
		appIDs = append(appIDs, appID)
	}

	if len(appIDs) == 0 {
		return
	}

	// Check all apps in parallel
	results := a.checkAppsParallel(appIDs)

	// Update runtime state
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()

	// Track which apps we've seen
	seen := make(map[common.AppID]bool)
	for _, status := range results {
		appID := common.AppID(status.appID)
		seen[appID] = true
		rt := a.getOrCreateRuntimeLocked(appID)
		rt.SetRunning(status.running)
		rt.SetHealth(status.health)
		rt.SetPort(status.port)

		// Handle stopped containers
		if status.running == common.RunningStatusStopped {
			currentPhase := rt.GetPhase()
			if currentPhase == common.PhaseBackupStopping {
				// Advance backup phase: container confirmed stopped → BackupStopped
				rt.SetPhase(common.PhaseBackupStopped)
				sensorLog.Info("Container confirmed stopped, advancing to BackupStopped", "app", status.appID)
			} else if currentPhase == common.PhaseRunning {
				// Clear stale running phase when container is stopped
				rt.SetPhase(common.PhaseEmpty)
			}
		}

		if status.health == common.HealthHealthy {
			rt.SetLastError("")
			rt.ResetRestartCount()
			// Update phase to running if not in a backup phase
			currentPhase := rt.GetPhase()
			if currentPhase != common.PhaseBackupStopping &&
				currentPhase != common.PhaseBackupStopped &&
				currentPhase != common.PhaseBackingUp {
				rt.SetPhase(common.PhaseRunning)
			}
		}
	}

	// Mark apps not in managed list as not running.
	// Skip caddy — it runs in-process and is managed by the reconcile loop.
	for appID, rt := range a.runtime {
		if !seen[appID] && appID != common.GatewayProxyAppID {
			rt.SetRunning(common.RunningStatusStopped)
			rt.SetHealth(common.HealthUnknown)
		}
	}
}

// checkAppsParallel checks running status and health for all apps in parallel
func (a *Agent) checkAppsParallel(appIDs []string) []appStatus {
	var wg sync.WaitGroup
	results := make(chan appStatus, len(appIDs))

	for _, appID := range appIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			status := a.checkApp(id)
			results <- status
		}(appID)
	}

	// Wait for all checks to complete
	wg.Wait()
	close(results)

	// Collect results
	statuses := make([]appStatus, 0, len(appIDs))
	for status := range results {
		statuses = append(statuses, status)
	}

	return statuses
}

// checkApp checks the running status and health of a single app
func (a *Agent) checkApp(appID string) appStatus {
	status := appStatus{
		appID:  appID,
		health: common.HealthUnknown,
	}

	if common.AppID(appID) == common.GatewayProxyAppID {
		status.running = common.RunningStatusRunning
		status.health = common.HealthHealthy
		return status
	}

	// Check if running
	running, err := a.docker.IsRunning(appID)
	if err != nil {
		sensorLog.Error("Failed to check running status", "app", appID, "err", err)
		return status
	}
	status.running = running

	// Only check health if running
	if running != common.RunningStatusRunning {
		return status
	}

	// Get app info for health check
	app, err := a.docker.GetAppInfo(appID)
	if err != nil {
		sensorLog.Error("Failed to get app info", "app", appID, "err", err)
		return status
	}

	// Health check on first port if available
	if app.HealthHost != "" && app.HealthPort > 0 {
		status.port = app.HealthPort
		url := fmt.Sprintf("http://%s:%d", app.HealthHost, app.HealthPort)
		var hostHeader string
		if a.domainSuffix != "" {
			hostHeader = fmt.Sprintf("%s.internal.%s", app.Name, a.domainSuffix)
		}
		status.health = a.healthCheck(url, hostHeader)
	} else {
		// No ports exposed: if container is running, consider it healthy
		status.health = common.HealthHealthy
	}

	return status
}

// healthCheck performs an HTTP health check on the given URL with an optional Host header.
func (a *Agent) healthCheck(url, hostHeader string) common.HealthStatus {
	client := &http.Client{
		Timeout: HealthCheckTimeout,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return common.HealthUnhealthy
	}
	if hostHeader != "" {
		req.Host = hostHeader
	}

	resp, err := client.Do(req)
	if err != nil {
		return common.HealthUnhealthy
	}
	defer resp.Body.Close()

	// Consider any 2xx or 3xx status as healthy
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return common.HealthHealthy
	}

	return common.HealthUnhealthy
}

// getOrCreateRuntimeLocked gets or creates a runtime for an app
// Caller must hold runtimeMu lock
func (a *Agent) getOrCreateRuntimeLocked(appID common.AppID) *AppRuntime {
	if rt, ok := a.runtime[appID]; ok {
		return rt
	}
	rt := NewAppRuntime(appID)

	// Restore persisted backup timestamp so it survives agent restarts
	if meta, err := loadBackupMeta(a.stateDir, appID); err != nil {
		sensorLog.Warn("Failed to load backup meta", "app", appID, "err", err)
	} else if meta != nil {
		rt.Backup.LastBackupAt = meta.LastBackupAt
		rt.Backup.BackupDefer = time.Time{} // Clear defer since we have real backup data
		sensorLog.Info("Restored backup meta", "app", appID, "last_backup", meta.LastBackupAt.Format(time.RFC3339))
	}

	a.runtime[appID] = rt
	return rt
}
