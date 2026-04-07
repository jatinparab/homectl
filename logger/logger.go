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
//	// Existing log.Printf calls automatically get the component/node tags
//	// because Init calls slog.SetDefault, which also redirects log.Printf.
//
// Future extensions: switch to JSON output by changing the handler in Init,
// or add a level flag to toggle Debug output — no call sites need to change.
package logger

import (
	"log/slog"
	"os"
)

// Logger wraps *slog.Logger and adds homectl-specific helpers.
// The embedded *slog.Logger is accessible directly for advanced usage.
type Logger struct {
	*slog.Logger
}

// Init creates a global logger tagged with component ("agent" or "manager") and
// an optional node name, installs it as the slog default, and redirects the
// standard library log.Printf to it.  Call once at process startup.
func Init(component, node string) *Logger {
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
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
// Overrides the embedded method to return *Logger instead of *slog.Logger.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{l.Logger.With(args...)}
}

// cleanAttrs produces compact, readable output:
//
//	time=17:00:47.123 level=INF component=agent node=k8s-node-1 area=backup msg="..." app=nginx
func cleanAttrs(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		return slog.String("time", a.Value.Time().Format("15:04:05.000"))
	case slog.LevelKey:
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
