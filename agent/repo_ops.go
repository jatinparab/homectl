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

func (a *Agent) RepoCheckStatus() RepoOpStatus {
	return a.repoCheck.Status()
}

func (a *Agent) RepoUnlockStatus() RepoOpStatus {
	return a.repoUnlock.Status()
}

func (a *Agent) TriggerRepoCheck() error {
	a.repoCheck.mu.Lock()
	if a.repoCheck.Busy {
		a.repoCheck.mu.Unlock()
		return ErrRepoOpBusy
	}
	a.repoCheck.Busy = true
	a.repoCheck.LastError = ""
	a.repoCheck.ProgressPercent = 0
	a.repoCheck.Log.Clear()
	a.repoCheck.Log.Write("Starting restic check...")
	a.repoCheck.mu.Unlock()

	go a.runRepoCheck()
	return nil
}

func (a *Agent) TriggerRepoUnlock() error {
	a.repoUnlock.mu.Lock()
	if a.repoUnlock.Busy {
		a.repoUnlock.mu.Unlock()
		return ErrRepoOpBusy
	}
	a.repoUnlock.Busy = true
	a.repoUnlock.LastError = ""
	a.repoUnlock.ProgressPercent = 0
	a.repoUnlock.Log.Clear()
	a.repoUnlock.Log.Write("Starting restic unlock...")
	a.repoUnlock.mu.Unlock()

	go a.runRepoUnlock()
	return nil
}

func (a *Agent) runRepoCheck() {
	ctx, cancel := context.WithCancel(a.sensorCtx)
	defer cancel()

	// Serialize with backups/prunes on this node.
	a.backupMu.Lock()
	defer a.backupMu.Unlock()

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

	a.repoCheck.mu.Lock()
	a.repoCheck.LastAt = time.Now()
	if err != nil {
		a.repoCheck.LastError = err.Error()
		a.repoCheck.Log.Write("ERROR: " + err.Error())
	}
	a.repoCheck.Busy = false
	a.repoCheck.ProgressPercent = 0
	a.repoCheck.mu.Unlock()

	a.saveRepoOpsState()
}

func (a *Agent) runRepoUnlock() {
	ctx, cancel := context.WithCancel(a.sensorCtx)
	defer cancel()

	// Serialize with backups/prunes/checks on this node.
	a.backupMu.Lock()
	defer a.backupMu.Unlock()

	a.repoUnlock.mu.Lock()
	a.repoUnlock.Log.Write("Repo lock acquired")
	a.repoUnlock.mu.Unlock()

	err := a.restic.Unlock(ctx, func(line string) {
		line = strings.TrimSuffix(line, "\n")
		a.repoUnlock.mu.Lock()
		a.repoUnlock.Log.Write(line)
		a.repoUnlock.mu.Unlock()
	})

	a.repoUnlock.mu.Lock()
	a.repoUnlock.LastAt = time.Now()
	if err != nil {
		a.repoUnlock.LastError = err.Error()
		a.repoUnlock.Log.Write("ERROR: " + err.Error())
	}
	a.repoUnlock.Busy = false
	a.repoUnlock.mu.Unlock()

	a.saveRepoOpsState()
}
