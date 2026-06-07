package manager

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLogLimit = 200
	maxLogLimit     = 1000
)

// LogFilter is a reusable filter for log queries across different log types.
type LogFilter struct {
	Host   string
	IP     string
	URI    string
	Status string
	Search string
	Limit  int
	Before time.Time
	After  time.Time
}

func parseLogFilters(r *http.Request) LogFilter {
	f := LogFilter{Limit: defaultLogLimit}

	if raw := strings.TrimSpace(r.URL.Query().Get("host")); raw != "" {
		f.Host = normalizePublicRouteHost(raw)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("ip")); raw != "" {
		f.IP = strings.ToLower(raw)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("uri")); raw != "" {
		f.URI = raw
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		f.Status = raw
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("search")); raw != "" {
		f.Search = strings.ToLower(raw)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			f.Limit = parsed
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			f.Before = ts
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			f.After = ts
		}
	}

	if f.Limit <= 0 || f.Limit > maxLogLimit {
		f.Limit = maxLogLimit
	}
	return f
}

func (f LogFilter) toQueryString() string {
	var parts []string
	if f.Host != "" {
		parts = append(parts, "host="+f.Host)
	}
	if f.IP != "" {
		parts = append(parts, "ip="+f.IP)
	}
	if f.URI != "" {
		parts = append(parts, "uri="+f.URI)
	}
	if f.Status != "" {
		parts = append(parts, "status="+f.Status)
	}
	if f.Search != "" {
		parts = append(parts, "search="+f.Search)
	}
	if f.Limit > 0 && f.Limit != defaultLogLimit {
		parts = append(parts, fmt.Sprintf("limit=%d", f.Limit))
	}
	if !f.Before.IsZero() {
		parts = append(parts, "before="+f.Before.Format(time.RFC3339))
	}
	if !f.After.IsZero() {
		parts = append(parts, "after="+f.After.Format(time.RFC3339))
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}
