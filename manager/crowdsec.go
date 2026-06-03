package manager

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jatinparab/homectl/config"
)

var crowdSecLog = slog.With("area", "crowdsec")

const (
	crowdSecBinary       = "crowdsec"
	cscliBinary          = "cscli"
	crowdSecDefaultsDir  = "/usr/share/homectl/crowdsec"
	crowdSecBouncerName  = "homectl-caddy"
	crowdSecLogLineLimit = 500
)

var (
	crowdSecCmd   *exec.Cmd
	crowdSecCmdMu sync.Mutex
	logrusLineRe  = regexp.MustCompile(`\b([a-zA-Z_]+)=("[^"]*"|\S+)`)
)

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
	AcquisitionLinesRead      int `json:"acquisition_lines_read"`
	AcquisitionLinesParsed    int `json:"acquisition_lines_parsed"`
	AcquisitionLinesPour      int `json:"acquisition_lines_pour"`
	AlertsCount               int `json:"alerts_count"`
	ActiveBans                int `json:"active_bans"`
	ActiveScenarios           int `json:"active_scenarios"`
	ParserHitsTotal           int `json:"parser_hits_total"`
	ParserParsedTotal         int `json:"parser_parsed_total"`
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
		cmd := exec.CommandContext(ctx, crowdSecBinary, "-c", crowdSecConfigPath())
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start crowdsec: %w", err)
		}
		crowdSecCmd = cmd
		crowdSecLog.Info("CrowdSec started", "pid", cmd.Process.Pid)
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

func writeCrowdSecAcquisition() error {
	if err := os.MkdirAll(filepath.Dir(crowdSecCaddyAcquisPath()), 0755); err != nil {
		return err
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
	if err := runCSCLI(2*time.Minute, "hub", "update"); err != nil {
		return err
	}
	items := []string{
		"crowdsecurity/caddy",
		"crowdsecurity/http-cve",
	}
	args := append([]string{"collections", "install"}, items...)
	return runCSCLI(5*time.Minute, args...)
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
		_ = crowdSecCmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() {
			_ = crowdSecCmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = crowdSecCmd.Process.Kill()
			<-done
		}
		crowdSecCmd = nil
	}
	crowdSecLog.Info("CrowdSec stopped")
}

func isCrowdSecRunning() bool {
	crowdSecCmdMu.Lock()
	defer crowdSecCmdMu.Unlock()
	return crowdSecCmd != nil && crowdSecCmd.Process != nil
}

func (m *Manager) crowdSecDecisions() ([]CrowdSecDecision, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cscliBinary, "decisions", "list", "-o", "json")
	output, err := cmd.CombinedOutput()
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cscliBinary, "metrics", "-o", "json")
	output, err := cmd.CombinedOutput()
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
	cmd := exec.CommandContext(ctx, cscliBinary, "decisions", "add", "--ip", ip, "--type", "ban", "--duration", duration, "--reason", reason)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("add crowdsec decision: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (m *Manager) deleteCrowdSecDecision(id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid decision id %d", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idStr := strconv.Itoa(id)
	cmd := exec.CommandContext(ctx, cscliBinary, "decisions", "delete", "--id", idStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete crowdsec decision: %w: %s", err, strings.TrimSpace(string(output)))
	}
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
