package manager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

const appLogsPollInterval = 2 * time.Second

func splitLogLines(b []byte) []string {
	raw := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// overlapSuffixPrefix returns k such that prev[len(prev)-k:] == curr[:k] with maximal k.
func overlapSuffixPrefix(prev, curr []string) int {
	max := len(prev)
	if len(curr) < max {
		max = len(curr)
	}
	for k := max; k > 0; k-- {
		match := true
		for i := 0; i < k; i++ {
			if prev[len(prev)-k+i] != curr[i] {
				match = false
				break
			}
		}
		if match {
			return k
		}
	}
	return 0
}

// streamAgentAppLogsPolling provides near-real-time app logs by polling the agent
// and emitting only newly appended lines.
func (m *Manager) streamAgentAppLogsPolling(ctx context.Context, agentName, appID string, emit func(string) bool) error {
	var prev []string
	fetch := func() error {
		status, body, err := m.requestAgentUnary(ctx, agentName, common.LinkReqAppLogs, common.AppLogsPayload{AppID: appID})
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("agent returned HTTP %d", status)
		}
		curr := splitLogLines(body)
		overlap := overlapSuffixPrefix(prev, curr)
		for _, line := range curr[overlap:] {
			if !emit(line) {
				return context.Canceled
			}
		}
		prev = curr
		return nil
	}

	if err := fetch(); err != nil {
		return err
	}

	ticker := time.NewTicker(appLogsPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = fetch()
		}
	}
}
