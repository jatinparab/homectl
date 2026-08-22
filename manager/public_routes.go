package manager

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

var publicRouteLog = slog.With("area", "public-routes")

const (
	PublicRouteModeHomelab = "homelab"
	PublicRouteModeCustom  = "custom"

	PublicRouteAuthModeAuthentik = "authentik"
	PublicRouteAuthModeUpstream  = "upstream"
	PublicRouteAuthModePublic    = "public"

	homectlLocalhostAlias  = "homectl-localhost"
	managerLocalhostAlias  = "manager-localhost"
	publicRouteLogsAPIPath = "/api/v1/public-route-logs"
)

var publicRouteHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

type PublicRoute struct {
	ID           string    `json:"id"`
	Host         string    `json:"host"`
	Mode         string    `json:"mode"`
	AuthMode     string    `json:"auth_mode,omitempty"`
	CustomTarget string    `json:"custom_target,omitempty"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type PublicRouteHealth struct {
	Working       bool      `json:"working"`
	Status        string    `json:"status"`
	CheckedAt     time.Time `json:"checked_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	ResolvedTo    string    `json:"resolved_to,omitempty"`
	HTTPStatus    int       `json:"http_status,omitempty"`
	ResponseMS    int64     `json:"response_ms,omitempty"`
	RequestCount  uint64    `json:"request_count"`
	ResponseBytes uint64    `json:"response_bytes"`
}

type PublicRouteStatus struct {
	PublicRoute
	Health PublicRouteHealth `json:"health"`
}

type PublicRouteAccessLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Host      string    `json:"host"`
	ClientIP  string    `json:"client_ip"`
	Method    string    `json:"method"`
	URI       string    `json:"uri"`
	Protocol  string    `json:"protocol"`
	Status    int       `json:"status"`
	Size      uint64    `json:"size"`
	Duration  float64   `json:"duration"`
	UserAgent string    `json:"user_agent,omitempty"`
}

func (m *Manager) publicRoutesPath() string {
	return filepath.Join(m.stateDir, "public-routes.json")
}

func (m *Manager) loadPublicRoutes() {
	data, err := os.ReadFile(m.publicRoutesPath())
	if err != nil {
		if !os.IsNotExist(err) {
			publicRouteLog.Warn("Failed to read public-routes file", "err", err)
		}
		return
	}
	var routes []PublicRoute
	if err := json.Unmarshal(data, &routes); err != nil {
		publicRouteLog.Warn("Failed to parse public-routes file", "err", err)
		return
	}
	m.publicRoutesMu.Lock()
	m.publicRoutes = normalizePublicRouteList(routes)
	m.publicRoutesMu.Unlock()
}

func (m *Manager) savePublicRoutesLocked() error {
	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.publicRoutes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.publicRoutesPath(), data, 0644)
}

func normalizePublicRouteList(routes []PublicRoute) []PublicRoute {
	out := make([]PublicRoute, 0, len(routes))
	seen := make(map[string]bool, len(routes))
	for _, route := range routes {
		route.Host = normalizePublicRouteHost(route.Host)
		if route.Host == "" || seen[route.Host] {
			continue
		}
		seen[route.Host] = true
		if route.ID == "" {
			route.ID = publicRouteID(route.Host)
		}
		if route.Mode == "" {
			route.Mode = PublicRouteModeHomelab
		}
		if route.AuthMode == "" {
			route.AuthMode = PublicRouteAuthModeUpstream
		}
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

func publicRouteID(host string) string {
	id := strings.NewReplacer(".", "-", "_", "-", ":", "-").Replace(strings.ToLower(host))
	return strings.Trim(id, "-")
}

func homectlPublicHost() (string, error) {
	if host := normalizePublicRouteHost(os.Getenv("HOMECTL_PUBLIC_HOST")); host != "" {
		return host, nil
	}
	return "", errors.New("HOMECTL_PUBLIC_HOST is required")
}

func normalizePublicRouteHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	return host
}

func validatePublicRoute(route PublicRoute) error {
	publicHost, err := homectlPublicHost()
	if err != nil {
		return err
	}
	route.Host = normalizePublicRouteHost(route.Host)
	if route.Host == "" {
		return errors.New("host is required")
	}
	if !publicRouteHostPattern.MatchString(route.Host) {
		return fmt.Errorf("invalid host %q", route.Host)
	}
	if route.Host == publicHost {
		return fmt.Errorf("%s is reserved for the manager UI", publicHost)
	}
	if route.Host == "auth.example.net" && route.AuthMode == PublicRouteAuthModeAuthentik {
		return errors.New("auth.example.net must not use authentik auth mode")
	}
	switch route.AuthMode {
	case "", PublicRouteAuthModeAuthentik, PublicRouteAuthModeUpstream, PublicRouteAuthModePublic:
	default:
		return errors.New("auth_mode must be authentik, upstream, or public")
	}
	switch route.Mode {
	case PublicRouteModeHomelab:
		return nil
	case PublicRouteModeCustom:
		if route.CustomTarget == "" {
			return errors.New("custom_target is required")
		}
		u, err := url.Parse(route.CustomTarget)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return errors.New("custom_target must be an absolute URL")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return errors.New("custom_target must use http or https")
		}
		if u.Path != "" && u.Path != "/" {
			return errors.New("custom_target must be an origin URL without a path")
		}
		return nil
	default:
		return errors.New("mode must be homelab or custom")
	}
}

func (m *Manager) publicRouteStatuses() []PublicRouteStatus {
	m.publicRoutesMu.RLock()
	routes := append([]PublicRoute(nil), m.publicRoutes...)
	healthByID := make(map[string]PublicRouteHealth, len(m.routeHealth))
	for id, health := range m.routeHealth {
		healthByID[id] = health
	}
	m.publicRoutesMu.RUnlock()

	stats := m.readPublicRouteStats()
	out := make([]PublicRouteStatus, 0, len(routes))
	for _, route := range routes {
		health := healthByID[route.ID]
		if !route.Enabled {
			health.Status = "disabled"
			health.Working = false
		}
		if stat, ok := stats[route.Host]; ok {
			health.RequestCount = stat.RequestCount
			health.ResponseBytes = stat.ResponseBytes
		}
		out = append(out, PublicRouteStatus{PublicRoute: route, Health: health})
	}
	return out
}

func (m *Manager) StartPublicRouteLoop(ctx context.Context) {
	if crowdSecBouncerKey() != "" {
		publicRouteLog.Info("Deferring initial Caddy sync until CrowdSec bootstrap completes")
	} else if err := m.SyncCaddyConfig(); err != nil {
		publicRouteLog.Warn("Initial Caddy sync failed", "err", err)
	}
	m.refreshPublicRouteHealth(ctx)

	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.refreshPublicRouteHealth(ctx)
				if err := m.SyncCaddyConfig(); err != nil {
					publicRouteLog.Warn("Caddy sync failed", "err", err)
				}
			}
		}
	}()
}

func (m *Manager) refreshPublicRouteHealth(ctx context.Context) {
	m.publicRoutesMu.RLock()
	routes := append([]PublicRoute(nil), m.publicRoutes...)
	m.publicRoutesMu.RUnlock()

	publicRouteLog.Info("Checking public route health", "count", len(routes))

	health := make(map[string]PublicRouteHealth, len(routes))
	for _, route := range routes {
		func(r PublicRoute) {
			defer func() {
				if v := recover(); v != nil {
					publicRouteLog.Warn("Panic checking route", "host", r.Host, "panic", v)
				}
			}()
			health[r.ID] = m.checkPublicRoute(ctx, r)
		}(route)
	}
	m.publicRoutesMu.Lock()
	for id, v := range health {
		if v.CheckedAt.IsZero() {
			v.CheckedAt = time.Now()
			v.Status = "unhealthy"
			v.LastError = "health check did not complete"
		}
		m.routeHealth[id] = v
	}
	m.publicRoutesMu.Unlock()
	publicRouteLog.Info("Public route health updated", "count", len(health))
}

func (m *Manager) checkPublicRoute(ctx context.Context, route PublicRoute) PublicRouteHealth {
	result := PublicRouteHealth{Status: "unknown", CheckedAt: time.Now()}
	if !route.Enabled {
		result.Status = "disabled"
		return result
	}

	target, err := m.publicRouteTarget(route)
	if err != nil {
		result.Status = "unhealthy"
		result.LastError = err.Error()
		return result
	}
	result.ResolvedTo = target

	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, target, nil)
	if err != nil {
		result.Status = "unhealthy"
		result.LastError = err.Error()
		return result
	}
	if route.Mode == PublicRouteModeHomelab {
		req.Host = route.Host
		req.Header.Set("X-Homectl-Health-Check", "true")
	}
	// Route health checks are machine-generated probes, not user activity. Do
	// not leave a pooled Caddy -> app connection established between checks;
	// backup-sensitive apps use established connections to determine idleness.
	req.Close = true

	start := time.Now()
	client := *m.httpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if route.Mode == PublicRouteModeHomelab {
		client.Transport = &http.Transport{
			TLSClientConfig:   &tls.Config{ServerName: route.Host},
			DisableKeepAlives: true,
		}
	} else if transport, ok := client.Transport.(*http.Transport); ok {
		transport = transport.Clone()
		transport.DisableKeepAlives = true
		client.Transport = transport
	}
	resp, err := client.Do(req)
	result.ResponseMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = "unhealthy"
		result.LastError = err.Error()
		return result
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode
	if resp.StatusCode < 500 {
		result.Working = true
		result.Status = "healthy"
		return result
	}
	result.Status = "unhealthy"
	result.LastError = fmt.Sprintf("HTTP %d", resp.StatusCode)
	return result
}

func (m *Manager) publicRouteTarget(route PublicRoute) (string, error) {
	if route.Mode == PublicRouteModeCustom {
		u, err := url.Parse(route.CustomTarget)
		if err != nil {
			return "", err
		}
		u.Path = ""
		u.RawPath = ""
		u.RawQuery = ""
		u.Fragment = ""
		return resolveManagerLocalhostTarget(u.String()), nil
	}
	gateway := m.getGatewayNode()
	if gateway == "" {
		return "", errors.New("no gateway selected")
	}
	agent, ok := m.leaseManager.GetAgent(gateway)
	if !ok || time.Since(agent.LastSeen) > 90*time.Second {
		return "", errors.New("gateway offline")
	}
	if wgIP := m.agentWireGuardAddress(gateway); wgIP != "" {
		return "https://" + wgIP, nil
	}
	if agent.IP == "" {
		return "", errors.New("gateway IP unavailable")
	}
	return "https://" + agent.IP, nil
}

func resolveManagerLocalhostTarget(target string) string {
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return target
	}
	host := u.Hostname()
	if host != homectlLocalhostAlias && host != managerLocalhostAlias {
		return target
	}
	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		return target
	}
	if port := u.Port(); port != "" {
		u.Host = net.JoinHostPort(ips[0], port)
	} else {
		u.Host = ips[0]
	}
	return u.String()
}

func (m *Manager) agentWireGuardAddress(agentName string) string {
	peers, err := m.loadWireGuardPeers()
	if err != nil {
		publicRouteLog.Warn("Failed to read WireGuard peers", "err", err)
		return ""
	}
	for _, peer := range peers {
		if peer.Name == agentName {
			return strings.TrimSpace(peer.Address)
		}
	}
	return ""
}

func (m *Manager) SyncCaddyConfig() error {
	content, err := m.GenerateManagerCaddyfile()
	if err != nil {
		return err
	}
	return writeAndReloadManagerCaddyfile(content)
}

func (m *Manager) SyncManagerHostCaddyConfig() error {
	content, err := m.generateManagerCaddyfile(false)
	if err != nil {
		return err
	}
	return writeAndReloadManagerCaddyfile(content)
}

// writeAndReloadManagerCaddyfile atomically writes and validates the Caddyfile.
// It is serialized with caddySyncMu to prevent concurrent generation/reload races.
// The previous file is preserved on failure so the last known-good config is always available.
func writeAndReloadManagerCaddyfile(content string) error {
	caddySyncMu.Lock()
	defer caddySyncMu.Unlock()

	path := managerCaddyfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(managerCaddyAccessLogPath()), 0755); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, []byte(content)) {
		if isCaddyRunning() {
			return nil
		}
	}

	// Save previous config for rollback on failure
	var backup []byte
	if existing, err := os.ReadFile(path); err == nil {
		backup = make([]byte, len(existing))
		copy(backup, existing)
	}

	// Write to temp file, then validate, then atomically rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return err
	}
	defer os.Remove(tmpPath) // clean up on any exit path

	// Validate with Caddy (dry-run check)
	caddyCmdMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, caddyBinary, "validate", "--config", tmpPath, "--adapter", "caddyfile")
	validateOutput, validateErr := cmd.CombinedOutput()
	caddyCmdMu.Unlock()
	if validateErr != nil {
		caddyContainerLog.Warn("Caddy config validation failed, restoring previous file",
			"err", validateErr, "output", string(validateOutput))
		os.Rename(tmpPath, tmpPath+".invalid") // preserve invalid config for debugging
		if backup != nil {
			os.WriteFile(path, backup, 0644) // restore previous working config
		} else {
			os.Remove(tmpPath) // nothing to restore
		}
		return fmt.Errorf("caddy config validation failed: %s", validateOutput)
	}

	// Validate passed, atomically rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		if backup != nil {
			os.WriteFile(path, backup, 0644) // restore previous working config
		}
		return err
	}

	if err := ensureCaddy(); err != nil {
		return err
	}
	caddyRestartW.Lock()
	defer caddyRestartW.Unlock()
	if err := reloadCaddy(); err != nil {
		// Restore previous config on reload failure
		if backup != nil {
			os.WriteFile(path, backup, 0644)
		}
		return err
	}
	return nil
}

func (m *Manager) GenerateManagerCaddyfile() (string, error) {
	return m.generateManagerCaddyfile(true)
}

func (m *Manager) generateManagerCaddyfile(includePublicRoutes bool) (string, error) {
	m.publicRoutesMu.RLock()
	routes := append([]PublicRoute(nil), m.publicRoutes...)
	m.publicRoutesMu.RUnlock()

	var b strings.Builder
	b.WriteString("{\n")
	if email := strings.TrimSpace(m.configValue(common.ConfigKeyCaddyACMEEmail)); email != "" {
		fmt.Fprintf(&b, "\temail %s\n", email)
	}
	crowdSecKey := ""
	if includePublicRoutes {
		crowdSecKey = crowdSecBouncerKey()
	}
	if crowdSecKey != "" {
		b.WriteString("\tcrowdsec {\n")
		b.WriteString("\t\tapi_url http://127.0.0.1:8080\n")
		fmt.Fprintf(&b, "\t\tapi_key %s\n", crowdSecKey)
		b.WriteString("\t\tticker_interval 10s\n")
		b.WriteString("\t}\n")
		b.WriteString("\tservers {\n")
		b.WriteString("\t\ttrusted_proxies static private_ranges\n")
		b.WriteString("\t\tclient_ip_headers X-Forwarded-For X-Real-IP\n")
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n\n")
	writeAuthentikForwardAuthSnippet(&b, m.configValue(common.ConfigKeyAuthentikOutpost))

	writeAccessLog := func() {
		fmt.Fprintf(&b, "\tlog {\n\t\toutput file %s {\n\t\t\troll_size 10MiB\n\t\t\troll_keep 5\n\t\t}\n\t\tformat json\n\t}\n", managerCaddyAccessLogPath())
	}

	publicHost, err := homectlPublicHost()
	if err != nil {
		return "", err
	}

	b.WriteString("# Auth mode: upstream\n")
	fmt.Fprintf(&b, "%s {\n", publicHost)
	writeAccessLog()
	if crowdSecKey != "" {
		b.WriteString("\troute {\n")
		b.WriteString("\t\tcrowdsec\n")
		b.WriteString("\t\treverse_proxy 127.0.0.1:1338\n")
		b.WriteString("\t}\n")
	} else {
		b.WriteString("\treverse_proxy 127.0.0.1:1338\n")
	}
	b.WriteString("}\n\n")

	if !includePublicRoutes {
		return b.String(), nil
	}

	sort.Slice(routes, func(i, j int) bool { return routes[i].Host < routes[j].Host })
	for _, route := range routes {
		if !route.Enabled {
			continue
		}
		if err := validatePublicRoute(route); err != nil {
			return "", err
		}
		target, err := m.publicRouteTarget(route)
		if err != nil && route.Mode == PublicRouteModeHomelab {
			continue
		}
		if err != nil {
			return "", err
		}

		authMode := normalizePublicRouteAuthMode(route.AuthMode)
		fmt.Fprintf(&b, "# Auth mode: %s\n", authMode)
		if route.Host == "auth.example.net" {
			b.WriteString("# Authentik route; do not protect this with authentik_forward_auth or login can break.\n")
		}
		fmt.Fprintf(&b, "%s {\n", route.Host)
		writeAccessLog()
		needsRoute := crowdSecKey != "" || authMode == PublicRouteAuthModeAuthentik
		if needsRoute {
			b.WriteString("\troute {\n")
		}
		if crowdSecKey != "" {
			b.WriteString("\t\tcrowdsec\n")
		}
		if authMode == PublicRouteAuthModeAuthentik {
			b.WriteString("\t\timport authentik_forward_auth\n")
		}
		indent := "\t"
		if needsRoute {
			indent = "\t\t"
		}
		fmt.Fprintf(&b, "%sreverse_proxy %s {\n", indent, target)
		if route.Mode == PublicRouteModeHomelab {
			fmt.Fprintf(&b, "%s\theader_up Host {host}\n", indent)
			if route.Host == "auth.example.net" {
				fmt.Fprintf(&b, "%s\theader_up X-Forwarded-Host {header.X-Forwarded-Host}\n", indent)
			} else {
				fmt.Fprintf(&b, "%s\theader_up X-Forwarded-Host {host}\n", indent)
			}
			fmt.Fprintf(&b, "%s\ttransport http {\n%s\t\ttls_server_name %s\n%s\t}\n", indent, indent, route.Host, indent)
		}
		fmt.Fprintf(&b, "%s}\n", indent)
		if needsRoute {
			b.WriteString("\t}\n")
		}
		b.WriteString("}\n\n")
	}

	return b.String(), nil
}

func normalizePublicRouteAuthMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case PublicRouteAuthModeAuthentik:
		return PublicRouteAuthModeAuthentik
	case PublicRouteAuthModePublic:
		return PublicRouteAuthModePublic
	default:
		return PublicRouteAuthModeUpstream
	}
}

func writeAuthentikForwardAuthSnippet(b *strings.Builder, outpostURL string) {
	outpostURL = strings.TrimSpace(outpostURL)
	if outpostURL == "" {
		outpostURL = "{$AUTHENTIK_OUTPOST_URL:https://auth.example.net}"
	}
	b.WriteString("(authentik_forward_auth) {\n")
	fmt.Fprintf(b, "\treverse_proxy /outpost.goauthentik.io/* %s\n\n", outpostURL)
	b.WriteString("\trequest_header -X-Authentik-Username\n")
	b.WriteString("\trequest_header -X-Authentik-Groups\n")
	b.WriteString("\trequest_header -X-Authentik-Email\n")
	b.WriteString("\trequest_header -X-Authentik-Name\n")
	b.WriteString("\trequest_header -X-Authentik-Uid\n")
	b.WriteString("\trequest_header -X-Authentik-Jwt\n")
	b.WriteString("\trequest_header -X-Authentik-Meta-Jwks\n")
	b.WriteString("\trequest_header -X-Authentik-Meta-Outpost\n")
	b.WriteString("\trequest_header -X-Authentik-Meta-Provider\n")
	b.WriteString("\trequest_header -X-Authentik-Meta-App\n")
	b.WriteString("\trequest_header -X-Authentik-Meta-Version\n\n")
	fmt.Fprintf(b, "\tforward_auth %s {\n", outpostURL)
	b.WriteString("\t\turi /outpost.goauthentik.io/auth/caddy\n")
	b.WriteString("\t\theader_up X-Forwarded-Host {host}\n")
	b.WriteString("\t\tcopy_headers X-Authentik-Username X-Authentik-Groups X-Authentik-Email X-Authentik-Name X-Authentik-Uid X-Authentik-Jwt X-Authentik-Meta-Jwks X-Authentik-Meta-Outpost X-Authentik-Meta-Provider X-Authentik-Meta-App X-Authentik-Meta-Version\n")
	b.WriteString("\t\ttrusted_proxies private_ranges\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n\n")
}

type routeStats struct {
	RequestCount  uint64
	ResponseBytes uint64
}

func (m *Manager) readPublicRouteStats() map[string]routeStats {
	path := managerCaddyAccessLogPath()
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	stats := make(map[string]routeStats)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry struct {
			Request struct {
				Host string `json:"host"`
				Size uint64 `json:"size"`
			} `json:"request"`
			Size uint64 `json:"size"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		host := normalizePublicRouteHost(entry.Request.Host)
		if host == "" {
			continue
		}
		stat := stats[host]
		stat.RequestCount++
		stat.ResponseBytes += entry.Size
		stats[host] = stat
	}
	return stats
}

func parsePublicRouteAccessLogLine(line []byte) (PublicRouteAccessLogEntry, bool) {
	var raw struct {
		Timestamp json.Number `json:"ts"`
		Request   struct {
			ClientIP string              `json:"client_ip"`
			RemoteIP string              `json:"remote_ip"`
			Proto    string              `json:"proto"`
			Method   string              `json:"method"`
			Host     string              `json:"host"`
			URI      string              `json:"uri"`
			Headers  map[string][]string `json:"headers"`
		} `json:"request"`
		Duration float64 `json:"duration"`
		Size     uint64  `json:"size"`
		Status   int     `json:"status"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return PublicRouteAccessLogEntry{}, false
	}
	host := normalizePublicRouteHost(raw.Request.Host)
	if host == "" {
		return PublicRouteAccessLogEntry{}, false
	}
	clientIP := strings.TrimSpace(raw.Request.ClientIP)
	if clientIP == "" {
		clientIP = strings.TrimSpace(raw.Request.RemoteIP)
	}
	entry := PublicRouteAccessLogEntry{
		Timestamp: parseCaddyLogTimestamp(raw.Timestamp),
		Host:      host,
		ClientIP:  clientIP,
		Method:    raw.Request.Method,
		URI:       raw.Request.URI,
		Protocol:  raw.Request.Proto,
		Status:    raw.Status,
		Size:      raw.Size,
		Duration:  raw.Duration,
		UserAgent: firstHeaderValue(raw.Request.Headers, "User-Agent"),
	}
	return entry, true
}

func parseCaddyLogTimestamp(value json.Number) time.Time {
	if value == "" {
		return time.Time{}
	}
	seconds, err := value.Float64()
	if err != nil {
		return time.Time{}
	}
	whole := int64(seconds)
	nanos := int64((seconds - float64(whole)) * 1_000_000_000)
	return time.Unix(whole, nanos).UTC()
}

func firstHeaderValue(headers map[string][]string, name string) string {
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func (m *Manager) requirePublicHostBasicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicHost, err := homectlPublicHost()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		if normalizePublicRouteHost(r.Host) != publicHost {
			next.ServeHTTP(w, r)
			return
		}
		// Public API routes are consumed by the browser and agents directly.
		// Keep basic auth on the UI surface; /api/join is token-authenticated.
		if strings.HasPrefix(r.URL.Path, "/api/v1/") || r.URL.Path == "/api/join" {
			next.ServeHTTP(w, r)
			return
		}
		user := os.Getenv("ADMIN_USER")
		pass := os.Getenv("ADMIN_PASSWORD")
		if user == "" || pass == "" {
			http.Error(w, "manager public auth is not configured", http.StatusServiceUnavailable)
			return
		}
		gotUser, gotPass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(gotUser), []byte(user)) != 1 || subtle.ConstantTimeCompare([]byte(gotPass), []byte(pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="homectl"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
