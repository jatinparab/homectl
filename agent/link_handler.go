package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/logger"
	"github.com/jatinparab/homectl/technitium"
)

const defaultAppLogsTailLines = "200"

func (a *Agent) handleManagerLinkRequest(msg common.LinkMessage) <-chan common.LinkMessage {
	sendJSON := func(status int, v any) <-chan common.LinkMessage {
		b, _ := json.Marshal(v)
		return staticMsgs(common.LinkMessage{Status: status, Body: b, Done: true})
	}
	errMsg := func(status int, err string) <-chan common.LinkMessage {
		return staticMsgs(common.LinkMessage{Status: status, Error: err, Done: true})
	}

	switch msg.RequestType {
	case common.LinkReqAgentManagerSync:
		if err := a.SyncConfigs(); err != nil {
			return errMsg(http.StatusInternalServerError, err.Error())
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "synced"})

	case common.LinkReqRestartApp:
		payload, err := common.DecodePayload[common.RestartAppPayload](msg)
		if err != nil || payload.AppID == "" {
			return errMsg(http.StatusBadRequest, "invalid restart payload")
		}
		rt := a.getRuntime(common.AppID(payload.AppID))
		if rt == nil {
			return errMsg(http.StatusNotFound, "app not found")
		}
		rt.ResetForRestart()
		if rt.RunningStatus() == common.RunningStatusRunning || rt.RunningStatus() == common.RunningStatusRestarting {
			a.executor.Submit(ActionStop(common.AppID(payload.AppID)))
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "restart_triggered"})

	case common.LinkReqSnapshots:
		payload, err := common.DecodePayload[common.SnapshotsPayload](msg)
		if err != nil || payload.AppID == "" {
			return errMsg(http.StatusBadRequest, "invalid snapshots payload")
		}
		cache := a.getSnapshotCache(common.AppID(payload.AppID))
		if cache == nil {
			a.refreshSnapshotCache(common.AppID(payload.AppID))
			cache = a.getSnapshotCache(common.AppID(payload.AppID))
		}
		if cache == nil {
			return sendJSON(http.StatusOK, SnapshotCache{Loading: true})
		}
		return sendJSON(http.StatusOK, cache)

	case common.LinkReqSnapshotRefresh:
		payload, err := common.DecodePayload[common.SnapshotRefreshPayload](msg)
		if err != nil || payload.AppID == "" {
			return errMsg(http.StatusBadRequest, "invalid snapshot refresh payload")
		}
		go a.refreshSnapshotCache(common.AppID(payload.AppID))
		return sendJSON(http.StatusOK, map[string]bool{"loading": true})

	case common.LinkReqSnapshotLs:
		payload, err := common.DecodePayload[common.SnapshotLsPayload](msg)
		if err != nil || payload.SnapshotID == "" {
			return errMsg(http.StatusBadRequest, "invalid snapshot ls payload")
		}
		browsePath := payload.Path
		if browsePath == "" {
			browsePath = "/"
		}
		entries, err := a.restic.ListSnapshotFiles(context.Background(), payload.SnapshotID, browsePath)
		if err != nil {
			return errMsg(http.StatusInternalServerError, err.Error())
		}
		return sendJSON(http.StatusOK, entries)

	case common.LinkReqContainers:
		containers, err := listContainers()
		if err != nil {
			return errMsg(http.StatusInternalServerError, err.Error())
		}
		return sendJSON(http.StatusOK, containers)

	case common.LinkReqStorageStatus:
		return sendJSON(http.StatusOK, a.getStorageStatus())

	case common.LinkReqStoragePrune:
		if !a.startStoragePrune() {
			return errMsg(http.StatusConflict, "prune already running")
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "prune_started"})

	case common.LinkReqRepoCheck:
		if !a.IsGateway() {
			return errMsg(http.StatusServiceUnavailable, "not gateway")
		}
		if err := a.TriggerRepoCheck(); err != nil {
			return errMsg(http.StatusConflict, err.Error())
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "check_triggered"})

	case common.LinkReqRepoUnlock:
		if !a.IsGateway() {
			return errMsg(http.StatusServiceUnavailable, "not gateway")
		}
		if err := a.TriggerRepoUnlock(); err != nil {
			return errMsg(http.StatusConflict, err.Error())
		}
		return sendJSON(http.StatusOK, map[string]string{"status": "unlock_triggered"})

	case common.LinkReqRepoForceUnlock:
		if !a.IsGateway() {
			return errMsg(http.StatusServiceUnavailable, "not gateway")
		}
		if err := a.TriggerRepoForceUnlock(); err != nil {
			return errMsg(http.StatusConflict, err.Error())
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
			return errMsg(http.StatusBadRequest, "invalid phase logs payload")
		}
		rt := a.getRuntime(common.AppID(payload.AppID))
		if rt == nil || rt.PhaseLog == nil {
			return errMsg(http.StatusNotFound, "app not found")
		}
		return streamFromBuffer(rt.PhaseLog)

	case common.LinkReqAppLogs:
		payload, err := common.DecodePayload[common.AppLogsPayload](msg)
		if err != nil || payload.AppID == "" {
			return errMsg(http.StatusBadRequest, "invalid app logs payload")
		}
		var cmd *exec.Cmd
		if common.AppID(payload.AppID) == common.DNSAppID {
			cmd = exec.Command("docker", "logs", "--tail="+defaultAppLogsTailLines, technitium.ContainerName)
		} else {
			cmd = a.docker.ComposeCommand(payload.AppID, "logs", "--tail="+defaultAppLogsTailLines, "--no-log-prefix=false")
		}
		out, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil && len(strings.TrimSpace(string(out))) == 0 {
			return errMsg(http.StatusInternalServerError, fmt.Sprintf("%s: %s", cmdErr, strings.TrimSpace(string(out))))
		}
		return staticMsgs(common.LinkMessage{Status: http.StatusOK, Body: out, Done: true})

	case common.LinkReqSystemLogs:
		lines := logger.Buffer.Snapshot()
		body := []byte(strings.Join(lines, "\n"))
		if len(body) > 0 {
			body = append(body, '\n')
		}
		return staticMsgs(common.LinkMessage{Status: http.StatusOK, Body: body, Done: true})

	case common.LinkReqContainerLogs:
		payload, err := common.DecodePayload[common.ContainerLogsPayload](msg)
		if err != nil || payload.ContainerID == "" {
			return errMsg(http.StatusBadRequest, "invalid container logs payload")
		}
		args := []string{"logs"}
		if !payload.ShowAll {
			args = append(args, "--tail=100")
		}
		args = append(args, payload.ContainerID)
		cmd := exec.Command("docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errMsg(http.StatusInternalServerError, fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(out))))
		}
		return staticMsgs(common.LinkMessage{Status: http.StatusOK, Body: out, Done: true})

	default:
		return errMsg(http.StatusNotFound, "unknown request type")
	}
}

// staticMsgs wraps one or more messages into a closed channel for non-streaming responses.
func staticMsgs(msgs ...common.LinkMessage) <-chan common.LinkMessage {
	ch := make(chan common.LinkMessage, len(msgs))
	for _, m := range msgs {
		ch <- m
	}
	close(ch)
	return ch
}

// streamFromBuffer sends a snapshot of the buffer then live-follows until the phase ends.
// The phase ends when Clear() is called (which closes all follower channels).
func streamFromBuffer(buf *PhaseLogBuffer) <-chan common.LinkMessage {
	ch := make(chan common.LinkMessage, 32)
	go func() {
		defer close(ch)
		if buf == nil {
			ch <- common.LinkMessage{Status: http.StatusServiceUnavailable, Error: "log buffer unavailable", Done: true}
			return
		}
		// Send initial snapshot
		lines := buf.Snapshot()
		body := []byte{}
		if len(lines) > 0 {
			body = []byte(strings.Join(lines, "\n") + "\n")
		}
		ch <- common.LinkMessage{Status: http.StatusOK, Body: body}

		// Follow live until the phase ends (Clear closes the follower channel)
		follow := buf.Follow()
		for line := range follow {
			ch <- common.LinkMessage{Status: http.StatusOK, Body: []byte(line + "\n")}
		}
		ch <- common.LinkMessage{Status: http.StatusOK, Done: true}
	}()
	return ch
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
