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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var publicRouteLog = slog.With("area", "public-routes")

const (
	PublicRouteModeHomelab = "homelab"
	PublicRouteModeCustom  = "custom"

	homectlLocalhostAlias  = "homectl-localhost"
	managerLocalhostAlias  = "manager-localhost"
	publicRouteLogsAPIPath = "/api/v1/public-route-logs"
)

var publicRouteHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

type PublicRoute struct {
	ID           string    `json:"id"`
	Host         string    `json:"host"`
	Mode         string    `json:"mode"`
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
	if err := m.SyncCaddyConfig(); err != nil {
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
	}

	start := time.Now()
	client := *m.httpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if route.Mode == PublicRouteModeHomelab {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{ServerName: route.Host},
		}
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
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	if err := ensureCaddy(); err != nil {
		return err
	}
	if err := reloadCaddy(); err != nil {
		return err
	}
	return nil
}

func (m *Manager) GenerateManagerCaddyfile() (string, error) {
	m.publicRoutesMu.RLock()
	routes := append([]PublicRoute(nil), m.publicRoutes...)
	m.publicRoutesMu.RUnlock()

	var b strings.Builder
	b.WriteString("{\n")
	if email := strings.TrimSpace(os.Getenv("CADDY_ACME_EMAIL")); email != "" {
		fmt.Fprintf(&b, "\temail %s\n", email)
	}
	crowdSecKey := crowdSecBouncerKey()
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

	writeAccessLog := func() {
		fmt.Fprintf(&b, "\tlog {\n\t\toutput file %s {\n\t\t\troll_size 10MiB\n\t\t\troll_keep 5\n\t\t}\n\t\tformat json\n\t}\n", managerCaddyAccessLogPath())
	}

	publicHost, err := homectlPublicHost()
	if err != nil {
		return "", err
	}

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

		fmt.Fprintf(&b, "%s {\n", route.Host)
		writeAccessLog()
		if crowdSecKey != "" {
			b.WriteString("\troute {\n")
			b.WriteString("\t\tcrowdsec\n")
			fmt.Fprintf(&b, "\t\treverse_proxy %s {\n", target)
		} else {
			fmt.Fprintf(&b, "\treverse_proxy %s {\n", target)
		}
		if route.Mode == PublicRouteModeHomelab {
			if crowdSecKey != "" {
				b.WriteString("\t\t\theader_up Host {host}\n")
				b.WriteString("\t\t\theader_up X-Forwarded-Host {host}\n")
				fmt.Fprintf(&b, "\t\t\ttransport http {\n\t\t\t\ttls_server_name %s\n\t\t\t}\n", route.Host)
			} else {
				b.WriteString("\t\theader_up Host {host}\n")
				b.WriteString("\t\theader_up X-Forwarded-Host {host}\n")
				fmt.Fprintf(&b, "\t\ttransport http {\n\t\t\ttls_server_name %s\n\t\t}\n", route.Host)
			}
		}
		if crowdSecKey != "" {
			b.WriteString("\t\t}\n")
			b.WriteString("\t}\n")
		} else {
			b.WriteString("\t}\n")
		}
		b.WriteString("}\n\n")
	}

	return b.String(), nil
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
