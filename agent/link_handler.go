package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

func (a *Agent) handleManagerLinkRequest(msg common.LinkMessage) []common.LinkMessage {
	path := msg.Path
	if path == "" {
		return []common.LinkMessage{{Status: http.StatusBadRequest, Error: "missing path", Done: true}}
	}

	u, _ := url.Parse(path)
	cleanPath := u.Path
	q := u.Query()

	sendJSON := func(status int, v any) []common.LinkMessage {
		b, _ := json.Marshal(v)
		return []common.LinkMessage{{Status: status, Body: b, Done: true}}
	}

	switch {
	case cleanPath == "/sync":
		if err := a.SyncConfigs(); err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "synced"})

	case strings.HasPrefix(cleanPath, "/restart/"):
		appID := strings.TrimPrefix(cleanPath, "/restart/")
		rt := a.getRuntime(common.AppID(appID))
		if rt == nil {
			return []common.LinkMessage{{Status: http.StatusNotFound, Error: "app not found", Done: true}}
		}
		rt.ResetForRestart()
		if rt.RunningStatus() == common.RunningStatusRunning || rt.RunningStatus() == common.RunningStatusRestarting {
			a.executor.Submit(ActionStop(common.AppID(appID)))
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "restart_triggered"})

	case strings.HasPrefix(cleanPath, "/snapshots/"):
		pathRest := strings.TrimPrefix(cleanPath, "/snapshots/")
		if strings.HasSuffix(pathRest, "/refresh") {
			appID := strings.TrimSuffix(pathRest, "/refresh")
			go a.refreshSnapshotCache(common.AppID(appID))
			return sendJSON(http.StatusOK, map[string]bool{"loading": true})
		}
		appID := pathRest
		cache := a.getSnapshotCache(common.AppID(appID))
		if cache == nil {
			a.refreshSnapshotCache(common.AppID(appID))
			cache = a.getSnapshotCache(common.AppID(appID))
		}
		if cache == nil {
			return sendJSON(http.StatusOK, SnapshotCache{Loading: true})
		}
		return sendJSON(http.StatusOK, cache)

	case strings.HasPrefix(cleanPath, "/snapshot-ls/"):
		snapshotID := strings.TrimPrefix(cleanPath, "/snapshot-ls/")
		browsePath := q.Get("path")
		if browsePath == "" {
			browsePath = "/"
		}
		entries, err := a.restic.ListSnapshotFiles(context.Background(), snapshotID, browsePath)
		if err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, entries)

	case cleanPath == "/containers":
		containers, err := listContainers()
		if err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, containers)

	case cleanPath == "/storage/status":
		return sendJSON(http.StatusOK, a.getStorageStatus())

	case cleanPath == "/storage/prune":
		if !a.startStoragePrune() {
			return []common.LinkMessage{{Status: http.StatusConflict, Error: "prune already running", Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "prune_started"})

	case cleanPath == "/check":
		if !a.IsGateway() {
			return []common.LinkMessage{{Status: http.StatusServiceUnavailable, Error: "not gateway", Done: true}}
		}
		if err := a.TriggerRepoCheck(); err != nil {
			return []common.LinkMessage{{Status: http.StatusConflict, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "check_triggered"})

	case cleanPath == "/unlock":
		if !a.IsGateway() {
			return []common.LinkMessage{{Status: http.StatusServiceUnavailable, Error: "not gateway", Done: true}}
		}
		if err := a.TriggerRepoUnlock(); err != nil {
			return []common.LinkMessage{{Status: http.StatusConflict, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "unlock_triggered"})

	case cleanPath == "/force-unlock":
		if !a.IsGateway() {
			return []common.LinkMessage{{Status: http.StatusServiceUnavailable, Error: "not gateway", Done: true}}
		}
		if err := a.TriggerRepoForceUnlock(); err != nil {
			return []common.LinkMessage{{Status: http.StatusConflict, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "force_unlock_triggered"})

	case cleanPath == "/check-status":
		return sendJSON(http.StatusOK, a.RepoCheckStatus())
	case cleanPath == "/unlock-status":
		return sendJSON(http.StatusOK, a.RepoUnlockStatus())

	case cleanPath == "/check-logs":
		return streamFromBuffer(a.repoCheck.Log)
	case cleanPath == "/unlock-logs":
		return streamFromBuffer(a.repoUnlock.Log)

	case strings.HasPrefix(cleanPath, "/phase-logs/"):
		appID := strings.TrimPrefix(cleanPath, "/phase-logs/")
		rt := a.getRuntime(common.AppID(appID))
		if rt == nil || rt.PhaseLog == nil {
			return []common.LinkMessage{{Status: http.StatusNotFound, Error: "app not found", Done: true}}
		}
		return streamFromBuffer(rt.PhaseLog)

	case strings.HasPrefix(cleanPath, "/logs/"):
		appID := strings.TrimPrefix(cleanPath, "/logs/")
		composePath := filepath.Join(a.dockerRootDir, appID, "docker-compose.yml")
		cmd := exec.Command("docker", "compose", "-f", composePath, "logs", "--tail=100")
		cmd.Env = a.docker.ComposeEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(out))), Done: true}}
		}
		return []common.LinkMessage{{Status: http.StatusOK, Body: out, Done: true}}

	case cleanPath == "/system-logs":
		cmd := exec.Command("journalctl", "-u", "homectl-agent", "--output=short-iso", "-n", "100")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(out))), Done: true}}
		}
		return []common.LinkMessage{{Status: http.StatusOK, Body: out, Done: true}}

	case strings.HasPrefix(cleanPath, "/container-logs/"):
		containerID := strings.TrimPrefix(cleanPath, "/container-logs/")
		showAll := q.Get("all") == "true"
		args := []string{"logs"}
		if !showAll {
			args = append(args, "--tail=100")
		}
		args = append(args, containerID)
		cmd := exec.Command("docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(out))), Done: true}}
		}
		return []common.LinkMessage{{Status: http.StatusOK, Body: out, Done: true}}

	default:
		return []common.LinkMessage{{Status: http.StatusNotFound, Error: "unknown path", Done: true}}
	}
}

func streamFromBuffer(buf *PhaseLogBuffer) []common.LinkMessage {
	if buf == nil {
		return []common.LinkMessage{{Status: http.StatusServiceUnavailable, Error: "log buffer unavailable", Done: true}}
	}
	lines := buf.Snapshot()
	if len(lines) == 0 {
		return []common.LinkMessage{{Status: http.StatusOK, Body: []byte{}, Done: true}}
	}
	return []common.LinkMessage{{Status: http.StatusOK, Body: []byte(strings.Join(lines, "\n") + "\n"), Done: true}}
}

func listContainers() ([]ContainerInfo, error) {
	cmd := exec.Command("docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
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
	if containers == nil {
		containers = []ContainerInfo{}
	}
	return containers, nil
}

var _ = time.Second
