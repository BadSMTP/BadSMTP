// Package logging provides SMTP-specific structured logging
package logging

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

// SMTPLogger provides SMTP-specific logging methods
type SMTPLogger struct {
	Logger
	sessionID string
	clientIP  string
	hostname  string
}

// NewSMTPLogger creates a new SMTP logger with session context
func NewSMTPLogger(logger Logger, conn net.Conn, hostname string) *SMTPLogger {
	sessionID := generateSessionID()
	clientIP := ""
	if conn != nil {
		if addr := conn.RemoteAddr(); addr != nil {
			clientIP = addr.String()
			// Extract IP without port for cleaner logs
			if host, _, err := net.SplitHostPort(clientIP); err == nil {
				clientIP = host
			}
		}
	}

	return &SMTPLogger{
		Logger:    logger.With(F("session_id", sessionID)),
		sessionID: sessionID,
		clientIP:  clientIP,
		hostname:  hostname,
	}
}

// SessionIDBytes is the number of bytes used for session ID generation
const SessionIDBytes = 12

// generateSessionID creates a random session identifier
func generateSessionID() string {
	b := make([]byte, SessionIDBytes)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use timestamp-based ID if crypto/rand fails (very unlikely)
		return fmt.Sprintf("sess_%x", time.Now().UnixNano())
	}
	return "sess_" + hex.EncodeToString(b)
}

// LogConnection logs connection establishment
func (l *SMTPLogger) LogConnection(port int, tlsEnabled bool) {
	fields := []Field{
		F("client_ip", l.clientIP),
		F("port", port),
		F("tls_enabled", tlsEnabled),
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}
	l.Info("SMTP connection established", fields...)
}

// LogConnectionClosed logs connection closure
func (l *SMTPLogger) LogConnectionClosed(duration time.Duration) {
	l.Info("SMTP connection closed",
		F("client_ip", l.clientIP),
		F("duration_ms", duration.Milliseconds()))
}

// LogCommand logs an SMTP command received
func (l *SMTPLogger) LogCommand(command string, args []string, smtpState string) {
	fields := []Field{
		F("client_ip", l.clientIP),
		F("command", command),
		F("smtp_state", smtpState),
	}
	if len(args) > 0 {
		fields = append(fields, F("args", args))
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}
	l.Info("SMTP command received", fields...)
}

// LogResponse logs an SMTP response sent
func (l *SMTPLogger) LogResponse(response, command string) {
	// Parse response code
	parts := strings.SplitN(response, " ", 2)
	responseCode := ""
	if len(parts) >= 2 {
		responseCode = parts[0]
	}

	fields := []Field{
		F("client_ip", l.clientIP),
		F("response", response),
		F("response_code", responseCode),
	}
	if command != "" {
		fields = append(fields, F("command", command))
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}

	// Determine log level based on response code
	if strings.HasPrefix(responseCode, "2") {
		l.Info("SMTP response sent", fields...)
	} else if strings.HasPrefix(responseCode, "4") || strings.HasPrefix(responseCode, "5") {
		l.Warn("SMTP error response sent", fields...)
	} else {
		l.Info("SMTP response sent", fields...)
	}
}

// LogAuthentication logs authentication attempts
func (l *SMTPLogger) LogAuthentication(mechanism, username string, success bool) {
	level := "Info"
	msg := "SMTP authentication successful"
	if !success {
		level = "Warn"
		msg = "SMTP authentication failed"
	}

	fields := []Field{
		F("client_ip", l.clientIP),
		F("auth_mechanism", mechanism),
		F("username", username),
		F("success", success),
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}

	if level == "Warn" {
		l.Warn(msg, fields...)
	} else {
		l.Info(msg, fields...)
	}
}

// LogMessageStart logs the start of message processing
func (l *SMTPLogger) LogMessageStart(from string, to []string) {
	fields := []Field{
		F("client_ip", l.clientIP),
		F("mail_from", from),
		F("rcpt_to", to),
		F("rcpt_count", len(to)),
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}
	l.Info("SMTP message processing started", fields...)
}

// LogMessageStored logs successful message storage
func (l *SMTPLogger) LogMessageStored(from string, to []string, size int, storageType string, duration time.Duration) {
	fields := []Field{
		F("client_ip", l.clientIP),
		F("mail_from", from),
		F("rcpt_to", to),
		F("message_size", size),
		F("storage_type", storageType),
		F("duration_ms", duration.Milliseconds()),
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}
	l.Info("SMTP message stored successfully", fields...)
}

// LogMessageStorageError logs message storage failures
func (l *SMTPLogger) LogMessageStorageError(from string, to []string, size int, storageType string, err error) {
	fields := []Field{
		F("client_ip", l.clientIP),
		F("mail_from", from),
		F("rcpt_to", to),
		F("message_size", size),
		F("storage_type", storageType),
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}
	l.Error("SMTP message storage failed", err, fields...)
}

// LogBehaviourTriggered logs when special port behaviour is triggered
func (l *SMTPLogger) LogBehaviourTriggered(behaviour string, port, delaySeconds int) {
	fields := []Field{
		F("client_ip", l.clientIP),
		F("behaviour", behaviour),
		F("port", port),
	}
	if delaySeconds > 0 {
		fields = append(fields, F("delay_seconds", delaySeconds))
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}
	l.Info("SMTP behaviour triggered", fields...)
}

// LogErrorSimulation logs when error codes are simulated
func (l *SMTPLogger) LogErrorSimulation(errorCode int, trigger, smtpStage string) {
	l.Warn("SMTP error simulation triggered",
		F("client_ip", l.clientIP),
		F("error_code", errorCode),
		F("trigger", trigger),
		F("smtp_stage", smtpStage),
		F("hostname", l.hostname))
}

// LogTLSHandshake logs TLS handshake events
func (l *SMTPLogger) LogTLSHandshake(success bool, tlsVersion, cipher string, err error) {
	fields := []Field{
		F("client_ip", l.clientIP),
		F("success", success),
	}
	if tlsVersion != "" {
		fields = append(fields, F("tls_version", tlsVersion))
	}
	if cipher != "" {
		fields = append(fields, F("cipher", cipher))
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}

	if success {
		l.Info("TLS handshake successful", fields...)
	} else {
		l.Error("TLS handshake failed", err, fields...)
	}
}

// LogPerformanceMetric logs performance-related metrics
func (l *SMTPLogger) LogPerformanceMetric(operation string, duration time.Duration, success bool) {
	fields := []Field{
		F("client_ip", l.clientIP),
		F("operation", operation),
		F("duration_ms", duration.Milliseconds()),
		F("success", success),
	}
	if l.hostname != "" {
		fields = append(fields, F("hostname", l.hostname))
	}
	l.Debug("Performance metric", fields...)
}

// LogStateTransition logs SMTP state changes
func (l *SMTPLogger) LogStateTransition(fromState, toState, command string) {
	l.Debug("SMTP state transition",
		F("client_ip", l.clientIP),
		F("from_state", fromState),
		F("to_state", toState),
		F("command", command),
		F("hostname", l.hostname))
}

// GetSessionID returns the session ID for external use
func (l *SMTPLogger) GetSessionID() string {
	return l.sessionID
}

// GetClientIP returns the client IP for external use
func (l *SMTPLogger) GetClientIP() string {
	return l.clientIP
}
