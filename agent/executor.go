package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"os"
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
	// DefaultExecutorWorkers limits parallel Docker/restic actions to avoid host CPU pressure.
	DefaultExecutorWorkers = 4
	ExecutorWorkersEnv     = "HOMECTL_EXECUTOR_WORKERS"

	// BackupTimeout caps how long a single backup operation may run.
	// If restic hangs (B2 unreachable, lock conflicts), the phase is reset
	// and the agent can retry on next reconcile.
	BackupTimeout = 15 * time.Minute

	// RestoreTimeout caps how long a single restore operation may run.
	RestoreTimeout = 15 * time.Minute

	// PruneTimeout caps how long a single prune operation may run.
	PruneTimeout = 2 * time.Hour
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
	workers := executorWorkerCount()
	for i := 0; i < workers; i++ {
		e.wg.Add(1)
		go e.worker(i)
	}
	executorLog.Info("Started workers", "count", workers)
}

func executorWorkerCount() int {
	raw := os.Getenv(ExecutorWorkersEnv)
	if raw == "" {
		return DefaultExecutorWorkers
	}
	workers, err := strconv.Atoi(raw)
	if err != nil {
		executorLog.Warn("Invalid executor worker count, using default", "value", raw, "default", DefaultExecutorWorkers)
		return DefaultExecutorWorkers
	}
	if workers < 1 {
		return 1
	}
	if workers > 16 {
		return 16
	}
	return workers
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
	restoreSnapshotID := rt.GetRestoreSnapshotID()

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

	// Clear JustBackedUp flag on every start so the next restart doesn't
	// incorrectly skip restoring latest. Only use the flag to skip restore
	// when no override exists (user explicitly requested a specific snapshot).
	e.agent.runtimeMu.Lock()
	justBacked := rt.Backup.JustBackedUp
	rt.Backup.JustBackedUp = false
	e.agent.runtimeMu.Unlock()

	if justBacked && restoreSnapshotID == "" {
		executorLog.Info("Restarting after local backup, skipping restore", "app", appID)
		needsRestore = false
	}

	if needsRestore {
		meta, metaErr := loadBackupMeta(e.agent.stateDir, appID)
		if metaErr != nil {
			executorLog.Info("Failed to load backup meta (will restore)", "app", appID, "err", metaErr)
		} else if meta == nil {
			executorLog.Info("No backup meta (first start, will restore)", "app", appID)
		} else if restoreSnapshotID != "" {
			executorLog.Info("Restore snapshot override set, restoring selected snapshot", "app", appID, "snapshot", restoreSnapshotID)
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
		restoreCtx, restoreCancel := context.WithTimeout(e.ctx, RestoreTimeout)
		defer restoreCancel()
		err := e.restic.Restore(restoreCtx, appID, restoreSnapshotID, func(p *common.ResticProgress) {
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
		var snapTime time.Time
		var snapID string
		var snapErr error
		if restoreSnapshotID != "" {
			snapTime, snapID, snapErr = e.restic.Snapshot(e.ctx, appID, restoreSnapshotID)
		} else {
			snapTime, snapID, snapErr = e.restic.LatestSnapshot(e.ctx, appID)
		}
		if snapErr != nil {
			executorLog.Info("Failed to query snapshot after restore", "app", appID, "err", snapErr)
		} else if !snapTime.IsZero() {
			newMeta := &BackupMeta{
				LastBackupAt: snapTime,
				SnapshotID:   snapID,
				Node:         e.agent.name,
			}
			if err := writeBackupMeta(e.agent.stateDir, appID, newMeta); err != nil {
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
	if err := e.startApp(appID, rt); err != nil {
		return err
	}
	// The service start has succeeded. Update the
	// runtime optimistically so a sensor tick racing just behind this action
	// cannot enqueue a duplicate start and consume JustBackedUp a second time.
	markAppStarted(rt, common.PhaseRunning)

	executorLog.Info("Started", "app", appID)
	return nil
}

func (e *Executor) startApp(appID common.AppID, rt *AppRuntime) error {
	if appID == common.DNSAppID {
		ctx, cancel := context.WithTimeout(e.ctx, 5*time.Minute)
		defer cancel()
		return e.agent.technitium.Ensure(ctx, e.agent.technitiumConfig())
	}
	return e.docker.StartWithLogs(string(appID), func(line string) {
		executorLog.Debug(line, "app", appID)
		rt.PhaseLog.Write(line)
	})
}

func (e *Executor) stopApp(appID common.AppID) error {
	if appID == common.DNSAppID {
		ctx, cancel := context.WithTimeout(e.ctx, time.Minute)
		defer cancel()
		return e.agent.technitium.Stop(ctx)
	}
	return e.docker.Stop(string(appID))
}

func (e *Executor) downApp(appID common.AppID) error {
	if appID == common.DNSAppID {
		ctx, cancel := context.WithTimeout(e.ctx, time.Minute)
		defer cancel()
		return e.agent.technitium.Remove(ctx)
	}
	return e.docker.Down(string(appID))
}

// executeRestart restarts an app
func (e *Executor) executeRestart(appID common.AppID, rt *AppRuntime) error {
	// Stop first
	if err := e.stopApp(appID); err != nil {
		executorLog.Info("Stop failed during restart", "app", appID, "err", err)
		// Continue to try start anyway
	}

	// Then pull + start
	rt.SetPhase(common.PhasePulling)
	rt.PhaseLog.Clear()
	rt.IncrementRestarts()
	if err := e.startApp(appID, rt); err != nil {
		return err
	}
	markAppStarted(rt, common.PhaseEmpty)

	executorLog.Info("Restarted", "app", appID)
	return nil
}

func markAppStarted(rt *AppRuntime, phase common.Phase) {
	rt.SetRunning(common.RunningStatusRunning)
	rt.SetPhase(phase)
}

// executeStop stops an app
func (e *Executor) executeStop(appID common.AppID, rt *AppRuntime) error {
	if appID == common.DNSAppID && rt.GetPhase() == common.PhaseBackupStopping {
		if err := e.agent.prepareDNSBackup(e.ctx); err != nil {
			// Restore eligibility and postpone, rather than getting stuck in the
			// stop phase or retrying the drain on every reconcile tick.
			retryAt := time.Now().Add(5 * time.Minute)
			e.agent.dnsBackupRetryAt.Store(retryAt.UnixNano())
			e.agent.runtimeMu.Lock()
			rt.Backup.BackupDefer = retryAt
			rt.Backup.LastError = err.Error()
			e.agent.runtimeMu.Unlock()
			rt.SetPhase(common.PhaseRunning)
			return err
		}
	}
	if err := e.stopApp(appID); err != nil {
		if rt.GetPhase() == common.PhaseBackupStopping {
			rt.SetPhase(common.PhaseRunning)
		}
		return err
	}

	executorLog.Info("Stopped", "app", appID)
	return nil
}

// executeDown stops and removes compose resources for apps that are no longer expected on this node.
func (e *Executor) executeDown(appID common.AppID, rt *AppRuntime) error {
	if err := e.downApp(appID); err != nil {
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
	backupCtx, backupCancel := context.WithTimeout(e.ctx, BackupTimeout)
	defer backupCancel()
	backupErr := e.restic.Backup(backupCtx, appID, func(p *common.ResticProgress) {
		rt.SetProgress(p)
	}, func(line string) {
		rt.PhaseLog.Write(line)
	})
	e.agent.endBackup()
	rt.ClearProgress()

	// Step 3: Update backup state
	e.agent.runtimeMu.Lock()
	// Track attempt time regardless of outcome. On success, also record success time.
	// On failure we intentionally do NOT update LastBackupAt so the manager does not
	// mistake a failed attempt for a recent successful backup and skip re-scheduling.
	if backupErr != nil {
		rt.Backup.LastError = backupErr.Error()
		executorLog.Info("Backup failed", "app", appID, "err", backupErr)
		// Exponential backoff: defer next attempt by 1h, then double each time up to 24h
		if rt.Backup.BackupDefer.IsZero() {
			rt.Backup.BackupDefer = time.Now().Add(1 * time.Hour)
		} else {
			delay := rt.Backup.BackupDefer.Sub(time.Now())
			if delay > 0 && delay < 24*time.Hour {
				rt.Backup.BackupDefer = time.Now().Add(delay * 2)
			} else {
				rt.Backup.BackupDefer = time.Now().Add(24 * time.Hour)
			}
		}
	} else {
		rt.Backup.LastError = ""
		executorLog.Info("Backup completed", "app", appID)
		rt.Backup.LastBackupAt = time.Now()
		rt.Backup.BackupDefer = time.Time{} // Reset defer on success
		rt.Backup.JustBackedUp = true
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
		if err := writeBackupMeta(e.agent.stateDir, appID, meta); err != nil {
			executorLog.Info("Failed to write backup meta", "app", appID, "err", err)
		}

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
	pruneCtx, pruneCancel := context.WithTimeout(e.ctx, PruneTimeout)
	defer pruneCancel()
	pruneErr := e.restic.Prune(pruneCtx, appID, func(line string) {
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
