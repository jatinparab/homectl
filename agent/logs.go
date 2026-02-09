package agent

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// LogsServerPort is the port for the logs server
	LogsServerPort = 8081
)

// LogsServer provides log streaming for docker compose apps
type LogsServer struct {
	agent  *Agent
	server *http.Server
}

// NewLogsServer creates a new logs server
func NewLogsServer(agent *Agent) *LogsServer {
	return &LogsServer{
		agent: agent,
	}
}

// Start starts the logs server
func (ls *LogsServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/logs/", ls.handleLogs)

	ls.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", LogsServerPort),
		Handler: mux,
	}

	log.Printf("Logs server starting on :%d", LogsServerPort)
	return ls.server.ListenAndServe()
}

// Stop stops the logs server
func (ls *LogsServer) Stop() error {
	if ls.server != nil {
		return ls.server.Close()
	}
	return nil
}

// handleLogs streams docker compose logs for an app
// GET /logs/{appId}
func (ls *LogsServer) handleLogs(w http.ResponseWriter, r *http.Request) {
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

	if ls.agent == nil || ls.agent.docker == nil {
		http.Error(w, "Agent not connected", http.StatusServiceUnavailable)
		return
	}

	// Get the docker compose file path
	composePath := filepath.Join(ls.agent.dockerRootDir, appID, "docker-compose.yml")

	// Execute docker compose logs -f --tail=100
	cmd := exec.CommandContext(r.Context(), "docker", "compose", "-f", composePath, "logs", "-f", "--tail=100")

	// Set environment variables including APPDATA
	cmd.Env = ls.agent.docker.ComposeEnv()

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
