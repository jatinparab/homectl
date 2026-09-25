package manager

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jatinparab/homectl/config"
)

var crowdSecLog = slog.With("area", "crowdsec")

const (
	crowdSecBinary        = "crowdsec"
	cscliBinary           = "cscli"
	crowdSecDefaultsDir   = "/usr/share/homectl/crowdsec"
	crowdSecBouncerName   = "homectl-caddy"
	crowdSecLogLineLimit  = 500
	crowdSecBansCacheTTL  = 30 * time.Second
	crowdSecStatsCacheTTL = 15 * time.Second
	crowdSecReadTimeout   = 4 * time.Second
)

var (
	crowdSecCmd     *exec.Cmd
	crowdSecCmdDone chan struct{}
	crowdSecCmdMu   sync.Mutex
	logrusLineRe    = regexp.MustCompile(`\b([a-zA-Z_]+)=("[^"]*"|\S+)`)
)

var runCrowdSecCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

type crowdSecCachedResult[T any] struct {
	mu         sync.Mutex
	value      T
	hasValue   bool
	fetchedAt  time.Time
	refreshing bool
	waitCh     chan struct{}
}

type CrowdSecDecision struct {
	ID          int    `json:"id,omitempty"`
	Scope       string `json:"scope"`
	Value       string `json:"value"`
	Type        string `json:"type"`
	Origin      string `json:"origin,omitempty"`
	Scenario    string `json:"scenario,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Duration    string `json:"duration,omitempty"`
	Until       string `json:"until,omitempty"`
	ASNName     string `json:"as_name,omitempty"`
	ASNNumber   string `json:"as_number,omitempty"`
	Country     string `json:"country,omitempty"`
	SourceRange string `json:"source_range,omitempty"`
	EventCount  int    `json:"event_count"`
	StartAt     string `json:"start_at,omitempty"`
	StopAt      string `json:"stop_at,omitempty"`
}

type CrowdSecMetrics struct {
	AcquisitionLinesRead   int `json:"acquisition_lines_read"`
	AcquisitionLinesParsed int `json:"acquisition_lines_parsed"`
	AcquisitionLinesPour   int `json:"acquisition_lines_pour"`
	AlertsCount            int `json:"alerts_count"`
	ActiveBans             int `json:"active_bans"`
	ActiveScenarios        int `json:"active_scenarios"`
	ParserHitsTotal        int `json:"parser_hits_total"`
	ParserParsedTotal      int `json:"parser_parsed_total"`
}

type CrowdSecLogEntry struct {
	Timestamp string `json:"timestamp,omitempty"`
	Source    string `json:"source"`
	Level     string `json:"level,omitempty"`
	Message   string `json:"message"`
	Raw       string `json:"raw"`
}

type CrowdSecDecisionInput struct {
	IP       string `json:"ip"`
	Duration string `json:"duration"`
	Reason   string `json:"reason"`
}

type CrowdSecStatus struct {
	State           string    `json:"state"`
	Step            string    `json:"step"`
	Message         string    `json:"message,omitempty"`
	Error           string    `json:"error,omitempty"`
	ConfigReady     bool      `json:"config_ready"`
	HubReady        bool      `json:"hub_ready"`
	EngineRunning   bool      `json:"engine_running"`
	BouncerReady    bool      `json:"bouncer_ready"`
	CaddyIntegrated bool      `json:"caddy_integrated"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CrowdSecHubItem struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Description     string `json:"description"`
	Version         string `json:"version"`
	Status          string `json:"status,omitempty"`
	Installed       bool   `json:"installed"`
	UpdateAvailable bool   `json:"update_available"`
}

type CrowdSecHubResponse struct {
	Items []CrowdSecHubItem `json:"items"`
}

type CrowdSecHubInstallInput struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

func crowdSecDir() string {
	return config.CrowdSecDir
}

func crowdSecEtcDir() string {
	return filepath.Join(crowdSecDir(), "etc")
}

func crowdSecDataDir() string {
	return filepath.Join(crowdSecDir(), "data")
}

func crowdSecLogsDir() string {
	return filepath.Join(crowdSecDir(), "logs")
}

func crowdSecBouncerKeyPath() string {
	return filepath.Join(crowdSecDir(), "bouncer-key")
}

func crowdSecCaddyAcquisPath() string {
	return filepath.Join(crowdSecEtcDir(), "acquis.d", "homectl-caddy.yaml")
}

func crowdSecAppSecAcquisPath() string {
	return filepath.Join(crowdSecEtcDir(), "acquis.d", "homectl-appsec.yaml")
}

func crowdSecSSHAcquisPath() string {
	return filepath.Join(crowdSecEtcDir(), "acquis.d", "homectl-ssh.yaml")
}

func crowdSecBudgetWhitelistPath() string {
	return filepath.Join(crowdSecEtcDir(), "postoverflows", "s01-whitelist", "homectl-budget.yaml")
}

func crowdSecSSHLogPath() string {
	return os.Getenv("HOMECTL_CROWDSEC_SSH_LOG")
}

func crowdSecSSHEnabled() bool {
	return crowdSecSSHLogPath() != ""
}

func crowdSecConfigPath() string {
	return filepath.Join(crowdSecEtcDir(), "config.yaml")
}

func crowdSecBouncerKey() string {
	data, err := os.ReadFile(crowdSecBouncerKeyPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (m *Manager) StartCrowdSecBootstrap(ctx context.Context) {
	m.setCrowdSecStatus(func(status *CrowdSecStatus) {
		status.State = "starting"
		status.Step = "queued"
		status.Message = "Preparing CrowdSec"
		status.CaddyIntegrated = crowdSecBouncerKey() != ""
	})
	go func() {
		err := m.bootstrapCrowdSec(ctx)
		if err != nil {
			crowdSecLog.Warn("CrowdSec bootstrap failed", "err", err)
			m.setCrowdSecStatus(func(status *CrowdSecStatus) {
				status.State = "error"
				status.Error = err.Error()
				status.Message = "CrowdSec bootstrap failed"
			})
			return
		}
		m.setCrowdSecStatus(func(status *CrowdSecStatus) {
			status.State = "ready"
			status.Step = "ready"
			status.Message = "CrowdSec is protecting public routes"
			status.Error = ""
		})
	}()
}

func (m *Manager) crowdSecBootstrapStatus() CrowdSecStatus {
	m.crowdSecMu.RLock()
	status := m.crowdSecStatus
	m.crowdSecMu.RUnlock()
	if status.State == "" {
		status.State = "idle"
		status.Step = "idle"
		status.UpdatedAt = time.Now()
	}
	status.BouncerReady = status.BouncerReady || crowdSecBouncerKey() != ""
	status.CaddyIntegrated = status.CaddyIntegrated || crowdSecBouncerKey() != ""
	status.EngineRunning = status.EngineRunning || isCrowdSecRunning()
	return status
}

func (m *Manager) setCrowdSecStatus(update func(*CrowdSecStatus)) {
	m.crowdSecMu.Lock()
	defer m.crowdSecMu.Unlock()
	status := m.crowdSecStatus
	update(&status)
	status.UpdatedAt = time.Now()
	m.crowdSecStatus = status
}

func (m *Manager) EnsureCrowdSec() error {
	crowdSecCmdMu.Lock()
	defer crowdSecCmdMu.Unlock()

	return m.ensureCrowdSecLocked(context.Background())
}

func (m *Manager) bootstrapCrowdSec(ctx context.Context) error {
	crowdSecCmdMu.Lock()
	defer crowdSecCmdMu.Unlock()

	if err := m.ensureCrowdSecLocked(ctx); err != nil {
		return err
	}
	m.setCrowdSecStatus(func(status *CrowdSecStatus) {
		status.Step = "caddy"
		status.Message = "Reloading Caddy with CrowdSec bouncer"
	})
	if err := m.SyncCaddyConfig(); err != nil {
		return fmt.Errorf("reload caddy with crowdsec: %w", err)
	}
	m.setCrowdSecStatus(func(status *CrowdSecStatus) {
		status.CaddyIntegrated = true
	})
	return nil
}

func (m *Manager) ensureCrowdSecLocked(ctx context.Context) error {
	m.setCrowdSecStatus(func(status *CrowdSecStatus) {
		status.State = "starting"
		status.Step = "binaries"
		status.Message = "Checking CrowdSec binaries"
		status.Error = ""
	})

	if _, err := exec.LookPath(crowdSecBinary); err != nil {
		return fmt.Errorf("crowdsec binary not found: %w", err)
	}
	if _, err := exec.LookPath(cscliBinary); err != nil {
		return fmt.Errorf("cscli binary not found: %w", err)
	}
	m.setCrowdSecStatus(func(status *CrowdSecStatus) {
		status.Step = "config"
		status.Message = "Writing persisted CrowdSec config under /config"
	})
	if err := bootstrapCrowdSecPersistence(); err != nil {
		return err
	}
	if err := writeCrowdSecAcquisition(); err != nil {
		return err
	}
	if err := ensureCrowdSecLocalCredentials(); err != nil {
		return err
	}
	m.setCrowdSecStatus(func(status *CrowdSecStatus) {
		status.ConfigReady = true
		status.Step = "hub"
		status.Message = "Installing CrowdSec Caddy collections"
	})
	if err := installCrowdSecHubItems(); err != nil {
		return err
	}
	m.setCrowdSecStatus(func(status *CrowdSecStatus) {
		status.HubReady = true
		status.Step = "engine"
		status.Message = "Starting CrowdSec engine"
	})
	if crowdSecCmd == nil || crowdSecCmd.Process == nil {
		if err := startCrowdSecLocked(ctx); err != nil {
			return err
		}
	}
	if err := waitCrowdSecReady(30 * time.Second); err != nil {
		return err
	}
	m.setCrowdSecStatus(func(status *CrowdSecStatus) {
		status.EngineRunning = true
		status.Step = "bouncer"
		status.Message = "Creating Caddy bouncer key"
	})
	if crowdSecBouncerKey() == "" {
		key, err := createCrowdSecBouncerKey()
		if err != nil {
			return err
		}
		if err := os.WriteFile(crowdSecBouncerKeyPath(), []byte(key+"\n"), 0600); err != nil {
			return fmt.Errorf("persist crowdsec bouncer key: %w", err)
		}
	}
	m.setCrowdSecStatus(func(status *CrowdSecStatus) {
		status.BouncerReady = true
	})
	return nil
}

func bootstrapCrowdSecPersistence() error {
	for _, dir := range []string{crowdSecEtcDir(), crowdSecDataDir(), crowdSecLogsDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create crowdsec directory %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(crowdSecConfigPath()); errors.Is(err, os.ErrNotExist) {
		if err := copyDir(filepath.Join(crowdSecDefaultsDir, "etc"), crowdSecEtcDir()); err != nil {
			return fmt.Errorf("bootstrap crowdsec config: %w", err)
		}
	}
	if err := setCrowdSecLogTargets(crowdSecConfigPath(), crowdSecLogsDir()); err != nil {
		return err
	}
	if empty, err := isDirEmpty(crowdSecDataDir()); err != nil {
		return err
	} else if empty {
		if err := copyDir(filepath.Join(crowdSecDefaultsDir, "data"), crowdSecDataDir()); err != nil {
			crowdSecLog.Warn("Failed to bootstrap CrowdSec data defaults", "err", err)
		}
	}
	if err := ensureSymlink("/etc/crowdsec", crowdSecEtcDir()); err != nil {
		return err
	}
	if err := os.MkdirAll("/var/lib/crowdsec", 0755); err != nil {
		return err
	}
	if err := ensureSymlink("/var/lib/crowdsec/data", crowdSecDataDir()); err != nil {
		return err
	}
	if err := ensureSymlink("/var/log/crowdsec.log", filepath.Join(crowdSecLogsDir(), "crowdsec.log")); err != nil {
		return err
	}
	if err := ensureSymlink("/var/log/crowdsec_api.log", filepath.Join(crowdSecLogsDir(), "crowdsec_api.log")); err != nil {
		return err
	}
	return nil
}

func setCrowdSecLogTargets(configPath, logDir string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	updated := string(data)
	updated = strings.ReplaceAll(updated, "log_media: stdout", "log_media: file")
	updated = strings.ReplaceAll(updated, "log_dir: /var/log/", "log_dir: "+logDir)
	if updated == string(data) {
		return nil
	}
	return os.WriteFile(configPath, []byte(updated), 0644)
}

func ensureSymlink(linkPath, target string) error {
	if current, err := os.Readlink(linkPath); err == nil {
		if current == target {
			return nil
		}
		if err := os.Remove(linkPath); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		if err := os.RemoveAll(linkPath); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return err
	}
	if strings.HasPrefix(linkPath, "/var/log/") {
		if f, err := os.OpenFile(target, os.O_CREATE, 0644); err == nil {
			_ = f.Close()
		}
	}
	return os.Symlink(target, linkPath)
}

func isDirEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(linkTarget, target)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

const crowdSecBudgetWhitelist = `name: homectl/budget-actual
description: Prevent Actual Budget's normal asset and sync requests from triggering false-positive remediation
whitelist:
  reason: Actual Budget legitimately serves SQL migration assets and makes many non-static sync requests
  expression:
    - (evt.Overflow.Alert.Scenario == "crowdsecurity/http-sensitive-files" || evt.Overflow.Alert.Scenario == "crowdsecurity/http-crawl-non_statics") && all(evt.Overflow.Alert.Events, {.GetMeta("target_fqdn") == "budget.example.net"})
`

func writeCrowdSecAcquisition() error {
	if err := os.MkdirAll(filepath.Dir(crowdSecCaddyAcquisPath()), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(crowdSecBudgetWhitelistPath()), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(crowdSecBudgetWhitelistPath(), []byte(crowdSecBudgetWhitelist), 0644); err != nil {
		return fmt.Errorf("write Actual Budget whitelist: %w", err)
	}
	if err := os.WriteFile(filepath.Join(crowdSecEtcDir(), "acquis.yaml"), []byte("# homectl writes acquisition sources under acquis.d\n"), 0644); err != nil {
		return err
	}
	caddyAcquis := fmt.Sprintf(`filenames:
  - %s
labels:
  type: caddy
source: file
`, managerCaddyAccessLogPath())
	if err := os.WriteFile(crowdSecCaddyAcquisPath(), []byte(caddyAcquis), 0644); err != nil {
		return err
	}
	if err := os.Remove(crowdSecAppSecAcquisPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if crowdSecSSHEnabled() {
		sshAcquis := fmt.Sprintf(`filenames:
  - %s
labels:
  type: syslog
source: file
`, crowdSecSSHLogPath())
		if err := os.WriteFile(crowdSecSSHAcquisPath(), []byte(sshAcquis), 0644); err != nil {
			return fmt.Errorf("write ssh acquis: %w", err)
		}
	} else {
		if err := os.Remove(crowdSecSSHAcquisPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func ensureCrowdSecLocalCredentials() error {
	path := filepath.Join(crowdSecEtcDir(), "local_api_credentials.yaml")
	data, err := os.ReadFile(path)
	if err == nil && bytes.Contains(data, []byte("login:")) && bytes.Contains(data, []byte("password:")) {
		if bytes.Contains(data, []byte("url: http://127.0.0.1:8080")) {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "url:") {
				lines[i] = "url: http://127.0.0.1:8080"
			}
		}
		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600)
	}

	secretBytes := make([]byte, 24)
	if _, err := rand.Read(secretBytes); err != nil {
		return fmt.Errorf("generate crowdsec machine password: %w", err)
	}
	login := "homectl-manager"
	password := hex.EncodeToString(secretBytes)
	content := fmt.Sprintf("url: http://127.0.0.1:8080\nlogin: %s\npassword: %s\n", login, password)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write crowdsec local credentials: %w", err)
	}
	if err := runCSCLI(30*time.Second, "machines", "add", login, "-p", password, "--force"); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("rewrite crowdsec local credentials: %w", err)
	}
	return nil
}

func installCrowdSecHubItems() error {
	return installCrowdSecHubItemsWithRunner(crowdSecSSHEnabled(), runCSCLI)
}

func installCrowdSecHubItemsWithRunner(sshEnabled bool, run func(time.Duration, ...string) error) error {
	// Install from the hub index bundled in the image. Startup must not depend on
	// upstream DNS/network access because Caddy's bouncer needs the local LAPI.
	// Hub updates remain available through the explicit CrowdSec management API.
	items := []string{
		"crowdsecurity/caddy",
		"crowdsecurity/http-cve",
	}
	if sshEnabled {
		items = append(items, "crowdsecurity/sshd")
	}
	args := append([]string{"collections", "install"}, items...)
	return run(5*time.Minute, args...)
}

func runCSCLI(timeout time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cscliBinary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cscli %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func waitCrowdSecReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, cscliBinary, "lapi", "status")
		output, err := cmd.CombinedOutput()
		cancel()
		last = strings.TrimSpace(string(output))
		if err == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("crowdsec LAPI not ready: %s", last)
}

func createCrowdSecBouncerKey() (string, error) {
	name := crowdSecBouncerName
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cscliBinary, "bouncers", "add", name, "-o", "raw")
	output, err := cmd.CombinedOutput()
	if err != nil {
		name = fmt.Sprintf("%s-%d", crowdSecBouncerName, time.Now().Unix())
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd = exec.CommandContext(ctx, cscliBinary, "bouncers", "add", name, "-o", "raw")
		output, err = cmd.CombinedOutput()
	}
	if err != nil {
		return "", fmt.Errorf("create crowdsec bouncer: %w: %s", err, strings.TrimSpace(string(output)))
	}
	key := strings.TrimSpace(string(output))
	if key == "" {
		return "", errors.New("crowdsec returned an empty bouncer key")
	}
	return key, nil
}

func stopCrowdSec() {
	crowdSecCmdMu.Lock()
	defer crowdSecCmdMu.Unlock()
	if crowdSecCmd != nil && crowdSecCmd.Process != nil {
		done := crowdSecCmdDone
		if done == nil {
			done = make(chan struct{})
			close(done)
		}
		_ = crowdSecCmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = crowdSecCmd.Process.Kill()
			<-done
		}
		crowdSecCmd = nil
		crowdSecCmdDone = nil
	}
	crowdSecLog.Info("CrowdSec stopped")
}

func isCrowdSecRunning() bool {
	crowdSecCmdMu.Lock()
	defer crowdSecCmdMu.Unlock()
	return crowdSecCmd != nil && crowdSecCmd.Process != nil
}

func startCrowdSecLocked(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, crowdSecBinary, "-c", crowdSecConfigPath())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start crowdsec: %w", err)
	}
	done := make(chan struct{})
	crowdSecCmd = cmd
	crowdSecCmdDone = done
	crowdSecLog.Info("CrowdSec started", "pid", cmd.Process.Pid)
	go func(started *exec.Cmd, done chan struct{}) {
		err := started.Wait()
		if err != nil {
			crowdSecLog.Warn("CrowdSec exited", "pid", started.Process.Pid, "err", err)
		} else {
			crowdSecLog.Info("CrowdSec exited", "pid", started.Process.Pid)
		}
		close(done)
		crowdSecCmdMu.Lock()
		if crowdSecCmd == started {
			crowdSecCmd = nil
			crowdSecCmdDone = nil
		}
		crowdSecCmdMu.Unlock()
	}(cmd, done)
	return nil
}

func (m *Manager) crowdSecDecisions() ([]CrowdSecDecision, error) {
	ctx, cancel := context.WithTimeout(context.Background(), crowdSecReadTimeout+time.Second)
	defer cancel()
	return m.crowdSecBans.get(ctx, crowdSecBansCacheTTL, func() ([]CrowdSecDecision, error) {
		return loadCrowdSecDecisions()
	})
}

func (m *Manager) invalidateCrowdSecDecisions() {
	m.crowdSecBans.invalidate()
}

func loadCrowdSecDecisions() ([]CrowdSecDecision, error) {
	ctx, cancel := context.WithTimeout(context.Background(), crowdSecReadTimeout)
	defer cancel()
	output, err := runCrowdSecCommand(ctx, cscliBinary, "decisions", "list", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("list crowdsec decisions: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var raw []map[string]any
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, err
	}
	decisions := make([]CrowdSecDecision, 0)
	for _, alert := range raw {
		alertScenario := stringFromAny(alert["scenario"])
		source := mapFromAny(alert["source"])
		sourceScope := stringFromAny(source["scope"])
		sourceValue := stringFromAny(source["value"])
		asName := stringFromAny(source["as_name"])
		asNumber := stringFromAny(source["as_number"])
		country := stringFromAny(source["cn"])
		sourceRange := stringFromAny(source["range"])
		eventCount := intFromAny(alert["events_count"])
		startAt := stringFromAny(alert["start_at"])
		stopAt := stringFromAny(alert["stop_at"])

		decisionsRaw, _ := alert["decisions"].([]any)
		for _, d := range decisionsRaw {
			item, ok := d.(map[string]any)
			if !ok {
				continue
			}
			scope := stringFromAny(item["scope"])
			value := stringFromAny(item["value"])
			if scope == "" {
				scope = sourceScope
			}
			if value == "" {
				value = sourceValue
			}
			until := stopAt
			if until == "" {
				until = computeUntil(alert["start_at"], stringFromAny(item["duration"]))
			}
			decisions = append(decisions, CrowdSecDecision{
				ID:          intFromAny(item["id"]),
				Scope:       scope,
				Value:       value,
				Type:        stringFromAny(item["type"]),
				Origin:      stringFromAny(item["origin"]),
				Scenario:    stringFromAny(item["scenario"]),
				Reason:      alertScenario,
				Duration:    stringFromAny(item["duration"]),
				Until:       until,
				ASNName:     asName,
				ASNNumber:   asNumber,
				Country:     country,
				SourceRange: sourceRange,
				EventCount:  eventCount,
				StartAt:     startAt,
				StopAt:      stopAt,
			})
		}
	}
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].Value < decisions[j].Value })
	return decisions, nil
}

func (c *crowdSecCachedResult[T]) get(ctx context.Context, ttl time.Duration, load func() (T, error)) (T, error) {
	now := time.Now()
	c.mu.Lock()
	if c.hasValue && now.Sub(c.fetchedAt) < ttl {
		value := c.value
		c.mu.Unlock()
		return value, nil
	}
	if c.refreshing {
		if c.hasValue {
			value := c.value
			c.mu.Unlock()
			return value, nil
		}
		waitCh := c.waitCh
		c.mu.Unlock()
		select {
		case <-waitCh:
			return c.get(ctx, ttl, load)
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
	waitCh := make(chan struct{})
	c.refreshing = true
	c.waitCh = waitCh
	c.mu.Unlock()

	value, err := load()

	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.value = value
		c.hasValue = true
		c.fetchedAt = time.Now()
	}
	c.refreshing = false
	close(waitCh)
	c.waitCh = nil
	if err != nil && c.hasValue {
		crowdSecLog.Warn("using stale CrowdSec data after refresh failed", "err", err)
		return c.value, nil
	}
	return value, err
}

func (c *crowdSecCachedResult[T]) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hasValue = false
	var zero T
	c.value = zero
	c.fetchedAt = time.Time{}
}

func computeUntil(startAt any, duration string) string {
	startStr := stringFromAny(startAt)
	if startStr == "" || duration == "" {
		return ""
	}
	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		return ""
	}
	d, err := time.ParseDuration(duration)
	if err != nil {
		return ""
	}
	return start.Add(d).Format(time.RFC3339)
}

func mapFromAny(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	default:
		return nil
	}
}

func (m *Manager) crowdSecMetrics() (CrowdSecMetrics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), crowdSecReadTimeout+time.Second)
	defer cancel()
	return m.crowdSecStats.get(ctx, crowdSecStatsCacheTTL, func() (CrowdSecMetrics, error) {
		return loadCrowdSecMetrics()
	})
}

func (m *Manager) invalidateCrowdSecMetrics() {
	m.crowdSecStats.invalidate()
}

func loadCrowdSecMetrics() (CrowdSecMetrics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), crowdSecReadTimeout)
	defer cancel()
	output, err := runCrowdSecCommand(ctx, cscliBinary, "metrics", "-o", "json")
	if err != nil {
		return CrowdSecMetrics{}, fmt.Errorf("fetch crowdsec metrics: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var raw map[string]any
	if err := json.Unmarshal(output, &raw); err != nil {
		return CrowdSecMetrics{}, err
	}
	out := CrowdSecMetrics{}
	if acq, ok := raw["acquisition"].(map[string]any); ok {
		for _, v := range acq {
			if src, ok := v.(map[string]any); ok {
				out.AcquisitionLinesRead += intFromAny(src["reads"])
				out.AcquisitionLinesParsed += intFromAny(src["parsed"])
				out.AcquisitionLinesPour += intFromAny(src["pour"])
			}
		}
	}
	if alerts, ok := raw["alerts"].(map[string]any); ok {
		for _, v := range alerts {
			out.AlertsCount += int(intFromAny(v))
		}
		out.AlertsCount = len(alerts)
	}
	if decisions, ok := raw["decisions"].(map[string]any); ok {
		for _, originMap := range decisions {
			if om, ok := originMap.(map[string]any); ok {
				for _, actionMap := range om {
					if am, ok := actionMap.(map[string]any); ok {
						if banCount, ok := am["ban"]; ok {
							out.ActiveBans += int(intFromAny(banCount))
						}
					}
				}
			}
		}
	}
	if scenarios, ok := raw["scenarios"].(map[string]any); ok {
		for _, v := range scenarios {
			if s, ok := v.(map[string]any); ok {
				cc := intFromAny(s["curr_count"])
				if cc > 0 {
					out.ActiveScenarios++
				}
			}
		}
	}
	if parsers, ok := raw["parsers"].(map[string]any); ok {
		for _, v := range parsers {
			if p, ok := v.(map[string]any); ok {
				out.ParserHitsTotal += intFromAny(p["hits"])
				out.ParserParsedTotal += intFromAny(p["parsed"])
			}
		}
	}
	return out, nil
}

func (m *Manager) addCrowdSecDecision(input CrowdSecDecisionInput) error {
	ip := strings.TrimSpace(input.IP)
	if parsed := net.ParseIP(ip); parsed == nil {
		return fmt.Errorf("invalid IP %q", input.IP)
	}
	duration := strings.TrimSpace(input.Duration)
	if duration == "" {
		duration = "4h"
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "manual ban from homectl"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := runCrowdSecCommand(ctx, cscliBinary, "decisions", "add", "--ip", ip, "--type", "ban", "--duration", duration, "--reason", reason)
	if err != nil {
		return fmt.Errorf("add crowdsec decision: %w: %s", err, strings.TrimSpace(string(output)))
	}
	m.invalidateCrowdSecDecisions()
	m.invalidateCrowdSecMetrics()
	return nil
}

func (m *Manager) deleteCrowdSecDecision(id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid decision id %d", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idStr := strconv.Itoa(id)
	output, err := runCrowdSecCommand(ctx, cscliBinary, "decisions", "delete", "--id", idStr)
	if err != nil {
		return fmt.Errorf("delete crowdsec decision: %w: %s", err, strings.TrimSpace(string(output)))
	}
	m.invalidateCrowdSecDecisions()
	m.invalidateCrowdSecMetrics()
	return nil
}

func (m *Manager) crowdSecLogs(limit int) []CrowdSecLogEntry {
	if limit <= 0 || limit > crowdSecLogLineLimit {
		limit = 200
	}
	entries := make([]CrowdSecLogEntry, 0, limit)
	for _, source := range []struct {
		name string
		path string
	}{
		{name: "engine", path: filepath.Join(crowdSecLogsDir(), "crowdsec.log")},
		{name: "api", path: filepath.Join(crowdSecLogsDir(), "crowdsec_api.log")},
	} {
		entries = append(entries, readCrowdSecLogFile(source.name, source.path, limit)...)
	}
	entries = filterCrowdSecLogNoise(entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp > entries[j].Timestamp
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

var accessLogLineRe = regexp.MustCompile(`^\S+\s+-\s+\[`)

func filterCrowdSecLogNoise(entries []CrowdSecLogEntry) []CrowdSecLogEntry {
	filtered := make([]CrowdSecLogEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Source == "api" && strings.Contains(strings.ToLower(entry.Message), "local query logs") {
			continue
		}
		if entry.Source == "api" && accessLogLineRe.MatchString(entry.Message) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func readCrowdSecLogFile(source, path string, limit int) []CrowdSecLogEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	lines := make([]string, 0, limit)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		if len(lines) > limit {
			copy(lines, lines[1:])
			lines = lines[:limit]
		}
	}
	entries := make([]CrowdSecLogEntry, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		entries = append(entries, parseCrowdSecLogLine(source, lines[i]))
	}
	return entries
}

func parseCrowdSecLogLine(source, line string) CrowdSecLogEntry {
	entry := CrowdSecLogEntry{Source: source, Raw: line, Message: line}
	fields := map[string]string{}
	for _, match := range logrusLineRe.FindAllStringSubmatch(line, -1) {
		value := strings.Trim(match[2], `"`)
		fields[match[1]] = value
	}
	entry.Timestamp = fields["time"]
	entry.Level = fields["level"]
	if msg := fields["msg"]; msg != "" {
		entry.Message = msg
	}
	if entry.Timestamp == "" {
		if idx := strings.IndexByte(line, ' '); idx > 0 {
			entry.Timestamp = line[:idx]
		}
	}
	return entry
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
	}
}

func validateCrowdSecConfig() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, crowdSecBinary, "-c", crowdSecConfigPath(), "-t")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("validate crowdsec config: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return nil
}

func (m *Manager) crowdSecHubItems(all bool) ([]CrowdSecHubItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"hub", "list", "-o", "json"}
	if all {
		args = append(args, "-a")
	}
	cmd := exec.CommandContext(ctx, cscliBinary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list hub items: %w: %s", err, strings.TrimSpace(string(output)))
	}
	raw := string(output)
	if idx := strings.Index(raw, "{"); idx > 0 {
		raw = raw[idx:]
	}
	var grouped map[string][]map[string]any
	if err := json.Unmarshal([]byte(raw), &grouped); err != nil {
		return nil, err
	}
	typeNames := []string{"collections", "scenarios", "parsers", "postoverflows", "contexts", "appsec-configs", "appsec-rules"}
	var items []CrowdSecHubItem
	for _, typeName := range typeNames {
		hubType := strings.TrimSuffix(typeName, "s")
		if hubType == "postoverflow" {
			continue
		}
		if hubType == "context" {
			continue
		}
		if hubType == "appsec-config" {
			continue
		}
		if hubType == "appsec-rule" {
			continue
		}
		for _, rawItem := range grouped[typeName] {
			status := stringFromAny(rawItem["status"])
			localPath := stringFromAny(rawItem["local_path"])
			installed := localPath != ""
			items = append(items, CrowdSecHubItem{
				Name:            stringFromAny(rawItem["name"]),
				Type:            strings.TrimSuffix(typeName, "s"),
				Description:     stringFromAny(rawItem["description"]),
				Version:         stringFromAny(rawItem["local_version"]),
				Status:          status,
				Installed:       installed,
				UpdateAvailable: strings.Contains(status, "update-available"),
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Installed != items[j].Installed {
			return items[i].Installed
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (m *Manager) installCrowdSecHubItem(input CrowdSecHubInstallInput) error {
	hubType := strings.TrimSpace(input.Type)
	name := strings.TrimSpace(input.Name)
	if hubType == "" || name == "" {
		return fmt.Errorf("type and name are required")
	}
	if err := runCSCLI(30*time.Second, "hub", "update"); err != nil {
		crowdSecLog.Warn("hub update before install failed, continuing", "err", err)
	}
	return runCSCLI(5*time.Minute, hubType+"s", "install", name)
}

func (m *Manager) removeCrowdSecHubItem(input CrowdSecHubInstallInput) error {
	hubType := strings.TrimSpace(input.Type)
	name := strings.TrimSpace(input.Name)
	if hubType == "" || name == "" {
		return fmt.Errorf("type and name are required")
	}
	return runCSCLI(2*time.Minute, hubType+"s", "remove", name)
}

type CrowdSecVersionInfo struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	Error           string `json:"error,omitempty"`
}

var crowdSecVersionRe = regexp.MustCompile(`v?(\d+\.\d+\.\d+)(?:-[\w.]+)?`)

func crowdSecCurrentVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, crowdSecBinary, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	matches := crowdSecVersionRe.FindStringSubmatch(string(output))
	if len(matches) < 2 {
		return ""
	}
	return "v" + matches[1]
}

func crowdSecLatestVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/crowdsecurity/crowdsec/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "homectl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases returned %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return strings.TrimPrefix(body.TagName, "v"), nil
}

func crowdSecUpdateAvailable() (string, string, bool) {
	current := crowdSecCurrentVersion()
	if current == "" {
		return "", "", false
	}
	latest, err := crowdSecLatestVersion()
	if err != nil {
		crowdSecLog.Warn("failed to fetch latest CrowdSec version", "err", err)
		return current, "", false
	}
	latestClean := strings.TrimPrefix(latest, "v")
	currentClean := strings.TrimPrefix(current, "v")
	return currentClean, latestClean, latestClean != "" && latestClean != currentClean
}

func (m *Manager) crowdSecVersion() CrowdSecVersionInfo {
	current, latest, updateAvail := crowdSecUpdateAvailable()
	return CrowdSecVersionInfo{
		Current:         current,
		Latest:          latest,
		UpdateAvailable: updateAvail,
	}
}

func (m *Manager) updateCrowdSec(ctx context.Context) error {
	crowdSecCmdMu.Lock()
	defer crowdSecCmdMu.Unlock()

	current, latest, avail := crowdSecUpdateAvailable()
	if !avail {
		return fmt.Errorf("CrowdSec %s is already the latest version", current)
	}

	crowdSecLog.Info("Updating CrowdSec", "from", current, "to", latest)

	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "amd64"
	} else if arch == "arm64" {
		arch = "arm64"
	} else {
		return fmt.Errorf("unsupported architecture %s", arch)
	}

	url := fmt.Sprintf("https://github.com/crowdsecurity/crowdsec/releases/download/v%s/crowdsec-release.tgz", latest)

	downloadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "homectl")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download crowdsec release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	tmpDir, err := os.MkdirTemp("", "crowdsec-update")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		extractedPath := filepath.Join(tmpDir, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(extractedPath, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(extractedPath), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(extractedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tarReader); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}

	if wasRunning := crowdSecCmd != nil && crowdSecCmd.Process != nil; wasRunning {
		done := crowdSecCmdDone
		if done == nil {
			done = make(chan struct{})
			close(done)
		}
		_ = crowdSecCmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = crowdSecCmd.Process.Kill()
			<-done
		}
		crowdSecCmd = nil
		crowdSecCmdDone = nil
	}

	for _, binary := range []string{"crowdsec", "cscli"} {
		src := findBinaryInDir(tmpDir, binary)
		if src == "" {
			return fmt.Errorf("binary %s not found in release", binary)
		}
		dst := "/usr/local/bin/" + binary
		if err := os.Rename(src, dst); err != nil {
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("replace %s: %w", binary, err)
			}
		}
		if err := os.Chmod(dst, 0755); err != nil {
			return fmt.Errorf("chmod %s: %w", binary, err)
		}
	}

	if err := startCrowdSecLocked(ctx); err != nil {
		return err
	}
	crowdSecLog.Info("CrowdSec updated and restarted", "version", latest)

	if err := waitCrowdSecReady(30 * time.Second); err != nil {
		return fmt.Errorf("crowdsec not ready after update: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()
	dstF, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer dstF.Close()
	_, err = io.Copy(dstF, srcF)
	return err
}

func findBinaryInDir(dir, name string) string {
	var found string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
