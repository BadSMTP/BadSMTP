// Package logging provides structured logging for BadSMTP server
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"strings"
	"time"
)

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
	Value interface{}
}

// F is a convenience function for creating fields
func F(key string, value interface{}) Field {
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

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Error     string                 `json:"error,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
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

// baseLogger provides common functionality
type baseLogger struct {
	config LogConfig
	fields map[string]interface{}
}

// formatEntry formats a log entry according to configuration
func (l *baseLogger) formatEntry(level LogLevel, msg string, err error, fields []Field) []byte {
	if level < l.config.Level {
		return nil
	}

	entry := LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     level.String(),
		Message:   msg,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	// Add logger-level fields
	// Use maps.Clone to copy logger-level fields if any
	entry.Fields = maps.Clone(l.fields)
	if entry.Fields == nil {
		entry.Fields = make(map[string]interface{})
	}

	// Add call-specific fields
	for _, field := range fields {
		entry.Fields[field.Key] = field.Value
	}

	if len(entry.Fields) == 0 {
		entry.Fields = nil
	}

	// Deliberately do NOT redact here; it's up to callers to sanitise sensitive values.

	switch l.config.Format {
	case "json":
		data, err := json.Marshal(entry)
		if err != nil {
			// Fallback to simple message if marshalling fails
			data = []byte(fmt.Sprintf("{\"message\":%q}", entry.Message))
		}
		return append(data, '\n')
	default:
		// Text format
		timestamp := entry.Timestamp.Format("2006-01-02T15:04:05Z")
		line := fmt.Sprintf("%s [%s] %s", timestamp, entry.Level, entry.Message)
		if entry.Error != "" {
			line += fmt.Sprintf(" error=%s", entry.Error)
		}
		if entry.Fields != nil {
			for k, v := range entry.Fields {
				line += fmt.Sprintf(" %s=%v", k, v)
			}
		}
		return []byte(line + "\n")
	}
}

// stdoutLogger writes to stdout
type stdoutLogger struct {
	baseLogger
	writer io.Writer
}

// NewStdoutLogger creates a stdout logger
func NewStdoutLogger(config *LogConfig) Logger {
	return &stdoutLogger{
		baseLogger: baseLogger{config: *config, fields: make(map[string]interface{})},
		writer:     os.Stdout,
	}
}

func (l *stdoutLogger) Debug(msg string, fields ...Field) {
	if data := l.formatEntry(DEBUG, msg, nil, fields); data != nil {
		if _, err := l.writer.Write(data); err != nil {
			// Best-effort: ignore write errors
			_ = err
		}
	}
}

func (l *stdoutLogger) Info(msg string, fields ...Field) {
	if data := l.formatEntry(INFO, msg, nil, fields); data != nil {
		if _, err := l.writer.Write(data); err != nil {
			_ = err
		}
	}
}

func (l *stdoutLogger) Warn(msg string, fields ...Field) {
	if data := l.formatEntry(WARN, msg, nil, fields); data != nil {
		if _, err := l.writer.Write(data); err != nil {
			_ = err
		}
	}
}

func (l *stdoutLogger) Error(msg string, err error, fields ...Field) {
	if data := l.formatEntry(ERROR, msg, err, fields); data != nil {
		if _, werr := l.writer.Write(data); werr != nil {
			_ = werr
		}
	}
}

func (l *stdoutLogger) With(fields ...Field) Logger {
	newFields := maps.Clone(l.fields)
	if newFields == nil {
		newFields = make(map[string]interface{})
	}
	for _, field := range fields {
		newFields[field.Key] = field.Value
	}
	return &stdoutLogger{
		baseLogger: baseLogger{config: l.config, fields: newFields},
		writer:     l.writer,
	}
}

func (l *stdoutLogger) SetLevel(level LogLevel) {
	l.config.Level = level
}

// remoteLogger writes to remote TCP/UDP endpoint
type remoteLogger struct {
	baseLogger
	protocol string
	addr     string
}

// NewRemoteLogger creates a remote logger
func NewRemoteLogger(protocol string, config *LogConfig) (Logger, error) {
	if config.RemoteAddr == "" {
		return nil, fmt.Errorf("remote address required for %s logging", protocol)
	}

	return &remoteLogger{
		baseLogger: baseLogger{config: *config, fields: make(map[string]interface{})},
		protocol:   protocol,
		addr:       config.RemoteAddr,
	}, nil
}

func (l *remoteLogger) sendLog(data []byte) {
	if data == nil {
		return
	}

	conn, err := net.Dial(l.protocol, l.addr)
	if err != nil {
		// Fallback to stdout if remote logging fails
		if _, werr := os.Stdout.Write(data); werr != nil {
			_ = werr
		}
		return
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			_ = cerr
		}
	}()

	if _, werr := conn.Write(data); werr != nil {
		_ = werr
	}
}

func (l *remoteLogger) Debug(msg string, fields ...Field) {
	data := l.formatEntry(DEBUG, msg, nil, fields)
	l.sendLog(data)
}

func (l *remoteLogger) Info(msg string, fields ...Field) {
	data := l.formatEntry(INFO, msg, nil, fields)
	l.sendLog(data)
}

func (l *remoteLogger) Warn(msg string, fields ...Field) {
	data := l.formatEntry(WARN, msg, nil, fields)
	l.sendLog(data)
}

func (l *remoteLogger) Error(msg string, err error, fields ...Field) {
	data := l.formatEntry(ERROR, msg, err, fields)
	l.sendLog(data)
}

func (l *remoteLogger) With(fields ...Field) Logger {
	newFields := maps.Clone(l.fields)
	if newFields == nil {
		newFields = make(map[string]interface{})
	}
	for _, field := range fields {
		newFields[field.Key] = field.Value
	}
	return &remoteLogger{
		baseLogger: baseLogger{config: l.config, fields: newFields},
		protocol:   l.protocol,
		addr:       l.addr,
	}
}

func (l *remoteLogger) SetLevel(level LogLevel) {
	l.config.Level = level
}

// RedactFields returns a copy of the provided fields slice with values replaced
// according to the replacements map. Callers can provide exact replacements for
// keys that need to be redacted (for example: {"args": []string{"[redacted]"}}).
func RedactFields(fields []Field, replacements map[string]interface{}) []Field {
	if len(fields) == 0 || len(replacements) == 0 {
		// nothing to do
		out := make([]Field, len(fields))
		copy(out, fields)
		return out
	}
	out := make([]Field, len(fields))
	copy(out, fields)
	for i, f := range out {
		if v, ok := replacements[f.Key]; ok {
			out[i].Value = v
		}
	}
	return out
}
