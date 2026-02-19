package agent

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/docker"
)

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
	ActionTypeQuarantine
	ActionTypeBackup
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
	case ActionTypeQuarantine:
		return "quarantine"
	case ActionTypeBackup:
		return "backup"
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

// ActionQuarantine returns a quarantine action for the app
func ActionQuarantine(appID common.AppID) Action {
	return Action{Type: ActionTypeQuarantine, AppID: appID}
}

// ActionBackup returns a quarantine action for the app
func ActionBackup(appID common.AppID) Action {
	return Action{Type: ActionTypeBackup, AppID: appID}
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
	log.Printf("Executor: started %d workers", ExecutorWorkers)
}

// Stop stops the executor
func (e *Executor) Stop() {
	e.cancel()
	close(e.actionQ)
	e.wg.Wait()
	log.Println("Executor: stopped")
}

// Submit submits an action for execution
func (e *Executor) Submit(action Action) {
	if action.Type == ActionTypeNone {
		return
	}

	// Check if executor is stopped
	select {
	case <-e.ctx.Done():
		log.Printf("Executor: not accepting %s for %s (stopped)", action.Type, action.AppID)
		return
	default:
	}

	// Check if already in flight (queued or executing)
	rt := e.agent.getRuntime(action.AppID)
	if rt != nil && rt.IsInFlight() {
		log.Printf("Executor: skipping %s for %s (already in flight)", action.Type, action.AppID)
		return
	}

	// Mark in-flight BEFORE enqueueing so the reconcile loop won't submit duplicates
	if rt != nil {
		rt.SetInFlight(true, action.Type.String())
	}

	select {
	case e.actionQ <- action:
		log.Printf("Executor: queued %s for %s", action.Type, action.AppID)
	case <-e.ctx.Done():
		if rt != nil {
			rt.SetInFlight(false)
		}
		log.Printf("Executor: not accepting %s for %s (stopped)", action.Type, action.AppID)
	default:
		if rt != nil {
			rt.SetInFlight(false)
		}
		log.Printf("Executor: action queue full, dropping %s for %s", action.Type, action.AppID)
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
		log.Printf("Executor: no runtime for %s, creating", appID)
		rt = e.agent.getOrCreateRuntime(appID)
	}

	// InFlight was set at Submit time; ensure it's cleared when done
	defer rt.SetInFlight(false)

	log.Printf("Executor: executing %s for %s", action.Type, appID)

	var err error
	switch action.Type {
	case ActionTypeStart:
		err = e.executeStart(appID, rt)
	case ActionTypeRestart:
		err = e.executeRestart(appID, rt)
	case ActionTypeStop:
		err = e.executeStop(appID, rt)
	case ActionTypeQuarantine:
		e.executeQuarantine(appID, rt)
	case ActionTypeBackup:
		err = e.executeBackup(appID, rt)
	}

	if err != nil {
		log.Printf("Executor: %s failed for %s: %s", action.Type, appID, err)
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
		log.Printf("Executor: %s backup disabled, skipping restore", appID)
		needsRestore = false
	}

	// If restore is disabled (force local data), skip restore
	if needsRestore && !rt.IsRestoreEnabled() {
		log.Printf("Executor: %s restore disabled, using local data", appID)
		needsRestore = false
	}

	// Short-circuit: if we just backed up this app locally, data is already current
	if needsRestore {
		e.agent.runtimeMu.Lock()
		justBacked := rt.Backup.JustBackedUp
		rt.Backup.JustBackedUp = false
		e.agent.runtimeMu.Unlock()

		if justBacked {
			log.Printf("Executor: %s restarting after local backup, skipping restore", appID)
			needsRestore = false
		}
	}

	if needsRestore {
		meta, metaErr := loadBackupMeta(e.agent.appdataDir, appID)
		if metaErr != nil {
			log.Printf("Executor: failed to load backup meta for %s: %s (will restore)", appID, metaErr)
		} else if meta == nil {
			log.Printf("Executor: no backup meta for %s (first start, will restore)", appID)
		} else {
			// Metadata exists — check if a newer snapshot is available
			snapTime, _, snapErr := e.restic.LatestSnapshot(e.ctx, appID)
			if snapErr != nil {
				log.Printf("Executor: failed to query latest snapshot for %s: %s (will restore)", appID, snapErr)
			} else if snapTime.IsZero() {
				log.Printf("Executor: no snapshots for %s, skipping restore", appID)
				needsRestore = false
			} else if snapTime.After(meta.LastBackupAt) {
				log.Printf("Executor: newer snapshot for %s (snap=%s, local=%s), will restore",
					appID, snapTime.Format(time.RFC3339), meta.LastBackupAt.Format(time.RFC3339))
			} else {
				log.Printf("Executor: local data for %s is current, skipping restore", appID)
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
			return err
		}

		// After restore, write metadata with the actual snapshot timestamp
		snapTime, snapID, snapErr := e.restic.LatestSnapshot(e.ctx, appID)
		if snapErr != nil {
			log.Printf("Executor: failed to query snapshot after restore for %s: %s", appID, snapErr)
		} else if !snapTime.IsZero() {
			newMeta := &BackupMeta{
				LastBackupAt: snapTime,
				SnapshotID:   snapID,
				Node:         e.agent.name,
			}
			if err := writeBackupMeta(e.agent.appdataDir, appID, newMeta); err != nil {
				log.Printf("Executor: failed to write backup meta after restore for %s: %s", appID, err)
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
		log.Printf("[docker:%s] %s", appID, line)
		rt.PhaseLog.Write(line)
	}); err != nil {
		return err
	}
	rt.SetPhase(common.PhaseRunning)

	log.Printf("Executor: started %s", appID)
	return nil
}

// executeRestart restarts an app
func (e *Executor) executeRestart(appID common.AppID, rt *AppRuntime) error {
	// Stop first
	if err := e.docker.Stop(string(appID)); err != nil {
		log.Printf("Executor: stop failed during restart for %s: %s", appID, err)
		// Continue to try start anyway
	}

	// Then pull + start
	rt.SetPhase(common.PhasePulling)
	rt.PhaseLog.Clear()
	rt.IncrementRestarts()
	if err := e.docker.StartWithLogs(string(appID), func(line string) {
		log.Printf("[docker:%s] %s", appID, line)
		rt.PhaseLog.Write(line)
	}); err != nil {
		return err
	}
	rt.SetPhase(common.PhaseEmpty)

	log.Printf("Executor: restarted %s", appID)
	return nil
}

// executeStop stops an app
func (e *Executor) executeStop(appID common.AppID, rt *AppRuntime) error {
	if err := e.docker.Stop(string(appID)); err != nil {
		return err
	}

	log.Printf("Executor: stopped %s", appID)
	return nil
}

// executeQuarantine quarantines an app
func (e *Executor) executeQuarantine(appID common.AppID, rt *AppRuntime) {
	rt.SetQuarantined(true)
	log.Printf("Executor: QUARANTINED %s (too many restarts)", appID)
}

// executeBackup runs the restic backup. The global lease is already held (acquired by reconcile).
// The executor does NOT restart the app — reconcile handles that on its next cycle.
func (e *Executor) executeBackup(appID common.AppID, rt *AppRuntime) error {
	// Step 1: Mark backup in progress
	rt.SetPhase(common.PhaseBackingUp)
	e.agent.runtimeMu.Lock()
	rt.Backup.InProgress = true
	e.agent.runtimeMu.Unlock()

	log.Printf("Executor: starting backup for %s", appID)

	// Step 2: Run restic backup (serialized via backupMu)
	rt.PhaseLog.Clear()
	e.agent.backupMu.Lock()
	backupErr := e.restic.Backup(e.ctx, appID, func(p *common.ResticProgress) {
		rt.SetProgress(p)
	}, func(line string) {
		rt.PhaseLog.Write(line)
	})
	e.agent.backupMu.Unlock()
	rt.ClearProgress()

	// Step 3: Update backup state (keep InProgress=true during post-backup steps)
	e.agent.runtimeMu.Lock()
	rt.Backup.LastBackupAt = time.Now() // Always update to prevent immediate retry on failure
	if backupErr != nil {
		rt.Backup.LastError = backupErr.Error()
		log.Printf("Executor: backup failed for %s: %s", appID, backupErr)
	} else {
		rt.Backup.LastError = ""
		log.Printf("Executor: backup completed for %s", appID)
	}
	// Don't clear InProgress yet — it stays true until post-backup steps finish
	e.agent.runtimeMu.Unlock()

	// Step 3b: On success, persist metadata to disk using system time
	if backupErr == nil {
		now := time.Now()
		meta := &BackupMeta{
			LastBackupAt: now,
			Node:         e.agent.name,
		}
		if err := writeBackupMeta(e.agent.appdataDir, appID, meta); err != nil {
			log.Printf("Executor: failed to write backup meta for %s: %s", appID, err)
		}
		e.agent.runtimeMu.Lock()
		rt.Backup.LastBackupAt = now
		rt.Backup.JustBackedUp = true
		e.agent.runtimeMu.Unlock()

		// Bust snapshot cache so next request sees the new snapshot
		go e.agent.refreshSnapshotCache(appID)

		// Run prune + check in the background (keeps BackupBusy=true in heartbeats)
		go e.runPostBackupSteps(appID, rt)
	} else {
		// Backup failed — clear InProgress immediately
		e.agent.runtimeMu.Lock()
		rt.Backup.InProgress = false
		e.agent.runtimeMu.Unlock()
	}

	// Step 4: Reset phase — reconcile will restart the app
	rt.SetPhase(common.PhaseEmpty)

	return backupErr
}

// runPostBackupSteps runs post-backup repo maintenance (currently: restic forget --prune).
// Operations are serialized via backupMu.
// PostBackupPhase is used instead of rt.Phase to avoid disrupting the app lifecycle.
// Clears BackupBusy when complete so the manager knows all post-backup work is done.
func (e *Executor) runPostBackupSteps(appID common.AppID, rt *AppRuntime) {
	e.agent.backupMu.Lock()
	defer e.agent.backupMu.Unlock()

	// --- Prune ---
	rt.SetPostBackupPhase("backup_pruning")
	rt.PhaseLog.Clear()
	log.Printf("Executor: starting prune for %s", appID)
	pruneErr := e.restic.Prune(e.ctx, appID, func(line string) {
		rt.PhaseLog.Write(line)
	})

	if pruneErr != nil {
		log.Printf("Executor: prune failed for %s: %s", appID, pruneErr)
	} else {
		log.Printf("Executor: prune completed for %s", appID)
	}

	// Post-backup work is complete — clear the BackupBusy flag
	// so the manager knows it can assign the next backup
	e.agent.runtimeMu.Lock()
	rt.Backup.InProgress = false
	e.agent.runtimeMu.Unlock()

	rt.SetPostBackupPhase("")
	log.Printf("Executor: post-backup steps completed for %s", appID)
}
