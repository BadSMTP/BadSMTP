//go:build !windows

package logging

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"log/syslog"
	"strings"
	"sync"
)

// NewSyslogLogger creates a syslog logger on unix-like systems.
func NewSyslogLogger(config *LogConfig) (Logger, error) {
	priority := syslog.LOG_INFO
	switch config.SyslogFacility {
	case "mail":
		priority |= syslog.LOG_MAIL
	case "daemon":
		priority |= syslog.LOG_DAEMON
	case "local0":
		priority |= syslog.LOG_LOCAL0
	case "local1":
		priority |= syslog.LOG_LOCAL1
	case "local2":
		priority |= syslog.LOG_LOCAL2
	case "local3":
		priority |= syslog.LOG_LOCAL3
	case "local4":
		priority |= syslog.LOG_LOCAL4
	case "local5":
		priority |= syslog.LOG_LOCAL5
	case "local6":
		priority |= syslog.LOG_LOCAL6
	case "local7":
		priority |= syslog.LOG_LOCAL7
	default:
		priority |= syslog.LOG_MAIL
	}

	writer, err := syslog.New(priority, "badsmtp")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to syslog: %w", err)
	}

	level := newLevelVar(config)
	return newSlogLoggerFromHandler(newSyslogHandler(writer, config, level), level), nil
}

// syslogHandler formats records with a delegate JSON/text handler and routes the
// result to the matching syslog severity. Derived handlers (WithAttrs/WithGroup)
// share the formatting buffer and its guard, so concurrent writes are serialised.
type syslogHandler struct {
	writer *syslog.Writer
	mu     *sync.Mutex
	buf    *bytes.Buffer
	inner  slog.Handler
}

func newSyslogHandler(w *syslog.Writer, config *LogConfig, level *slog.LevelVar) slog.Handler {
	buf := &bytes.Buffer{}
	return &syslogHandler{
		writer: w,
		mu:     &sync.Mutex{},
		buf:    buf,
		inner:  newHandler(buf, config, level),
	}
}

func (h *syslogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

//nolint:gocritic // slog.Handler.Handle requires Record to be passed by value
func (h *syslogHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.buf.Reset()
	if err := h.inner.Handle(ctx, r); err != nil {
		return err
	}
	msg := strings.TrimRight(h.buf.String(), "\n")

	switch {
	case r.Level >= slog.LevelError:
		return h.writer.Err(msg)
	case r.Level >= slog.LevelWarn:
		return h.writer.Warning(msg)
	case r.Level >= slog.LevelInfo:
		return h.writer.Info(msg)
	default:
		return h.writer.Debug(msg)
	}
}

func (h *syslogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &syslogHandler{writer: h.writer, mu: h.mu, buf: h.buf, inner: h.inner.WithAttrs(attrs)}
}

func (h *syslogHandler) WithGroup(name string) slog.Handler {
	return &syslogHandler{writer: h.writer, mu: h.mu, buf: h.buf, inner: h.inner.WithGroup(name)}
}
