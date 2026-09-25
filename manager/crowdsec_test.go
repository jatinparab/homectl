package manager

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCrowdSecBudgetWhitelistTargetsOnlyBudgetFalsePositives(t *testing.T) {
	for _, want := range []string{
		`evt.Overflow.Alert.Scenario == "crowdsecurity/http-sensitive-files"`,
		`evt.Overflow.Alert.Scenario == "crowdsecurity/http-crawl-non_statics"`,
		`all(evt.Overflow.Alert.Events, {.GetMeta("target_fqdn") == "budget.example.net"})`,
	} {
		if !strings.Contains(crowdSecBudgetWhitelist, want) {
			t.Errorf("Budget whitelist is missing %q", want)
		}
	}
}

func TestInstallCrowdSecHubItemsUsesCachedHubWithoutNetworkUpdate(t *testing.T) {
	var calls [][]string
	run := func(_ time.Duration, args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}

	if err := installCrowdSecHubItemsWithRunner(true, run); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"collections", "install", "crowdsecurity/caddy", "crowdsecurity/http-cve", "crowdsecurity/sshd"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected cscli calls: got %v, want %v", calls, want)
	}
}

func TestInstallCrowdSecHubItemsReturnsInstallFailure(t *testing.T) {
	installErr := errors.New("install failed")
	run := func(_ time.Duration, args ...string) error {
		if len(args) > 0 && args[0] == "collections" {
			return installErr
		}
		return nil
	}

	if err := installCrowdSecHubItemsWithRunner(false, run); !errors.Is(err, installErr) {
		t.Fatalf("got %v, want %v", err, installErr)
	}
}

func TestCrowdSecDecisionsCoalescesConcurrentLoads(t *testing.T) {
	originalRunner := runCrowdSecCommand
	defer func() { runCrowdSecCommand = originalRunner }()

	var calls int32
	runCrowdSecCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(50 * time.Millisecond)
		return []byte(`[
			{
				"scenario": "manual",
				"source": {"scope": "Ip", "value": "203.0.113.10", "cn": "US"},
				"events_count": 2,
				"start_at": "2026-06-07T10:00:00Z",
				"decisions": [
					{"id": 42, "scope": "Ip", "value": "203.0.113.10", "type": "ban", "origin": "cscli", "duration": "4h"}
				]
			}
		]`), nil
	}

	m := &Manager{}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decisions, err := m.crowdSecDecisions()
			if err != nil {
				errs <- err
				return
			}
			if len(decisions) != 1 || decisions[0].ID != 42 {
				errs <- errors.New("unexpected decisions")
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one cscli call, got %d", got)
	}
}

func TestCrowdSecMetricsUsesWarmCacheAndStaleOnRefreshError(t *testing.T) {
	originalRunner := runCrowdSecCommand
	defer func() { runCrowdSecCommand = originalRunner }()

	var calls int32
	runCrowdSecCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			return []byte(`{
				"acquisition": {"caddy": {"reads": 10, "parsed": 8, "pour": 6}},
				"alerts": {"crowdsecurity/http-probing": 3},
				"decisions": {"cscli": {"manual": {"ban": 1}}},
				"scenarios": {"crowdsecurity/http-probing": {"curr_count": 2}},
				"parsers": {"crowdsecurity/caddy-logs": {"hits": 10, "parsed": 8}}
			}`), nil
		default:
			return []byte("boom"), errors.New("cscli failed")
		}
	}

	m := &Manager{}
	metrics, err := m.crowdSecMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if metrics.AcquisitionLinesRead != 10 || metrics.ActiveBans != 1 || metrics.ParserHitsTotal != 10 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}

	cached, err := m.crowdSecMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if cached != metrics {
		t.Fatalf("warm cache changed metrics: %+v", cached)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected warm cache to avoid reload, got %d calls", got)
	}

	m.crowdSecStats.fetchedAt = time.Now().Add(-crowdSecStatsCacheTTL - time.Second)
	stale, err := m.crowdSecMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if stale != metrics {
		t.Fatalf("expected stale metrics after refresh error, got %+v", stale)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected one failed refresh, got %d calls", got)
	}
}
