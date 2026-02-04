package agent

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

const (
	// SensorInterval is how often the sensor samples docker state
	SensorInterval = 5 * time.Second
	// HealthCheckTimeout is the timeout for health check HTTP requests
	HealthCheckTimeout = 3 * time.Second
)

// appStatus holds the sampled status for an app
type appStatus struct {
	appID   string
	running common.RunningStatus
	health  common.HealthStatus
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
		log.Printf("Sensor: failed to list managed apps: %s", err)
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
		if status.health == common.HealthHealthy {
			rt.SetLastError("")
			rt.ResetRestartCount()
		}
	}

	// Mark apps not in managed list as not running
	for appID, rt := range a.runtime {
		if !seen[appID] {
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

	// Check if running
	running, err := a.docker.IsRunning(appID)
	if err != nil {
		log.Printf("Sensor: failed to check running status for %s: %s", appID, err)
		return status
	}
	status.running = running

	// Only check health if running
	if running != common.RunningStatusRunning {
		return status
	}

	// Get ports for health check
	ports, err := a.docker.GetPorts(appID)
	if err != nil {
		log.Printf("Sensor: failed to get ports for %s: %s", appID, err)
		return status
	}

	// Health check on first port if available
	if len(ports) > 0 {
		status.health = a.healthCheck(ports[0])
	}

	return status
}

// healthCheck performs an HTTP health check on the given port
func (a *Agent) healthCheck(port int) common.HealthStatus {
	url := fmt.Sprintf("http://localhost:%d", port)

	client := &http.Client{
		Timeout: HealthCheckTimeout,
	}

	resp, err := client.Get(url)
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
	a.runtime[appID] = rt
	return rt
}
