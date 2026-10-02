//go:build windows

package logging

// On Windows syslog is unavailable, so fall back to the stdout logger, which
// implements the same Logger interface.
func NewSyslogLogger(config *LogConfig) (Logger, error) {
	return NewStdoutLogger(config), nil
}
