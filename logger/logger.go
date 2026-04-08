// Package logger provides structured, tagged logging for homectl.
//
// Usage:
//
//	// At startup (agent or manager):
//	log := logger.Init("agent", hostname)
//
//	// Create area-scoped loggers:
//	backupLog := log.Area("backup")
//	backupLog.Info("starting backup", "app", appID)
//
// All log lines are written to stderr as JSON and also captured in Buffer,
// a global ring buffer that the HTTP layer can stream to the UI without
// any journalctl dependency.
package logger

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Logger wraps *slog.Logger and adds homectl-specific helpers.
type Logger struct {
	*slog.Logger
}

// Buffer is the global in-process log ring buffer, populated by Init.
// Server code can call Buffer.Snapshot() for recent history and
// Buffer.Follow() for a live subscription channel.
var Buffer = newLogRingBuffer(500)

// parseLevel parses a LOG_LEVEL string into a slog.Level, defaulting to Info.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "dbg":
		return slog.LevelDebug
	case "warn", "warning", "wrn":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Init creates a global logger tagged with component ("agent" or "manager") and
// an optional node name, installs it as the slog default, and redirects the
// standard library log.Printf to it. Call once at process startup.
// The minimum log level is read from the LOG_LEVEL environment variable (default: info).
func Init(component, node string) *Logger {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
	w := &logTee{out: os.Stderr, ring: Buffer}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: cleanAttrs,
	})

	attrs := []any{slog.String("component", component)}
	if node != "" {
		attrs = append(attrs, slog.String("node", node))
	}

	base := slog.New(h).With(attrs...)
	slog.SetDefault(base)
	return &Logger{base}
}

// Area returns a child logger tagged with the given area
// (e.g. "backup", "reconcile", "docker", "wireguard").
func (l *Logger) Area(area string) *Logger {
	return &Logger{l.Logger.With(slog.String("area", area))}
}

// App returns a child logger additionally tagged with an app ID.
func (l *Logger) App(app string) *Logger {
	return &Logger{l.Logger.With(slog.String("app", app))}
}

// With returns a child logger with additional key-value attributes.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{l.Logger.With(args...)}
}

// cleanAttrs normalises log attributes for JSON output.
// Time is kept as RFC3339Nano so the frontend can parse it.
// Level is abbreviated to 3 chars: DBG / INF / WRN / ERR.
func cleanAttrs(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.LevelKey {
		switch l := a.Value.Any().(slog.Level); {
		case l < slog.LevelInfo:
			return slog.String("level", "DBG")
		case l < slog.LevelWarn:
			return slog.String("level", "INF")
		case l < slog.LevelError:
			return slog.String("level", "WRN")
		default:
			return slog.String("level", "ERR")
		}
	}
	return a
}

// ── Ring buffer ────────────────────────────────────────────────────

type logRingBuffer struct {
	mu        sync.Mutex
	ring      []string
	pos       int
	count     int
	followers map[chan string]struct{}
}

func newLogRingBuffer(n int) *logRingBuffer {
	return &logRingBuffer{
		ring:      make([]string, n),
		followers: make(map[chan string]struct{}),
	}
}

func (b *logRingBuffer) write(line string) {
	b.mu.Lock()
	b.ring[b.pos] = line
	b.pos = (b.pos + 1) % len(b.ring)
	if b.count < len(b.ring) {
		b.count++
	}
	followers := make([]chan string, 0, len(b.followers))
	for ch := range b.followers {
		followers = append(followers, ch)
	}
	b.mu.Unlock()

	for _, ch := range followers {
		select {
		case ch <- line:
		default: // drop if follower is slow
		}
	}
}

// Snapshot returns up to the last N lines in chronological order.
func (b *logRingBuffer) Snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := b.count
	if n == 0 {
		return nil
	}
	lines := make([]string, 0, n)
	start := (b.pos - n + len(b.ring)) % len(b.ring)
	for i := 0; i < n; i++ {
		lines = append(lines, b.ring[(start+i)%len(b.ring)])
	}
	return lines
}

// Follow subscribes to new lines. Call Unfollow when done.
func (b *logRingBuffer) Follow() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.followers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unfollow removes a subscriber and closes its channel.
func (b *logRingBuffer) Unfollow(ch chan string) {
	b.mu.Lock()
	delete(b.followers, ch)
	b.mu.Unlock()
}

// ── Tee writer ─────────────────────────────────────────────────────

// logTee writes to an underlying io.Writer (stderr) and extracts
// complete lines to write into the ring buffer.
type logTee struct {
	out  io.Writer
	ring *logRingBuffer
	mu   sync.Mutex
	buf  []byte
}

func (t *logTee) Write(p []byte) (int, error) {
	n, err := t.out.Write(p)

	t.mu.Lock()
	t.buf = append(t.buf, p...)
	for {
		i := bytes.IndexByte(t.buf, '\n')
		if i < 0 {
			break
		}
		line := string(t.buf[:i])
		t.buf = t.buf[i+1:]
		if line != "" {
			t.ring.write(line)
		}
	}
	t.mu.Unlock()

	return n, err
}
