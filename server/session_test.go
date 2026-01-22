package server

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"badsmtp/smtp"
	"badsmtp/storage"
)

// Mock connection for testing
type mockConn struct {
	readBuffer  *bytes.Buffer
	writeBuffer *bytes.Buffer
	closed      bool
}

func newMockConn() *mockConn {
	return &mockConn{
		readBuffer:  &bytes.Buffer{},
		writeBuffer: &bytes.Buffer{},
		closed:      false,
	}
}

func (m *mockConn) Read(b []byte) (int, error) {
	return m.readBuffer.Read(b)
}

func (m *mockConn) Write(b []byte) (int, error) {
	return m.writeBuffer.Write(b)
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *mockConn) writeInput(data string) {
	m.readBuffer.WriteString(data)
}

func (m *mockConn) getOutput() string {
	return m.writeBuffer.String()
}

// Helper to create a config with defaults for tests
func newTestConfig() *Config {
	cfg := &Config{Port: 2525}
	cfg.EnsureDefaults()
	return cfg
}

func TestNewSession(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()

	session := NewSession(conn, config, nil)

	if session.conn != conn {
		t.Error("Session connection not set correctly")
	}
	if session.config != config {
		t.Error("Session config not set correctly")
	}
	if session.state != smtp.StateGreeting {
		t.Error("Session should start in StateGreeting")
	}
	if session.authenticated {
		t.Error("Session should not be authenticated initially")
	}
}

func TestSessionGreeting(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()
	session := NewSession(conn, config, nil)

	// Simulate QUIT command to end session quickly
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if !strings.Contains(output, fmt.Sprintf("%d badsmtp.test ESMTP %s", smtp.Code220, ServerGreeting)) {
		t.Error("Expected greeting message not found in output")
	}
}

func TestSessionImmediateDrop(t *testing.T) {
	conn := newMockConn()
	config := &Config{Port: 6000, DropImmediate: true}
	config.EnsureDefaults()
	config.AnalysePortBehaviour()

	session := NewSession(conn, config, nil)

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if output != "" {
		t.Error("Expected no output for immediate drop, got:", output)
	}
}

func TestSessionEHLO(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()
	session := NewSession(conn, config, nil)

	// Send EHLO followed by QUIT
	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if !strings.Contains(output, "250-badsmtp.test") {
		t.Error("Expected EHLO response not found")
	}
	if !strings.Contains(output, "250-AUTH") {
		t.Error("Expected AUTH mechanism in EHLO response")
	}
	if !strings.Contains(output, "250-8BITMIME") {
		t.Error("Expected 8BITMIME in EHLO response")
	}
}

func TestSessionEHLOWithHostnamePatterns(t *testing.T) {
	tests := []struct {
		hostname      string
		shouldHave    []string
		shouldNotHave []string
	}{
		{
			hostname:      "noauth.example.com",
			shouldHave:    []string{"250-badsmtp.test"},
			shouldNotHave: []string{"250-AUTH"},
		},
		{
			hostname:      "no8bit.example.com",
			shouldHave:    []string{"250-badsmtp.test"},
			shouldNotHave: []string{"250-8BITMIME"},
		},
		{
			hostname:      "starttls.example.com",
			shouldHave:    []string{"250-badsmtp.test", "250-STARTTLS"},
			shouldNotHave: []string{},
		},
		{
			hostname:      "size.example.com",
			shouldHave:    []string{"250-badsmtp.test", "250-SIZE"},
			shouldNotHave: []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.hostname, func(t *testing.T) {
			conn := newMockConn()
			config := newTestConfig()
			session := NewSession(conn, config, nil)

			conn.writeInput(fmt.Sprintf("EHLO %s\r\n", test.hostname))
			conn.writeInput("QUIT\r\n")

			err := session.Handle()
			if err != nil {
				t.Fatalf("Session handle failed: %v", err)
			}

			output := conn.getOutput()

			for _, expected := range test.shouldHave {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected '%s' in output for hostname %s", expected, test.hostname)
				}
			}

			for _, notExpected := range test.shouldNotHave {
				if strings.Contains(output, notExpected) {
					t.Errorf("Did not expect '%s' in output for hostname %s", notExpected, test.hostname)
				}
			}
		})
	}
}

func TestSessionMailTransaction(t *testing.T) {
	conn := newMockConn()

	// Create temporary directory for mailbox
	tempDir := t.TempDir()

	config := &Config{
		Port:       2525,
		MailboxDir: tempDir,
	}
	config.EnsureDefaults()
	session := NewSession(conn, config, nil)

	// Complete SMTP transaction
	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("MAIL FROM:<sender@example.com>\r\n")
	conn.writeInput("RCPT TO:<recipient@example.com>\r\n")
	conn.writeInput("DATA\r\n")
	conn.writeInput("Subject: Test Message\r\n")
	conn.writeInput("\r\n")
	conn.writeInput("This is a test message.\r\n")
	conn.writeInput(".\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()

	// Check all expected responses
	expectedResponses := []string{
		fmt.Sprintf("%d badsmtp.test ESMTP %s", smtp.Code220, ServerGreeting),
		"250-badsmtp.test",
		"250 OK", // MAIL FROM response
		"250 OK", // RCPT TO response
		"354 End data with <CR><LF>.<CR><LF>",
		"250 OK Message accepted for delivery",
		"221 Bye",
	}

	for _, expected := range expectedResponses {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected response '%s' not found in output", expected)
		}
	}
}

func TestSessionErrorCodes(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()
	session := NewSession(conn, config, nil)

	// Test flexible error code in MAIL FROM (using new format)
	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("MAIL FROM:<mail550@example.com>\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if !strings.Contains(output, "550 ") {
		t.Error("Expected 550 error code in response")
	}
}

func TestSessionBadSequence(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()
	session := NewSession(conn, config, nil)

	// Try to send MAIL FROM before EHLO
	conn.writeInput("MAIL FROM:<sender@example.com>\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if !strings.Contains(output, "503 Bad sequence") {
		t.Errorf("Expected bad sequence error, got: %s", output)
	}
}

func TestSessionRSET(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()
	session := NewSession(conn, config, nil)

	// Start transaction, then reset
	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("MAIL FROM:<sender@example.com>\r\n")
	conn.writeInput("RCPT TO:<recipient@example.com>\r\n")
	conn.writeInput("RSET\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if !strings.Contains(output, "250 OK") {
		t.Error("Expected OK response to RSET")
	}
}

func TestSessionNOOP(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()
	session := NewSession(conn, config, nil)

	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("NOOP\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if !strings.Contains(output, "250 OK") {
		t.Error("Expected OK response to NOOP")
	}
}

func TestSessionWithMailbox(t *testing.T) {
	// Create temporary directory and mailbox
	tempDir := t.TempDir()
	mailbox, err := storage.NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	conn := newMockConn()
	config := &Config{
		Port:       2525,
		MailboxDir: tempDir,
	}
	config.EnsureDefaults()
	session := NewSession(conn, config, mailbox)

	// Complete SMTP transaction
	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("MAIL FROM:<sender@example.com>\r\n")
	conn.writeInput("RCPT TO:<recipient@example.com>\r\n")
	conn.writeInput("DATA\r\n")
	conn.writeInput("Subject: Test Message\r\n")
	conn.writeInput("\r\n")
	conn.writeInput("This is a test message.\r\n")
	conn.writeInput(".\r\n")
	conn.writeInput("QUIT\r\n")

	err = session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if !strings.Contains(output, "250 OK Message accepted for delivery") {
		t.Error("Expected message accepted response")
	}
}

func TestSessionCommandDelay(t *testing.T) {
	conn := newMockConn()
	// Set per-session delay to 1 second for testing
	config := &Config{Port: 4001}
	config.EnsureDefaults()

	session := NewSession(conn, config, nil)
	// apply per-session command delay
	session.commandDelay = 1

	start := time.Now()
	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	elapsed := time.Since(start)
	// Should have some delay (at least 500ms, but allowing for test timing variance)
	if elapsed < 500*time.Millisecond {
		t.Error("Expected command delay to be applied")
	}
}

func TestSessionDotStuffing(t *testing.T) {
	conn := newMockConn()

	// Create temporary directory for mailbox
	tempDir := t.TempDir()

	config := &Config{
		Port:       2525,
		MailboxDir: tempDir,
	}
	config.EnsureDefaults()
	session := NewSession(conn, config, nil)

	// Send message with dot-stuffing
	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("MAIL FROM:<sender@example.com>\r\n")
	conn.writeInput("RCPT TO:<recipient@example.com>\r\n")
	conn.writeInput("DATA\r\n")
	conn.writeInput("Subject: Test Message\r\n")
	conn.writeInput("\r\n")
	conn.writeInput("..This line starts with two dots\r\n")
	conn.writeInput("Normal line\r\n")
	conn.writeInput(".\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if !strings.Contains(output, "250 OK Message accepted for delivery") {
		t.Error("Expected message accepted response")
	}
}

func TestSessionTLSState(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()
	session := NewSession(conn, config, nil)

	// Mock TLS state
	tlsState := &tls.ConnectionState{
		Version: tls.VersionTLS12,
	}
	session.tlsState = tlsState

	if session.tlsState == nil {
		t.Error("Expected TLS state to be set")
	}
	if session.tlsState.Version != tls.VersionTLS12 {
		t.Error("Expected TLS version to be TLS 1.2")
	}
}

func TestSessionWriteResponse(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()
	session := NewSession(conn, config, nil)

	err := session.writeResponse(fmt.Sprintf("%d OK Test Response", smtp.Code250))
	if err != nil {
		t.Fatalf("Failed to write response: %v", err)
	}

	output := conn.getOutput()
	expected := fmt.Sprintf("%d OK Test Response\r\n", smtp.Code250)
	if output != expected {
		t.Errorf("Expected response '%s', got '%s'", expected, output)
	}
}

func TestSessionMultipleRcptTo(t *testing.T) {
	conn := newMockConn()

	// Create temporary directory for mailbox
	tempDir := t.TempDir()

	config := &Config{
		Port:       2525,
		MailboxDir: tempDir,
	}
	config.EnsureDefaults()
	session := NewSession(conn, config, nil)

	// Send message to multiple recipients
	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("MAIL FROM:<sender@example.com>\r\n")
	conn.writeInput("RCPT TO:<recipient1@example.com>\r\n")
	conn.writeInput("RCPT TO:<recipient2@example.com>\r\n")
	conn.writeInput("DATA\r\n")
	conn.writeInput("Subject: Test Message\r\n")
	conn.writeInput("\r\n")
	conn.writeInput("This is a test message.\r\n")
	conn.writeInput(".\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()

	// Count the number of "250 OK" responses for RCPT TO
	rcptResponses := strings.Count(output, "250 OK")
	if rcptResponses < 2 {
		t.Error("Expected at least 2 OK responses for RCPT TO commands")
	}
}

func TestSessionInvalidCommands(t *testing.T) {
	conn := newMockConn()
	config := newTestConfig()
	session := NewSession(conn, config, nil)

	// Send invalid command
	conn.writeInput("INVALID COMMAND\r\n")
	conn.writeInput("QUIT\r\n")

	err := session.Handle()
	if err != nil {
		t.Fatalf("Session handle failed: %v", err)
	}

	output := conn.getOutput()
	if !strings.Contains(output, "500 Command not recognised") {
		t.Error("Expected command not recognised error")
	}
}
