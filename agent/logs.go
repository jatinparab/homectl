package agent

import (
	"bufio"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/logger"
)

// LogsHandler provides log streaming handlers for docker compose apps
type LogsHandler struct {
	agent *Agent
}

// NewLogsHandler creates a new logs handler
func NewLogsHandler(agent *Agent) *LogsHandler {
	return &LogsHandler{
		agent: agent,
	}
}

// RegisterRoutes registers all log-related routes on the given mux
func (lh *LogsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/logs/", lh.handleLogs)
	mux.HandleFunc("/phase-logs/", lh.handlePhaseLogs)
	mux.HandleFunc("/system-logs", lh.handleSystemLogs)
	mux.HandleFunc("/containers", lh.handleContainers)
	mux.HandleFunc("/container-logs/", lh.handleContainerLogs)
}

// handleLogs streams docker compose logs for an app
// GET /logs/{appId}
func (lh *LogsHandler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract appId from path: /logs/{appId}
	appID := strings.TrimPrefix(r.URL.Path, "/logs/")
	appID = strings.TrimSuffix(appID, "/")

	if appID == "" {
		http.Error(w, "App ID required", http.StatusBadRequest)
		return
	}

	if lh.agent == nil || lh.agent.docker == nil {
		http.Error(w, "Agent not connected", http.StatusServiceUnavailable)
		return
	}

	// Get the docker compose file path
	composePath := filepath.Join(lh.agent.dockerRootDir, appID, "docker-compose.yml")

	// Execute docker compose logs -f --tail=100
	cmd := exec.CommandContext(r.Context(), "docker", "compose", "-f", composePath, "logs", "-f", "--tail=100")

	// Set environment variables including APPDATA
	cmd.Env = lh.agent.docker.ComposeEnv()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "Failed to create stdout pipe: "+err.Error(), http.StatusInternalServerError)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		http.Error(w, "Failed to create stderr pipe: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := cmd.Start(); err != nil {
		http.Error(w, "Failed to start docker compose logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Set streaming headers
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Stream stdout and stderr
	done := make(chan struct{})

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			w.Write([]byte(scanner.Text() + "\n"))
			flusher.Flush()
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			w.Write([]byte(scanner.Text() + "\n"))
			flusher.Flush()
		}
		close(done)
	}()

	// Wait for context cancellation or command completion
	select {
	case <-r.Context().Done():
		cmd.Process.Kill()
	case <-done:
	}

	cmd.Wait()
}

// handlePhaseLogs streams the phase log buffer (e.g. pull output) for an app
// GET /phase-logs/{appId}
func (lh *LogsHandler) handlePhaseLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appID := strings.TrimPrefix(r.URL.Path, "/phase-logs/")
	appID = strings.TrimSuffix(appID, "/")

	if appID == "" {
		http.Error(w, "App ID required", http.StatusBadRequest)
		return
	}

	rt := lh.agent.getRuntime(common.AppID(appID))
	if rt == nil || rt.PhaseLog == nil {
		http.Error(w, "App not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send snapshot first
	for _, line := range rt.PhaseLog.Snapshot() {
		w.Write([]byte(line + "\n"))
	}
	flusher.Flush()

	// Follow live lines
	ch := rt.PhaseLog.Follow()
	defer rt.PhaseLog.Unfollow(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			w.Write([]byte(line + "\n"))
			flusher.Flush()
		}
	}
}

// handleSystemLogs streams in-process log lines (from the ring buffer) as SSE.
// GET /system-logs
func (lh *LogsHandler) handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send buffered history first
	for _, line := range logger.Buffer.Snapshot() {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()

	// Follow live
	ch := logger.Buffer.Follow()
	defer logger.Buffer.Unfollow(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}
	}
}
