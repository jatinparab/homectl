package manager

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// SSHLogEntry represents a parsed SSH authentication log line.
type SSHLogEntry struct {
	Timestamp  string `json:"timestamp"`
	Host       string `json:"host"`
	ClientIP   string `json:"client_ip"`
	Port       string `json:"port"`
	User       string `json:"user"`
	AuthMethod string `json:"auth_method,omitempty"`
	Status     string `json:"status"` // "accepted", "failed", "error", "disconnected", "other"
	Message    string `json:"message"`
	Raw        string `json:"raw"`
}

var (
	// Jun 07 10:15:30 hostname sshd[12345]: ...
	sshdLogLineRe = regexp.MustCompile(`^(\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})\s+(\S+)\s+sshd\[\d+\]:\s+(.*)`)

	// Accepted publickey for user from 1.2.3.4 port 56789 ...
	sshdAcceptedRe = regexp.MustCompile(`^Accepted\s+(\S+)\s+for\s+(\S+)\s+from\s+(\S+)\s+port\s+(\d+)`)

	// Failed password for user from 1.2.3.4 port 56789 ...
	sshdFailedPasswordRe = regexp.MustCompile(`^Failed\s+password\s+for\s+(\S+)\s+from\s+(\S+)\s+port\s+(\d+)`)

	// Failed publickey for user from 1.2.3.4 port 56789 ...
	sshdFailedPublicKeyRe = regexp.MustCompile(`^Failed\s+publickey\s+for\s+(\S+)\s+from\s+(\S+)\s+port\s+(\d+)`)

	// Connection closed by authenticating user ...
	sshdDisconnectedRe = regexp.MustCompile(`^Connection\s+closed\s+by\s+(\S+)\s+(\S+)\s+(\S+)\s+\[preauth\]`)
	sshdDisconnected2Re    = regexp.MustCompile(`^Disconnected\s+from\s+(\S+)\s+port\s+(\d+)`)
	sshdDisconnectedUserRe = regexp.MustCompile(`^Disconnected\s+from\s+user\s+(\S+)\s+(\S+)\s+port\s+(\d+)`)

	// Invalid user ...
	sshdInvalidUserRe = regexp.MustCompile(`^Invalid\s+user\s+(\S+)\s+from\s+(\S+)\s+port\s+(\d+)`)

	// Connection reset by ...
	sshdConnectionResetRe = regexp.MustCompile(`^Connection\s+reset\s+by\s+(\S+)\s+port\s+(\d+)`)

	// error: ...
	sshdErrorRe = regexp.MustCompile(`^(?:Received\s+disconnect|error:|fatal:|Bad\s+protocol|Did\s+not\s+receive)`)

	// pam_unix lines (extra info after auth)
	sshdPAMRe = regexp.MustCompile(`^pam_unix\(sshd:(?:auth|session)\):\s+(.*)`)
)

// readSSHLogs reads and parses SSH authentication logs from the configured path.
func readSSHLogs(filter LogFilter) []SSHLogEntry {
	sshLogPath := crowdSecSSHLogPath()
	if sshLogPath == "" {
		return nil
	}

	f, err := os.Open(sshLogPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	entries := make([]SSHLogEntry, 0, filter.Limit)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	now := time.Now()
	currentYear := now.Year()

	for scanner.Scan() {
		line := scanner.Text()
		entry, ok := parseSSHLogLine(line, currentYear)
		if !ok {
			continue
		}
		if !filter.matchesSSHLog(entry) {
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

func parseSSHLogLine(line string, currentYear int) (SSHLogEntry, bool) {
	matches := sshdLogLineRe.FindStringSubmatch(line)
	if len(matches) < 4 {
		return SSHLogEntry{}, false
	}

	rawTime := matches[1]
	host := matches[2]
	message := matches[3]

	timestamp := parseSyslogTimestamp(rawTime, currentYear)

	entry := SSHLogEntry{
		Timestamp: timestamp,
		Host:      host,
		Message:   message,
		Raw:       line,
		Status:    "other",
	}

	if m := sshdAcceptedRe.FindStringSubmatch(message); len(m) >= 5 {
		entry.AuthMethod = m[1]
		entry.User = m[2]
		entry.ClientIP = m[3]
		entry.Port = m[4]
		entry.Status = "accepted"
	} else if m := sshdFailedPasswordRe.FindStringSubmatch(message); len(m) >= 4 {
		entry.User = m[1]
		entry.ClientIP = m[2]
		entry.Port = m[3]
		entry.Status = "failed"
		entry.AuthMethod = "password"
	} else if m := sshdFailedPublicKeyRe.FindStringSubmatch(message); len(m) >= 4 {
		entry.User = m[1]
		entry.ClientIP = m[2]
		entry.Port = m[3]
		entry.Status = "failed"
		entry.AuthMethod = "publickey"
	} else if m := sshdInvalidUserRe.FindStringSubmatch(message); len(m) >= 4 {
		entry.User = m[1]
		entry.ClientIP = m[2]
		entry.Port = m[3]
		entry.Status = "failed"
	} else if m := sshdDisconnectedRe.FindStringSubmatch(message); len(m) >= 4 {
		entry.User = m[2]
		entry.ClientIP = m[3]
		entry.Status = "disconnected"
	} else if m := sshdDisconnectedUserRe.FindStringSubmatch(message); len(m) >= 4 {
		entry.User = m[1]
		entry.ClientIP = m[2]
		entry.Port = m[3]
		entry.Status = "disconnected"
	} else if m := sshdDisconnected2Re.FindStringSubmatch(message); len(m) >= 3 {
		entry.ClientIP = m[1]
		entry.Port = m[2]
		entry.Status = "disconnected"
	} else if m := sshdConnectionResetRe.FindStringSubmatch(message); len(m) >= 3 {
		entry.ClientIP = m[1]
		entry.Port = m[2]
		entry.Status = "disconnected"
	} else if sshdErrorRe.MatchString(message) {
		entry.Status = "error"
		if m := sshdConnectionResetRe.FindStringSubmatch(message); len(m) >= 3 {
			entry.ClientIP = m[1]
			entry.Port = m[2]
		}
	} else if sshdPAMRe.MatchString(message) {
		entry.Status = "other"
	}

	return entry, true
}

func parseSyslogTimestamp(raw string, currentYear int) string {
	// Format: "Jun  7 10:15:30" or "Jun 07 10:15:30"
	raw = strings.TrimSpace(raw)
	// Normalize single-digit days
	parts := strings.Fields(raw)
	if len(parts) < 3 {
		return ""
	}
	month := parts[0]
	day := parts[1]
	timeStr := parts[2]

	t, err := time.Parse("Jan 2 15:04:05", fmt.Sprintf("%s %s %s", month, day, timeStr))
	if err != nil {
		return ""
	}
	// Set year to current year
	t = time.Date(currentYear, t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
	return t.Format(time.RFC3339)
}

func (f LogFilter) matchesSSHLog(entry SSHLogEntry) bool {
	if f.IP != "" && !strings.Contains(strings.ToLower(entry.ClientIP), f.IP) {
		return false
	}
	if f.Status != "" {
		switch parseStatusCategory(f.Status) {
		case "accepted":
			if entry.Status != "accepted" {
				return false
			}
		case "failed":
			if entry.Status != "failed" {
				return false
			}
		case "error":
			if entry.Status != "error" && entry.Status != "disconnected" {
				return false
			}
		default:
			if entry.Status != f.Status {
				return false
			}
		}
	}
	if f.Search != "" {
		search := f.Search
		if !strings.Contains(strings.ToLower(entry.ClientIP), search) &&
			!strings.Contains(strings.ToLower(entry.User), search) &&
			!strings.Contains(strings.ToLower(entry.Message), search) &&
			!strings.Contains(strings.ToLower(entry.AuthMethod), search) {
			return false
		}
	}
	if f.Host != "" && !strings.Contains(entry.Host, f.Host) {
		return false
	}
	if !f.Before.IsZero() {
		ts, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err == nil && ts.After(f.Before) {
			return false
		}
	}
	if !f.After.IsZero() {
		ts, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err == nil && ts.Before(f.After) {
			return false
		}
	}
	return true
}

func parseStatusCategory(status string) string {
	switch strings.ToLower(status) {
	case "accepted", "success":
		return "accepted"
	case "failed", "fail", "failure":
		return "failed"
	case "error", "disconnected":
		return "error"
	default:
		return status
	}
}
