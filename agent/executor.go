package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/docker"
)

var executorLog = slog.With("area", "executor")

var resticTextProgressRe = regexp.MustCompile(`\[[0-9]{1,2}:[0-9]{2}\]\s+([0-9]+(?:\.[0-9]+)?)%\s+([0-9]+)\s+/\s+([0-9]+)\b`)

func parseResticTextProgress(line string) *common.ResticProgress {
	m := resticTextProgressRe.FindStringSubmatch(line)
	if len(m) < 4 {
		return nil
	}
	pct, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil
	}
	done, err := strconv.ParseUint(m[2], 10, 64)
	if err != nil {
		return nil
	}
	total, err := strconv.ParseUint(m[3], 10, 64)
	if err != nil {
		return nil
	}
	if total == 0 {
		return nil
	}
	frac := pct / 100.0
	frac = math.Max(0, math.Min(1, frac))
	return &common.ResticProgress{
		PercentDone: frac,
		TotalFiles:  total,
		FilesDone:   done,
	}
}

const (
	// ExecutorWorkers is the number of parallel workers for executing actions
	ExecutorWorkers = 12
)

// ActionType represents the type of action to execute
type ActionType int

const (
	ActionTypeNone ActionType = iota
	ActionTypeStart
	ActionTypeRestart
	ActionTypeStop
	ActionTypeDown
	ActionTypeQuarantine
	ActionTypeBackup
	ActionTypePrune
)

func (t ActionType) String() string {
	switch t {
	case ActionTypeNone:
		return "none"
	case ActionTypeStart:
		return "start"
	case ActionTypeRestart:
		return "restart"
	case ActionTypeStop:
		return "stop"
	case ActionTypeDown:
		return "down"
	case ActionTypeQuarantine:
		return "quarantine"
	case ActionTypeBackup:
		return "backup"
	case ActionTypePrune:
		return "prune"
	default:
		return "unknown"
	}
}

// Action represents an action to execute on an app
type Action struct {
	Type  ActionType
	AppID common.AppID
}

// ActionNone returns a no-op action
func ActionNone() Action {
	return Action{Type: ActionTypeNone}
}

// ActionStart returns a start action for the app
func ActionStart(appID common.AppID) Action {
	return Action{Type: ActionTypeStart, AppID: appID}
}

// ActionRestart returns a restart action for the app
func ActionRestart(appID common.AppID) Action {
	return Action{Type: ActionTypeRestart, AppID: appID}
}

// ActionStop returns a stop action for the app
func ActionStop(appID common.AppID) Action {
	return Action{Type: ActionTypeStop, AppID: appID}
}

// ActionDown returns a down action for the app.
func ActionDown(appID common.AppID) Action {
	return Action{Type: ActionTypeDown, AppID: appID}
}

// ActionQuarantine returns a quarantine action for the app
func ActionQuarantine(appID common.AppID) Action {
	return Action{Type: ActionTypeQuarantine, AppID: appID}
}

// ActionBackup returns a quarantine action for the app
func ActionBackup(appID common.AppID) Action {
	return Action{Type: ActionTypeBackup, AppID: appID}
}

// ActionPrune returns a prune action for the app
func ActionPrune(appID common.AppID) Action {
	return Action{Type: ActionTypePrune, AppID: appID}
}

// Executor executes actions on applications
type Executor struct {
	docker  *docker.Docker
	restic  Restic
	agent   *Agent
	actionQ chan Action
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewExecutor creates a new Executor
func NewExecutor(d *docker.Docker, r Restic, agent *Agent) *Executor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Executor{
		docker:  d,
		restic:  r,
		agent:   agent,
		actionQ: make(chan Action, 100),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start starts the executor workers
func (e *Executor) Start() {
	for i := 0; i < ExecutorWorkers; i++ {
		e.wg.Add(1)
		go e.worker(i)
	}
	executorLog.Info("Started workers", "count", ExecutorWorkers)
}

// Stop stops the executor
func (e *Executor) Stop() {
	e.cancel()
	close(e.actionQ)
	e.wg.Wait()
	executorLog.Info("Stopped")
}

// Submit submits an action for execution
func (e *Executor) Submit(action Action) {
	if action.Type == ActionTypeNone {
		return
	}

	// Check if executor is stopped
	select {
	case <-e.ctx.Done():
		executorLog.Info("Not accepting action (stopped)", "action", action.Type, "app", action.AppID)
		return
	default:
	}

	// Check if already in flight (queued or executing)
	rt := e.agent.getRuntime(action.AppID)
	if rt != nil && rt.IsInFlight() {
		executorLog.Info("Skipping action (already in flight)", "action", action.Type, "app", action.AppID)
		return
	}

	// Mark in-flight BEFORE enqueueing so the reconcile loop won't submit duplicates
	if rt != nil {
		rt.SetInFlight(true, action.Type.String())
	}

	select {
	case e.actionQ <- action:
		executorLog.Info("Queued action", "action", action.Type, "app", action.AppID)
	case <-e.ctx.Done():
		if rt != nil {
			rt.SetInFlight(false)
		}
		executorLog.Info("Not accepting action (stopped)", "action", action.Type, "app", action.AppID)
	default:
		if rt != nil {
			rt.SetInFlight(false)
		}
		executorLog.Info("Action queue full, dropping", "action", action.Type, "app", action.AppID)
	}
}

// worker processes actions from the queue
func (e *Executor) worker(id int) {
	defer e.wg.Done()

	for {
		select {
		case action, ok := <-e.actionQ:
			if !ok {
				return
			}
			e.execute(action)
		case <-e.ctx.Done():
			return
		}
	}
}

// execute dispatches and executes an action
func (e *Executor) execute(action Action) {
	appID := action.AppID
	rt := e.agent.getRuntime(appID)
	if rt == nil {
		executorLog.Info("No runtime for app, creating", "app", appID)
		rt = e.agent.getOrCreateRuntime(appID)
	}

	// InFlight was set at Submit time; ensure it's cleared when done
	defer rt.SetInFlight(false)

	executorLog.Info("Executing action", "action", action.Type, "app", appID)

	var err error
	switch action.Type {
	case ActionTypeStart:
		err = e.executeStart(appID, rt)
	case ActionTypeRestart:
		err = e.executeRestart(appID, rt)
	case ActionTypeStop:
		err = e.executeStop(appID, rt)
	case ActionTypeDown:
		err = e.executeDown(appID, rt)
	case ActionTypeQuarantine:
		e.executeQuarantine(appID, rt)
	case ActionTypeBackup:
		err = e.executeBackup(appID, rt)
	case ActionTypePrune:
		err = e.executePrune(appID, rt)
	}

	if err != nil {
		executorLog.Info("Action failed", "action", action.Type, "app", appID, "err", err)
		rt.SetLastError(err.Error())
	} else {
		rt.SetLastError("")
	}
}

// executeStart starts an app with smart restore: skip restore when local data is current.
func (e *Executor) executeStart(appID common.AppID, rt *AppRuntime) error {
	needsRestore := true

	// If backup is disabled for this app, skip all restore logic
	if !rt.IsBackupEnabled() {
		executorLog.Info("Backup disabled, skipping restore", "app", appID)
		needsRestore = false
	}

	// If restore is disabled (force local data), skip restore
	if needsRestore && !rt.IsRestoreEnabled() {
		executorLog.Info("Restore disabled, using local data", "app", appID)
		needsRestore = false
	}

	// Short-circuit: if we just backed up this app locally, data is already current
	if needsRestore {
		e.agent.runtimeMu.Lock()
		justBacked := rt.Backup.JustBackedUp
		rt.Backup.JustBackedUp = false
		e.agent.runtimeMu.Unlock()

		if justBacked {
			executorLog.Info("Restarting after local backup, skipping restore", "app", appID)
			needsRestore = false
		}
	}

	if needsRestore {
		meta, metaErr := loadBackupMeta(e.agent.appdataDir, appID)
		if metaErr != nil {
			executorLog.Info("Failed to load backup meta (will restore)", "app", appID, "err", metaErr)
		} else if meta == nil {
			executorLog.Info("No backup meta (first start, will restore)", "app", appID)
		} else {
			// Metadata exists — check if a newer snapshot is available
			snapTime, _, snapErr := e.restic.LatestSnapshot(e.ctx, appID)
			if snapErr != nil {
				executorLog.Info("Failed to query latest snapshot (will restore)", "app", appID, "err", snapErr)
			} else if snapTime.IsZero() {
				executorLog.Info("No snapshots, skipping restore", "app", appID)
				needsRestore = false
			} else if snapTime.After(meta.LastBackupAt) {
				executorLog.Info("Newer snapshot available, will restore", "app", appID, "snap", snapTime.Format(time.RFC3339), "local", meta.LastBackupAt.Format(time.RFC3339))
			} else {
				executorLog.Info("Local data is current, skipping restore", "app", appID)
				needsRestore = false
			}
		}
	}

	if needsRestore {
		rt.SetPhase(common.PhaseRestoring)
		rt.PhaseLog.Clear()
		err := e.restic.Restore(e.ctx, appID, func(p *common.ResticProgress) {
			rt.SetProgress(p)
		}, func(line string) {
			rt.PhaseLog.Write(line)
		})
		rt.ClearProgress()
		if err != nil {
			rt.SetPhase(common.PhaseEmpty) // Allow reconcile to retry on next cycle
			rt.IncrementRestarts()
			return err
		}

		// After restore, write metadata with the actual snapshot timestamp
		snapTime, snapID, snapErr := e.restic.LatestSnapshot(e.ctx, appID)
		if snapErr != nil {
			executorLog.Info("Failed to query snapshot after restore", "app", appID, "err", snapErr)
		} else if !snapTime.IsZero() {
			newMeta := &BackupMeta{
				LastBackupAt: snapTime,
				SnapshotID:   snapID,
				Node:         e.agent.name,
			}
			if err := writeBackupMeta(e.agent.appdataDir, appID, newMeta); err != nil {
				executorLog.Info("Failed to write backup meta after restore", "app", appID, "err", err)
			}
			e.agent.runtimeMu.Lock()
			rt.Backup.LastBackupAt = snapTime
			e.agent.runtimeMu.Unlock()
		}
	}

	// Pull images + start via docker compose
	rt.SetPhase(common.PhasePulling)
	rt.PhaseLog.Clear()
	if err := e.docker.StartWithLogs(string(appID), func(line string) {
		executorLog.Debug(line, "app", appID)
		rt.PhaseLog.Write(line)
	}); err != nil {
		return err
	}
	rt.SetPhase(common.PhaseRunning)

	executorLog.Info("Started", "app", appID)
	return nil
}

// executeRestart restarts an app
func (e *Executor) executeRestart(appID common.AppID, rt *AppRuntime) error {
	// Stop first
	if err := e.docker.Stop(string(appID)); err != nil {
		executorLog.Info("Stop failed during restart", "app", appID, "err", err)
		// Continue to try start anyway
	}

	// Then pull + start
	rt.SetPhase(common.PhasePulling)
	rt.PhaseLog.Clear()
	rt.IncrementRestarts()
	if err := e.docker.StartWithLogs(string(appID), func(line string) {
		executorLog.Debug(line, "app", appID)
		rt.PhaseLog.Write(line)
	}); err != nil {
		return err
	}
	rt.SetPhase(common.PhaseEmpty)

	executorLog.Info("Restarted", "app", appID)
	return nil
}

// executeStop stops an app
func (e *Executor) executeStop(appID common.AppID, rt *AppRuntime) error {
	if err := e.docker.Stop(string(appID)); err != nil {
		return err
	}

	executorLog.Info("Stopped", "app", appID)
	return nil
}

// executeDown stops and removes compose resources for apps that are no longer expected on this node.
func (e *Executor) executeDown(appID common.AppID, rt *AppRuntime) error {
	if err := e.docker.Down(string(appID)); err != nil {
		return err
	}
	executorLog.Info("Removed compose resources (down)", "app", appID)
	return nil
}

// executeQuarantine quarantines an app
func (e *Executor) executeQuarantine(appID common.AppID, rt *AppRuntime) {
	rt.SetQuarantined(true)
	executorLog.Info("Quarantined (too many restarts)", "app", appID)
}

// executeBackup runs the restic backup. The global lease is already held (acquired by reconcile).
// The executor does NOT restart the app — reconcile handles that on its next cycle.
func (e *Executor) executeBackup(appID common.AppID, rt *AppRuntime) error {
	// Step 1: Mark backup in progress
	rt.SetPhase(common.PhaseBackingUp)
	e.agent.runtimeMu.Lock()
	rt.Backup.InProgress = true
	e.agent.runtimeMu.Unlock()

	executorLog.Info("Starting backup", "app", appID)

	// Step 2: Run restic backup (parallel with other backups, but blocked by prune/check lane)
	rt.PhaseLog.Clear()
	e.agent.beginBackup()
	backupErr := e.restic.Backup(e.ctx, appID, func(p *common.ResticProgress) {
		rt.SetProgress(p)
	}, func(line string) {
		rt.PhaseLog.Write(line)
	})
	e.agent.endBackup()
	rt.ClearProgress()

	// Step 3: Update backup state
	e.agent.runtimeMu.Lock()
	rt.Backup.LastBackupAt = time.Now() // Always update to prevent immediate retry on failure
	if backupErr != nil {
		rt.Backup.LastError = backupErr.Error()
		executorLog.Info("Backup failed", "app", appID, "err", backupErr)
	} else {
		rt.Backup.LastError = ""
		executorLog.Info("Backup completed", "app", appID)
	}
	rt.Backup.InProgress = false
	e.agent.runtimeMu.Unlock()

	// Step 3b: On success, persist metadata to disk using system time
	if backupErr == nil {
		now := time.Now()
		meta := &BackupMeta{
			LastBackupAt: now,
			Node:         e.agent.name,
		}
		if err := writeBackupMeta(e.agent.appdataDir, appID, meta); err != nil {
			executorLog.Info("Failed to write backup meta", "app", appID, "err", err)
		}
		e.agent.runtimeMu.Lock()
		rt.Backup.LastBackupAt = now
		rt.Backup.JustBackedUp = true
		e.agent.runtimeMu.Unlock()

		// Bust snapshot cache so next request sees the new snapshot
		go e.agent.refreshSnapshotCache(appID)

	}

	// Step 4: Reset phase — reconcile will restart the app
	rt.SetPhase(common.PhaseEmpty)

	return backupErr
}

// executePrune runs repository prune for an app.
// Serialized using backupMu as prune/repo lock cannot overlap with backup/restore/check.
func (e *Executor) executePrune(appID common.AppID, rt *AppRuntime) error {
	e.agent.beginExclusiveBackupLane()
	defer e.agent.endExclusiveBackupLane()

	rt.SetPostBackupPhase("backup_pruning")
	rt.PhaseLog.Clear()
	rt.SetProgress(&common.ResticProgress{PercentDone: 0})
	e.agent.runtimeMu.Lock()
	rt.Prune.InProgress = true
	e.agent.runtimeMu.Unlock()

	executorLog.Info("Starting prune", "app", appID)
	pruneErr := e.restic.Prune(e.ctx, appID, func(line string) {
		if p := parseResticTextProgress(line); p != nil {
			rt.SetProgress(p)
			if b, err := json.Marshal(p); err == nil {
				rt.PhaseLog.Write("__PROGRESS__:" + string(b))
			}
		}
		rt.PhaseLog.Write(line)
	})
	rt.ClearProgress()
	rt.SetPostBackupPhase("")

	now := time.Now()
	e.agent.runtimeMu.Lock()
	rt.Prune.InProgress = false
	rt.Prune.LastPruneAt = now
	if pruneErr != nil {
		rt.Prune.LastError = pruneErr.Error()
	} else {
		rt.Prune.LastError = ""
	}
	e.agent.runtimeMu.Unlock()

	if pruneErr != nil {
		executorLog.Info("Prune failed", "app", appID, "err", pruneErr)
		return pruneErr
	}
	executorLog.Info("Prune completed", "app", appID)
	return nil
}
