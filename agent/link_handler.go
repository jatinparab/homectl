package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/logger"
)

func (a *Agent) handleManagerLinkRequest(msg common.LinkMessage) []common.LinkMessage {
	sendJSON := func(status int, v any) []common.LinkMessage {
		b, _ := json.Marshal(v)
		return []common.LinkMessage{{Status: status, Body: b, Done: true}}
	}

	switch msg.RequestType {
	case common.LinkReqAgentManagerSync:
		if err := a.SyncConfigs(); err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "synced"})

	case common.LinkReqRestartApp:
		payload, err := common.DecodePayload[common.RestartAppPayload](msg)
		if err != nil || payload.AppID == "" {
			return []common.LinkMessage{{Status: http.StatusBadRequest, Error: "invalid restart payload", Done: true}}
		}
		appID := payload.AppID
		rt := a.getRuntime(common.AppID(appID))
		if rt == nil {
			return []common.LinkMessage{{Status: http.StatusNotFound, Error: "app not found", Done: true}}
		}
		rt.ResetForRestart()
		if rt.RunningStatus() == common.RunningStatusRunning || rt.RunningStatus() == common.RunningStatusRestarting {
			a.executor.Submit(ActionStop(common.AppID(appID)))
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "restart_triggered"})

	case common.LinkReqSnapshots:
		payload, err := common.DecodePayload[common.SnapshotsPayload](msg)
		if err != nil || payload.AppID == "" {
			return []common.LinkMessage{{Status: http.StatusBadRequest, Error: "invalid snapshots payload", Done: true}}
		}
		appID := payload.AppID
		cache := a.getSnapshotCache(common.AppID(appID))
		if cache == nil {
			a.refreshSnapshotCache(common.AppID(appID))
			cache = a.getSnapshotCache(common.AppID(appID))
		}
		if cache == nil {
			return sendJSON(http.StatusOK, SnapshotCache{Loading: true})
		}
		return sendJSON(http.StatusOK, cache)

	case common.LinkReqSnapshotRefresh:
		payload, err := common.DecodePayload[common.SnapshotRefreshPayload](msg)
		if err != nil || payload.AppID == "" {
			return []common.LinkMessage{{Status: http.StatusBadRequest, Error: "invalid snapshot refresh payload", Done: true}}
		}
		go a.refreshSnapshotCache(common.AppID(payload.AppID))
		return sendJSON(http.StatusOK, map[string]bool{"loading": true})

	case common.LinkReqSnapshotLs:
		payload, err := common.DecodePayload[common.SnapshotLsPayload](msg)
		if err != nil || payload.SnapshotID == "" {
			return []common.LinkMessage{{Status: http.StatusBadRequest, Error: "invalid snapshot ls payload", Done: true}}
		}
		snapshotID := payload.SnapshotID
		browsePath := payload.Path
		if browsePath == "" {
			browsePath = "/"
		}
		entries, err := a.restic.ListSnapshotFiles(context.Background(), snapshotID, browsePath)
		if err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, entries)

	case common.LinkReqContainers:
		containers, err := listContainers()
		if err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, containers)

	case common.LinkReqStorageStatus:
		return sendJSON(http.StatusOK, a.getStorageStatus())

	case common.LinkReqStoragePrune:
		if !a.startStoragePrune() {
			return []common.LinkMessage{{Status: http.StatusConflict, Error: "prune already running", Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "prune_started"})

	case common.LinkReqRepoCheck:
		if !a.IsGateway() {
			return []common.LinkMessage{{Status: http.StatusServiceUnavailable, Error: "not gateway", Done: true}}
		}
		if err := a.TriggerRepoCheck(); err != nil {
			return []common.LinkMessage{{Status: http.StatusConflict, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "check_triggered"})

	case common.LinkReqRepoUnlock:
		if !a.IsGateway() {
			return []common.LinkMessage{{Status: http.StatusServiceUnavailable, Error: "not gateway", Done: true}}
		}
		if err := a.TriggerRepoUnlock(); err != nil {
			return []common.LinkMessage{{Status: http.StatusConflict, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "unlock_triggered"})

	case common.LinkReqRepoForceUnlock:
		if !a.IsGateway() {
			return []common.LinkMessage{{Status: http.StatusServiceUnavailable, Error: "not gateway", Done: true}}
		}
		if err := a.TriggerRepoForceUnlock(); err != nil {
			return []common.LinkMessage{{Status: http.StatusConflict, Error: err.Error(), Done: true}}
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "force_unlock_triggered"})

	case common.LinkReqRepoCheckStatus:
		return sendJSON(http.StatusOK, a.RepoCheckStatus())
	case common.LinkReqRepoUnlockStatus:
		return sendJSON(http.StatusOK, a.RepoUnlockStatus())

	case common.LinkReqRepoCheckLogs:
		return streamFromBuffer(a.repoCheck.Log)
	case common.LinkReqRepoUnlockLogs:
		return streamFromBuffer(a.repoUnlock.Log)

	case common.LinkReqAppPhaseLogs:
		payload, err := common.DecodePayload[common.AppPhaseLogsPayload](msg)
		if err != nil || payload.AppID == "" {
			return []common.LinkMessage{{Status: http.StatusBadRequest, Error: "invalid phase logs payload", Done: true}}
		}
		appID := payload.AppID
		rt := a.getRuntime(common.AppID(appID))
		if rt == nil || rt.PhaseLog == nil {
			return []common.LinkMessage{{Status: http.StatusNotFound, Error: "app not found", Done: true}}
		}
		return streamFromBuffer(rt.PhaseLog)

	case common.LinkReqAppLogs:
		payload, err := common.DecodePayload[common.AppLogsPayload](msg)
		if err != nil || payload.AppID == "" {
			return []common.LinkMessage{{Status: http.StatusBadRequest, Error: "invalid app logs payload", Done: true}}
		}
		appID := payload.AppID
		composePath := filepath.Join(a.dockerRootDir, appID, "docker-compose.yml")
		cmd := exec.Command("docker", "compose", "-f", composePath, "logs", "--tail=100")
		cmd.Env = a.docker.ComposeEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return []common.LinkMessage{{Status: http.StatusInternalServerError, Error: fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(out))), Done: true}}
		}
		return []common.LinkMessage{{Status: http.StatusOK, Body: out, Done: true}}

	case common.LinkReqSystemLogs:
		lines := logger.Buffer.Snapshot()
		body := []byte(strings.Join(lines, "\n"))
		if len(body) > 0 {
			body = append(body, '\n')
		}
		return []common.LinkMessage{{Status: http.StatusOK, Body: body, Done: true}}

	case common.LinkReqContainerLogs:
		payload, err := common.DecodePayload[common.ContainerLogsPayload](msg)
		if err != nil || payload.ContainerID == "" {
			return []common.LinkMessage{{Status: http.StatusBadRequest, Error: "invalid container logs payload", Done: true}}
		}
		containerID := payload.ContainerID
		showAll := payload.ShowAll
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
		return []common.LinkMessage{{Status: http.StatusNotFound, Error: "unknown request type", Done: true}}
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
