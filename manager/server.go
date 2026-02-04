package manager

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// NewServer creates and returns an HTTP server (for graceful shutdown support)
func (m *Manager) NewServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", m.handleHealth)
	mux.HandleFunc("/files/", m.handleFiles)
	mux.HandleFunc("/sync", m.handleSync)
	mux.HandleFunc("/agents", m.handleListAgents)
	mux.HandleFunc("/agents/heartbeat", m.handleHeartbeat)

	return &http.Server{
		Addr:    fmt.Sprintf(":%d", m.port),
		Handler: mux,
	}
}

// Start starts the HTTP server (blocking)
func (m *Manager) Start() error {
	server := m.NewServer()
	log.Printf("Manager server starting on %s", server.Addr)
	log.Printf("Serving files from: %s", m.repoDir)
	return server.ListenAndServe()
}

func (m *Manager) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (m *Manager) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := m.leaseManager.ListAgents()
	w.Header().Set("Content-Type", "application/json")

	// Simple JSON encoding
	w.Write([]byte("["))
	for i, agent := range agents {
		if i > 0 {
			w.Write([]byte(","))
		}
		fmt.Fprintf(w, `{"name":%q,"ip":%q,"last_seen":%q,"registered_at":%q}`,
			agent.Name, agent.IP, agent.LastSeen.Format("2006-01-02T15:04:05Z07:00"), agent.RegisteredAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	w.Write([]byte("]"))
}

func (m *Manager) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := m.GitSync(); err != nil {
		http.Error(w, fmt.Sprintf("Sync failed: %s", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Sync completed"))
}

func (m *Manager) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/files/")
	if path == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	// Prevent directory traversal attacks
	cleanPath := filepath.Clean(path)
	if strings.HasPrefix(cleanPath, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(m.repoDir, cleanPath)

	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Error accessing file", http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		m.serveDirectory(w, fullPath, cleanPath)
		return
	}

	m.serveFile(w, fullPath)
}

func (m *Manager) serveFile(w http.ResponseWriter, fullPath string) {
	file, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, file)
}

func (m *Manager) serveDirectory(w http.ResponseWriter, fullPath, relativePath string) {
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, "Error reading directory", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	for _, entry := range entries {
		entryPath := filepath.Join(relativePath, entry.Name())
		if entry.IsDir() {
			fmt.Fprintf(w, "%s/\n", entryPath)
		} else {
			fmt.Fprintf(w, "%s\n", entryPath)
		}
	}
}
