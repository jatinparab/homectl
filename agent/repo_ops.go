package agent

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrRepoOpBusy = errors.New("repo operation already running")

// RepoOpState tracks repo-wide maintenance operations (check/unlock).
// It is stored on the Agent, not per-app.
type RepoOpState struct {
	mu sync.Mutex

	Busy            bool
	LastAt          time.Time
	LastError       string
	ProgressPercent float64
	Log             *PhaseLogBuffer
}

func NewRepoOpState() *RepoOpState {
	return &RepoOpState{Log: NewPhaseLogBuffer()}
}

type RepoOpStatus struct {
	Busy            bool       `json:"busy"`
	LastAt          *time.Time `json:"last_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	ProgressPercent *float64   `json:"progress_percent,omitempty"`
}

func (s *RepoOpState) Status() RepoOpStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	var lastAt *time.Time
	if !s.LastAt.IsZero() {
		t := s.LastAt
		lastAt = &t
	}
	var p *float64
	if s.Busy {
		pp := s.ProgressPercent
		if pp > 0 {
			p = &pp
		}
	}
	return RepoOpStatus{
		Busy:            s.Busy,
		LastAt:          lastAt,
		LastError:       s.LastError,
		ProgressPercent: p,
	}
}

var checkProgressRe = regexp.MustCompile(`\[(\d{2}:\d{2})\]\s+([0-9]+(?:\.[0-9]+)?)%`)

func (s *RepoOpState) maybeUpdateProgressFromLine(line string) {
	// Example: [11:32] 77.41%  1874 / 2421 snapshots
	m := checkProgressRe.FindStringSubmatch(line)
	if len(m) < 3 {
		return
	}
	p, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return
	}
	s.ProgressPercent = p
}

func (s *RepoOpState) startOp(startMessage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Busy {
		return ErrRepoOpBusy
	}

	s.Busy = true
	s.LastError = ""
	s.ProgressPercent = 0
	s.Log.Clear()
	if startMessage != "" {
		s.Log.Write(startMessage)
	}

	return nil
}

func (s *RepoOpState) finishOp(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastAt = time.Now()
	if err != nil {
		s.LastError = err.Error()
	}
	s.Busy = false
	s.ProgressPercent = 0
}

func (a *Agent) RepoCheckStatus() RepoOpStatus {
	return a.repoCheck.Status()
}

func (a *Agent) RepoUnlockStatus() RepoOpStatus {
	return a.repoUnlock.Status()
}

func (a *Agent) TriggerRepoCheck() error {
	if err := a.repoCheck.startOp("Starting restic check..."); err != nil {
		return err
	}

	go a.runRepoCheck()
	return nil
}

func (a *Agent) TriggerRepoUnlock() error {
	if err := a.repoUnlock.startOp("Starting restic unlock..."); err != nil {
		return err
	}

	go a.runRepoUnlock()
	return nil
}

func (a *Agent) runRepoCheck() {
	ctx, cancel := context.WithCancel(a.sensorCtx)
	defer cancel()

	// Serialize with backup/prune lane on this node.
	a.beginExclusiveBackupLane()
	defer a.endExclusiveBackupLane()

	a.repoCheck.mu.Lock()
	a.repoCheck.Log.Write("Repo lock acquired")
	a.repoCheck.mu.Unlock()

	err := a.restic.Check(ctx, func(line string) {
		line = strings.TrimSuffix(line, "\n")
		a.repoCheck.mu.Lock()
		a.repoCheck.maybeUpdateProgressFromLine(line)
		a.repoCheck.Log.Write(line)
		a.repoCheck.mu.Unlock()
	})

	if err != nil {
		a.repoCheck.mu.Lock()
		a.repoCheck.Log.Write("ERROR: " + err.Error())
		a.repoCheck.mu.Unlock()
	}
	a.repoCheck.finishOp(err)

	a.saveRepoOpsState()
}

func (a *Agent) runRepoUnlock() {
	ctx, cancel := context.WithCancel(a.sensorCtx)
	defer cancel()

	// Serialize with backup/prune lane on this node.
	a.beginExclusiveBackupLane()
	defer a.endExclusiveBackupLane()

	a.repoUnlock.mu.Lock()
	a.repoUnlock.Log.Write("Repo lock acquired")
	a.repoUnlock.mu.Unlock()

	err := a.restic.Unlock(ctx, func(line string) {
		line = strings.TrimSuffix(line, "\n")
		a.repoUnlock.mu.Lock()
		a.repoUnlock.Log.Write(line)
		a.repoUnlock.mu.Unlock()
	})

	if err != nil {
		a.repoUnlock.mu.Lock()
		a.repoUnlock.Log.Write("ERROR: " + err.Error())
		a.repoUnlock.mu.Unlock()
	}
	a.repoUnlock.finishOp(err)

	a.saveRepoOpsState()
}

func (a *Agent) TriggerRepoForceUnlock() error {
	if err := a.repoUnlock.startOp("Starting restic unlock --remove-all..."); err != nil {
		return err
	}

	go a.runRepoForceUnlock()
	return nil
}

func (a *Agent) runRepoForceUnlock() {
	ctx, cancel := context.WithCancel(a.sensorCtx)
	defer cancel()

	// No backupMu — force unlock runs regardless of running operations.
	err := a.restic.ForceUnlock(ctx, func(line string) {
		line = strings.TrimSuffix(line, "\n")
		a.repoUnlock.mu.Lock()
		a.repoUnlock.Log.Write(line)
		a.repoUnlock.mu.Unlock()
	})

	if err != nil {
		a.repoUnlock.mu.Lock()
		a.repoUnlock.Log.Write("ERROR: " + err.Error())
		a.repoUnlock.mu.Unlock()
	}
	a.repoUnlock.finishOp(err)

	a.saveRepoOpsState()
}
