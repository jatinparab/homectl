package manager

import (
	"bufio"
	"os"
	"strings"
)

// readPublicRouteAccessLogs reads Caddy access logs with full server-side filtering.
func readPublicRouteAccessLogs(filter LogFilter) []PublicRouteAccessLogEntry {
	path := managerCaddyAccessLogPath()
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	entries := make([]PublicRouteAccessLogEntry, 0, filter.Limit)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		entry, ok := parsePublicRouteAccessLogLine(scanner.Bytes())
		if !ok {
			continue
		}
		if strings.HasPrefix(entry.URI, publicRouteLogsAPIPath) {
			continue
		}
		if !filter.matchesAccessLog(entry) {
			continue
		}
		entries = append(entries, entry)
		if len(entries) > filter.Limit {
			copy(entries, entries[1:])
			entries = entries[:filter.Limit]
		}
	}

	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}

func (f LogFilter) matchesAccessLog(entry PublicRouteAccessLogEntry) bool {
	if f.Host != "" && !strings.Contains(entry.Host, f.Host) {
		return false
	}
	if f.IP != "" && !strings.Contains(strings.ToLower(entry.ClientIP), f.IP) {
		return false
	}
	if f.URI != "" && !strings.Contains(strings.ToLower(entry.URI), strings.ToLower(f.URI)) {
		return false
	}
	if f.Status != "" && !matchStatus(entry.Status, f.Status) {
		return false
	}
	if f.Search != "" {
		if !strings.Contains(strings.ToLower(entry.Host), f.Search) &&
			!strings.Contains(strings.ToLower(entry.ClientIP), f.Search) &&
			!strings.Contains(strings.ToLower(entry.URI), f.Search) &&
			!strings.Contains(strings.ToLower(entry.UserAgent), f.Search) &&
			!strings.Contains(strings.ToLower(entry.Method), f.Search) {
			return false
		}
	}
	if !f.Before.IsZero() && entry.Timestamp.After(f.Before) {
		return false
	}
	if !f.After.IsZero() && entry.Timestamp.Before(f.After) {
		return false
	}
	return true
}

func matchStatus(entryStatus int, statusFilter string) bool {
	if statusFilter == "" {
		return true
	}
	category := strings.ToUpper(strings.TrimSpace(statusFilter))
	switch category {
	case "2XX":
		return entryStatus >= 200 && entryStatus < 300
	case "3XX":
		return entryStatus >= 300 && entryStatus < 400
	case "4XX":
		return entryStatus >= 400 && entryStatus < 500
	case "5XX":
		return entryStatus >= 500 && entryStatus < 600
	default:
		return strings.EqualFold(strconvItoa(entryStatus), category)
	}
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 3)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}


