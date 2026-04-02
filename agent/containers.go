package agent

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
)

// ContainerInfo represents a docker container's status
type ContainerInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	State   string `json:"state"`
	Ports   string `json:"ports"`
	Created string `json:"created"`
}

// dockerPSEntry matches the JSON output from docker ps --format '{{json .}}'
type dockerPSEntry struct {
	ID      string `json:"ID"`
	Names   string `json:"Names"`
	Image   string `json:"Image"`
	Status  string `json:"Status"`
	State   string `json:"State"`
	Ports   string `json:"Ports"`
	Created string `json:"CreatedAt"`
}

// handleContainers returns all docker containers on this agent
// GET /containers
func (ls *LogsHandler) handleContainers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cmd := exec.Command("docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.Output()
	if err != nil {
		http.Error(w, "Failed to run docker ps: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var containers []ContainerInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var entry dockerPSEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		containers = append(containers, ContainerInfo{
			ID:      entry.ID,
			Name:    entry.Names,
			Image:   entry.Image,
			Status:  entry.Status,
			State:   entry.State,
			Ports:   entry.Ports,
			Created: entry.Created,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(containers)
}

// handleContainerLogs streams docker logs for a specific container
// GET /container-logs/{containerId}?all=false
func (ls *LogsHandler) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	containerID := strings.TrimPrefix(r.URL.Path, "/container-logs/")
	containerID = strings.TrimSuffix(containerID, "/")

	if containerID == "" {
		http.Error(w, "Container ID required", http.StatusBadRequest)
		return
	}

	showAll := r.URL.Query().Get("all") == "true"

	args := []string{"logs", "-f"}
	if !showAll {
		args = append(args, "--tail=100")
	}
	args = append(args, containerID)

	cmd := exec.CommandContext(r.Context(), "docker", args...)

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
		http.Error(w, "Failed to start docker logs: "+err.Error(), http.StatusInternalServerError)
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

	select {
	case <-r.Context().Done():
		cmd.Process.Kill()
	case <-done:
	}

	cmd.Wait()
}
