package agent

import (
	"fmt"
	"log"
	"net/http"

	"github.com/jatinparab/homectl/common"
)

const (
	// ReadyServerPort is the port for the ready server
	ReadyServerPort = 1337
	// TraefikAppName is the app name to check for readiness
	TraefikAppName = "traefik"
)

// ReadyServer provides health check endpoints for keepalived
type ReadyServer struct {
	agent  *Agent
	server *http.Server
}

// NewReadyServer creates a new ready server
func NewReadyServer(agent *Agent) *ReadyServer {
	return &ReadyServer{
		agent: agent,
	}
}

// Start starts the ready server
func (rs *ReadyServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", rs.handleReady)
	mux.HandleFunc("/health", rs.handleHealth)

	// Register logs, system-logs, containers, container-logs routes
	logsHandler := NewLogsHandler(rs.agent)
	logsHandler.RegisterRoutes(mux)

	rs.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", ReadyServerPort),
		Handler: mux,
	}

	log.Printf("Ready server starting on :%d", ReadyServerPort)
	return rs.server.ListenAndServe()
}

// Stop stops the ready server
func (rs *ReadyServer) Stop() error {
	if rs.server != nil {
		return rs.server.Close()
	}
	return nil
}

// handleHealth is a basic health check for the agent ready server itself
func (rs *ReadyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok\n"))
}

// handleReady checks if traefik is running and healthy
func (rs *ReadyServer) handleReady(w http.ResponseWriter, r *http.Request) {
	isReady, message := rs.checkTraefikReady()

	if isReady {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(fmt.Sprintf("not ready: %s\n", message)))
	}
}

// checkTraefikReady checks if traefik app is running and healthy
func (rs *ReadyServer) checkTraefikReady() (bool, string) {
	if rs.agent == nil {
		return false, "agent not initialized"
	}

	rt := rs.agent.getRuntime(common.AppID(TraefikAppName))
	if rt == nil {
		return false, fmt.Sprintf("'%s' not tracked", TraefikAppName)
	}

	runningStatus := rt.RunningStatus()
	if runningStatus != common.RunningStatusRunning {
		return false, fmt.Sprintf("'%s' status: %s", TraefikAppName, runningStatus)
	}

	health := rt.GetHealth()
	if health == common.HealthUnhealthy {
		return false, fmt.Sprintf("'%s' unhealthy", TraefikAppName)
	}

	return true, "ok"
}
