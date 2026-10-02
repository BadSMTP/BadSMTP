// Package logging provides structured logging for BadSMTP server
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
)

// remoteDialTimeout bounds how long a remote log connection attempt may take.
const remoteDialTimeout = 2 * time.Second

// LogLevel represents the logging level
type LogLevel int

const (
	// DEBUG level for debug messages
	DEBUG LogLevel = iota
	// INFO level for information messages
	INFO
	// WARN level for warning messages
	WARN
	// ERROR level for error messages
	ERROR
)

const (
	// DebugLevel represents the debug log level
	DebugLevel = "DEBUG"
	// InfoLevel represents the info log level
	InfoLevel = "INFO"
	// WarnLevel represents the warn log level
	WarnLevel = "WARN"
	// ErrorLevel represents the error log level
	ErrorLevel = "ERROR"
)

func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return DebugLevel
	case INFO:
		return InfoLevel
	case WARN:
		return WarnLevel
	case ERROR:
		return ErrorLevel
	default:
		return InfoLevel
	}
}

// ParseLogLevel converts string to LogLevel
func ParseLogLevel(level string) LogLevel {
	switch strings.ToUpper(level) {
	case DebugLevel:
		return DEBUG
	case InfoLevel:
		return INFO
	case WarnLevel, "WARNING":
		return WARN
	case ErrorLevel:
		return ERROR
	default:
		return INFO
	}
}

// Field represents a key-value pair for structured logging
type Field struct {
	Key   string
	Value any
}

// F is a convenience function for creating fields
func F(key string, value any) Field {
	return Field{Key: key, Value: value}
}

// Logger interface for structured logging
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, err error, fields ...Field)
	With(fields ...Field) Logger
	SetLevel(level LogLevel)
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level          LogLevel
	Format         string // "json" or "text"
	Output         string // "stdout", "syslog", "tcp", "udp"
	RemoteAddr     string // for tcp/udp output
	SyslogFacility string // syslog facility
	IncludeTrace   bool
}

// DefaultConfig returns default logging configuration
func DefaultConfig() LogConfig {
	return LogConfig{
		Level:          INFO,
		Format:         "json",
		Output:         "stdout",
		SyslogFacility: "mail",
		IncludeTrace:   false,
	}
}

// LoadConfigFromEnv loads logging configuration from environment variables
func LoadConfigFromEnv() LogConfig {
	config := DefaultConfig()

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Level = ParseLogLevel(level)
	}
	if format := os.Getenv("LOG_FORMAT"); format != "" {
		config.Format = format
	}
	if output := os.Getenv("LOG_OUTPUT"); output != "" {
		config.Output = output
	}
	if addr := os.Getenv("LOG_REMOTE_ADDR"); addr != "" {
		config.RemoteAddr = addr
	}
	if facility := os.Getenv("SYSLOG_FACILITY"); facility != "" {
		config.SyslogFacility = facility
	}
	if trace := os.Getenv("LOG_TRACE"); trace == "true" {
		config.IncludeTrace = true
	}

	return config
}

// NOTE: Redaction responsibility
// The logging package deliberately does not perform heuristic redaction here.
// Redaction is best performed at the call site where the meaning of fields is
// known (for example, the AUTH handler should redact credentials before
// passing them to the logger). This avoids false positives (e.g. mailbox
// addresses that look like tokens) and gives extensions full control over
// what to sanitise.

// NewLogger creates a new logger based on configuration
func NewLogger(config *LogConfig) (Logger, error) {
	switch config.Output {
	case "syslog":
		return NewSyslogLogger(config)
	case "tcp":
		return NewRemoteLogger("tcp", config)
	case "udp":
		return NewRemoteLogger("udp", config)
	case "stdout":
		return NewStdoutLogger(config), nil
	default:
		return NewStdoutLogger(config), nil
	}
}

// toSlogLevel maps a LogLevel to the equivalent slog.Level.
func toSlogLevel(level LogLevel) slog.Level {
	switch level {
	case DEBUG:
		return slog.LevelDebug
	case INFO:
		return slog.LevelInfo
	case WARN:
		return slog.LevelWarn
	case ERROR:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// newHandler builds a slog.Handler for the given writer and configuration. The
// level is held in a LevelVar so SetLevel can change it atomically at runtime,
// and slog handlers serialise their own writes, so a single logger shared across
// concurrent sessions no longer races on the output.
func newHandler(w io.Writer, config *LogConfig, level *slog.LevelVar) slog.Handler {
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: config.IncludeTrace,
	}
	// JSON is the default; any other value (including "text") uses the text handler.
	if config.Format == "json" {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

// slogLogger implements Logger on top of log/slog.
type slogLogger struct {
	sl    *slog.Logger
	level *slog.LevelVar
}

// newSlogLoggerFromHandler wraps an already-built handler and its level var.
func newSlogLoggerFromHandler(h slog.Handler, level *slog.LevelVar) *slogLogger {
	return &slogLogger{sl: slog.New(h), level: level}
}

// newSlogLogger builds a slogLogger writing to w using config.
func newSlogLogger(w io.Writer, config *LogConfig) *slogLogger {
	level := newLevelVar(config)
	return newSlogLoggerFromHandler(newHandler(w, config, level), level)
}

// newLevelVar returns a LevelVar initialised from the configured level.
func newLevelVar(config *LogConfig) *slog.LevelVar {
	level := new(slog.LevelVar)
	level.Set(toSlogLevel(config.Level))
	return level
}

// fieldAttrs converts Fields to slog attributes.
func fieldAttrs(fields []Field) []slog.Attr {
	out := make([]slog.Attr, len(fields))
	for i, f := range fields {
		out[i] = slog.Any(f.Key, f.Value)
	}
	return out
}

func (l *slogLogger) log(level slog.Level, msg string, fields []Field) {
	l.sl.LogAttrs(context.Background(), level, msg, fieldAttrs(fields)...)
}

func (l *slogLogger) Debug(msg string, fields ...Field) { l.log(slog.LevelDebug, msg, fields) }
func (l *slogLogger) Info(msg string, fields ...Field)  { l.log(slog.LevelInfo, msg, fields) }
func (l *slogLogger) Warn(msg string, fields ...Field)  { l.log(slog.LevelWarn, msg, fields) }

func (l *slogLogger) Error(msg string, err error, fields ...Field) {
	attrs := fieldAttrs(fields)
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	l.sl.LogAttrs(context.Background(), slog.LevelError, msg, attrs...)
}

func (l *slogLogger) With(fields ...Field) Logger {
	args := make([]any, len(fields))
	for i, f := range fields {
		args[i] = slog.Any(f.Key, f.Value)
	}
	return &slogLogger{sl: l.sl.With(args...), level: l.level}
}

// SetLevel changes the active level atomically; it is safe to call concurrently
// with logging.
func (l *slogLogger) SetLevel(level LogLevel) {
	l.level.Set(toSlogLevel(level))
}

// NewStdoutLogger creates a logger that writes to stdout.
func NewStdoutLogger(config *LogConfig) Logger {
	return newSlogLogger(os.Stdout, config)
}

// remoteWriter writes each log record to a freshly dialled TCP/UDP connection,
// falling back to stdout when the endpoint is unreachable.
type remoteWriter struct {
	protocol string
	addr     string
}

func (w *remoteWriter) Write(p []byte) (int, error) {
	dialer := net.Dialer{Timeout: remoteDialTimeout}
	conn, err := dialer.DialContext(context.Background(), w.protocol, w.addr)
	if err != nil {
		// Fallback to stdout if remote logging fails
		return os.Stdout.Write(p)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			_ = cerr
		}
	}()
	return conn.Write(p)
}

// NewRemoteLogger creates a logger that ships records to a TCP/UDP endpoint.
func NewRemoteLogger(protocol string, config *LogConfig) (Logger, error) {
	if config.RemoteAddr == "" {
		return nil, fmt.Errorf("remote address required for %s logging", protocol)
	}
	return newSlogLogger(&remoteWriter{protocol: protocol, addr: config.RemoteAddr}, config), nil
}

// RedactFields returns a copy of the provided fields slice with values replaced
// according to the replacements map. Callers can provide exact replacements for
// keys that need to be redacted (for example: {"args": []string{"[redacted]"}}).
func RedactFields(fields []Field, replacements map[string]any) []Field {
	out := make([]Field, len(fields))
	copy(out, fields)
	if len(fields) == 0 || len(replacements) == 0 {
		return out
	}
	for i, f := range out {
		if v, ok := replacements[f.Key]; ok {
			out[i].Value = v
		}
	}
	return out
}
