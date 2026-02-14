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
	ExecutorWorkers = 4
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
		rt.SetInFlight(true)
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

// executeStart starts an app (with restic restore first)
func (e *Executor) executeStart(appID common.AppID, rt *AppRuntime) error {
	// First, restore from restic with progress tracking
	rt.SetPhase(common.PhaseRestoring)
	err := e.restic.Restore(appID, func(p *common.ResticProgress) {
		rt.SetProgress(p)
	})
	rt.ClearProgress()
	if err != nil {
		return err
	}

	// Then start via docker compose
	rt.SetPhase(common.PhaseEmpty)
	rt.IncrementRestarts()
	if err := e.docker.Start(string(appID)); err != nil {
		return err
	}

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

	// Then start
	rt.IncrementRestarts()
	if err := e.docker.Start(string(appID)); err != nil {
		return err
	}

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
	e.agent.backupMu.Lock()
	backupErr := e.restic.Backup(appID, func(p *common.ResticProgress) {
		rt.SetProgress(p)
	})
	e.agent.backupMu.Unlock()
	rt.ClearProgress()

	// Step 3: Update backup state
	e.agent.runtimeMu.Lock()
	if backupErr != nil {
		rt.Backup.LastError = backupErr.Error()
		log.Printf("Executor: backup failed for %s: %s", appID, backupErr)
	} else {
		rt.Backup.LastBackupAt = time.Now()
		rt.Backup.LastError = ""
		log.Printf("Executor: backup completed for %s", appID)
	}
	rt.Backup.InProgress = false
	e.agent.runtimeMu.Unlock()

	// Step 4: Release global backup lease
	if releaseErr := e.agent.managerClient.ReleaseBackupLease(e.agent.name); releaseErr != nil {
		log.Printf("Executor: failed to release backup lease for %s: %s", appID, releaseErr)
	}

	// Step 5: Reset phase — reconcile will restart the app
	rt.SetPhase(common.PhaseEmpty)

	return backupErr
}
