//go:build !windows
// +build !windows

package logging

import (
	"fmt"
	"log/syslog"
	"maps"
)

// syslogLogger writes to syslog (unix-like systems)
type syslogLogger struct {
	baseLogger
	writer *syslog.Writer
}

// NewSyslogLogger creates a syslog logger on unix-like systems
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

	return &syslogLogger{
		baseLogger: baseLogger{config: *config, fields: make(map[string]interface{})},
		writer:     writer,
	}, nil
}

func (l *syslogLogger) logToSyslog(level LogLevel, data []byte) {
	if data == nil {
		return
	}

	msg := string(data)
	switch level {
	case DEBUG:
		if err := l.writer.Debug(msg); err != nil {
			// best-effort: ignore but explicitly check to satisfy linters
			_ = err
		}
	case INFO:
		if err := l.writer.Info(msg); err != nil {
			_ = err
		}
	case WARN:
		if err := l.writer.Warning(msg); err != nil {
			_ = err
		}
	case ERROR:
		if err := l.writer.Err(msg); err != nil {
			_ = err
		}
	}
}

func (l *syslogLogger) Debug(msg string, fields ...Field) {
	l.logToSyslog(DEBUG, l.formatEntry(DEBUG, msg, nil, fields))
}

func (l *syslogLogger) Info(msg string, fields ...Field) {
	l.logToSyslog(INFO, l.formatEntry(INFO, msg, nil, fields))
}

func (l *syslogLogger) Warn(msg string, fields ...Field) {
	l.logToSyslog(WARN, l.formatEntry(WARN, msg, nil, fields))
}

func (l *syslogLogger) Error(msg string, err error, fields ...Field) {
	l.logToSyslog(ERROR, l.formatEntry(ERROR, msg, err, fields))
}

func (l *syslogLogger) With(fields ...Field) Logger {
	newFields := maps.Clone(l.fields)
	if newFields == nil {
		newFields = make(map[string]interface{})
	}
	for _, field := range fields {
		newFields[field.Key] = field.Value
	}
	return &syslogLogger{baseLogger: baseLogger{config: l.config, fields: newFields}, writer: l.writer}
}

func (l *syslogLogger) SetLevel(level LogLevel) { l.config.Level = level }
