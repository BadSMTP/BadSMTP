//go:build windows
// +build windows

package logging

import (
	"maps"
	"os"
)

// On Windows, syslog isn't available. Provide a lightweight stdout-based fallback
// implementation that implements the same Logger interface used on unix-like systems.

// syslogLogger is a lightweight wrapper used only on Windows to satisfy the
// same API used on unix-like systems. It delegates to stdout.
type syslogLogger struct {
	baseLogger
}

func NewSyslogLogger(config *LogConfig) (Logger, error) {
	// Simply return a stdout logger as a fallback on Windows.
	return NewStdoutLogger(config), nil
}

// Provide methods used by callers to match the syslogLogger API via a thin wrapper
// implemented inline when needed by the logger.NewSyslogLogger consumer.
func (l *syslogLogger) With(fields ...Field) Logger {
	newFields := maps.Clone(l.fields)
	if newFields == nil {
		newFields = make(map[string]interface{})
	}
	for _, field := range fields {
		newFields[field.Key] = field.Value
	}
	return &syslogLogger{baseLogger: baseLogger{config: l.config, fields: newFields}}
}

func (l *syslogLogger) SetLevel(level LogLevel) {
	l.config.Level = level
}

func (l *syslogLogger) Debug(msg string, fields ...Field) {
	l.formatAndPrint(DEBUG, msg, nil, fields)
}

func (l *syslogLogger) Info(msg string, fields ...Field) {
	l.formatAndPrint(INFO, msg, nil, fields)
}

func (l *syslogLogger) Warn(msg string, fields ...Field) {
	l.formatAndPrint(WARN, msg, nil, fields)
}

func (l *syslogLogger) Error(msg string, err error, fields ...Field) {
	l.formatAndPrint(ERROR, msg, err, fields)
}

func (l *syslogLogger) formatAndPrint(level LogLevel, msg string, err error, fields []Field) {
	if data := l.formatEntry(level, msg, err, fields); data != nil {
		// write to stdout
		_, _ = os.Stdout.Write(data)
	}
}
