//go:build !fasttests

package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"badsmtp/server"
	"badsmtp/smtp"
)

// setupSMTPConnection establishes an SMTP connection and performs initial handshake
func setupSMTPConnection(t *testing.T, port int) (net.Conn, *bufio.Reader, *bufio.Writer) {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	writer.WriteString("EHLO client.example.com\r\n")
	writer.Flush()

	// Read EHLO response
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read EHLO line: %v", err)
		}
		if strings.HasPrefix(line, fmt.Sprintf("%d ", smtp.Code250)) {
			break // Final line
		}
	}

	return conn, reader, writer
}

// writeLine writes a line to the connection and flushes it, checking for errors.
func writeLine(t *testing.T, writer *bufio.Writer, line string) {
	if _, err := writer.WriteString(line + "\r\n"); err != nil {
		t.Fatalf("Failed to write line '%s': %v", line, err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Failed to flush writer: %v", err)
	}
}

// readLine reads a line from the connection, checking for errors.
func readLine(t *testing.T, reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read line: %v", err)
	}
	return line
}

// TestSMTPIntegration tests complete SMTP sessions
func TestSMTPIntegration(t *testing.T) {
	// Create temporary mailbox directory
	tempDir, err := os.MkdirTemp("", "badsmtp-integration-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Create server configuration
	config := &server.Config{
		Port:                   0, // Use ephemeral port
		MailboxDir:             tempDir,
		GreetingDelayPortStart: 13000,
		DropDelayPortStart:     15000,
		ImmediateDropPort:      16000,
		TLSPort:                25465,
		STARTTLSPort:           25587,
	}

	// Create and start server
	srv, err := server.NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start server on ephemeral port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	config.Port = port

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Server stopped
			}
			go func(c net.Conn) {
				// Create a config specific to this port (like in server.go)
				portConfig := *config
				portConfig.Port = port
				portConfig.AnalysePortBehaviour()
				portConfig.EnsureDefaults()
				session := server.NewSession(c, &portConfig, srv.GetMailbox())
				session.Handle()
			}(conn)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test basic SMTP session
	t.Run("BasicSMTPSession", func(t *testing.T) {
		testBasicSMTPSession(t, port)
	})

	// Test EHLO with extensions
	t.Run("EHLOExtensions", func(t *testing.T) {
		testEHLOExtensions(t, port)
	})

	// Test error handling
	t.Run("ErrorHandling", func(t *testing.T) {
		testErrorHandling(t, port)
	})

	// Test authentication
	t.Run("Authentication", func(t *testing.T) {
		testAuthentication(t, port)
	})

	// Test multiple recipients
	t.Run("MultipleRecipients", func(t *testing.T) {
		testMultipleRecipients(t, port)
	})

	// Test message storage
	t.Run("MessageStorage", func(t *testing.T) {
		testMessageStorage(t, port, tempDir)
	})

	// Test command sequence validation
	t.Run("CommandSequence", func(t *testing.T) {
		testCommandSequence(t, port)
	})

	// Test RSET command
	t.Run("RSETCommand", func(t *testing.T) {
		testRSETCommand(t, port)
	})

	// Test NOOP command
	t.Run("NOOPCommand", func(t *testing.T) {
		testNOOPCommand(t, port)
	})

	// Stop server
	listener.Close()
	wg.Wait()
}

func testBasicSMTPSession(t *testing.T, port int) {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Read greeting
	greeting, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read greeting: %v", err)
	}
	if !strings.Contains(greeting, "220") {
		t.Errorf("Expected 220 greeting, got: %s", greeting)
	}

	// Send EHLO
	writer.WriteString("EHLO client.example.com\r\n")
	writer.Flush()

	// Read EHLO response
	response, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read EHLO response: %v", err)
	}
	if !strings.Contains(response, "250-badsmtp.test") {
		t.Errorf("Expected EHLO response, got: %s", response)
	}

	// Read remaining EHLO lines
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read EHLO line: %v", err)
		}
		if strings.HasPrefix(line, "250 ") {
			break // Final line
		}
	}

	// Send MAIL FROM
	writer.WriteString("MAIL FROM:<sender@example.com>\r\n")
	writer.Flush()

	response, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read MAIL FROM response: %v", err)
	}
	if !strings.Contains(response, "250 OK") {
		t.Errorf("Expected 250 OK for MAIL FROM, got: %s", response)
	}

	// Send RCPT TO
	writer.WriteString("RCPT TO:<recipient@example.com>\r\n")
	writer.Flush()

	response, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read RCPT TO response: %v", err)
	}
	if !strings.Contains(response, "250 OK") {
		t.Errorf("Expected 250 OK for RCPT TO, got: %s", response)
	}

	// Send DATA
	writer.WriteString("DATA\r\n")
	writer.Flush()

	response, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read DATA response: %v", err)
	}
	if !strings.Contains(response, "354") {
		t.Errorf("Expected 354 for DATA, got: %s", response)
	}

	// Send message content
	writer.WriteString("Subject: Test Message\r\n")
	writer.WriteString("\r\n")
	writer.WriteString("This is a test message.\r\n")
	writer.WriteString(".\r\n")
	writer.Flush()

	response, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read message response: %v", err)
	}
	if !strings.Contains(response, "250 OK") {
		t.Errorf("Expected 250 OK for message, got: %s", response)
	}

	// Send QUIT
	writer.WriteString("QUIT\r\n")
	writer.Flush()

	response, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read QUIT response: %v", err)
	}
	if !strings.Contains(response, "221") {
		t.Errorf("Expected 221 for QUIT, got: %s", response)
	}
}

func testEHLOExtensions(t *testing.T, port int) {
	testCases := []struct {
		hostname      string
		shouldHave    []string
		shouldNotHave []string
	}{
		{
			hostname:      "starttls.example.com",
			shouldHave:    []string{"STARTTLS"},
			shouldNotHave: []string{},
		},
		{
			hostname:      "noauth.example.com",
			shouldHave:    []string{},
			shouldNotHave: []string{"AUTH"},
		},
		{
			hostname:      "no8bit.example.com",
			shouldHave:    []string{},
			shouldNotHave: []string{"8BITMIME"},
		},
		{
			hostname:      "size.example.com",
			shouldHave:    []string{"SIZE"},
			shouldNotHave: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.hostname, func(t *testing.T) {
			conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
			if err != nil {
				t.Fatalf("Failed to connect: %v", err)
			}
			defer conn.Close()

			reader := bufio.NewReader(conn)
			writer := bufio.NewWriter(conn)

			// Read greeting
			reader.ReadString('\n')

			// Send EHLO
			fmt.Fprintf(writer, "EHLO %s\r\n", tc.hostname)
			writer.Flush()

			// Read all EHLO response lines
			var allLines []string
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					t.Fatalf("Failed to read EHLO line: %v", err)
				}
				allLines = append(allLines, line)
				if strings.HasPrefix(line, "250 ") {
					break
				}
			}

			fullResponse := strings.Join(allLines, "")

			// Check for expected extensions
			for _, expected := range tc.shouldHave {
				if !strings.Contains(fullResponse, expected) {
					t.Errorf("Expected extension %s not found in response", expected)
				}
			}

			// Check for unexpected extensions
			for _, notExpected := range tc.shouldNotHave {
				if strings.Contains(fullResponse, notExpected) {
					t.Errorf("Unexpected extension %s found in response", notExpected)
				}
			}

			// Send QUIT
			writer.WriteString("QUIT\r\n")
			writer.Flush()
			reader.ReadString('\n')
		})
	}
}

func testErrorHandling(t *testing.T, port int) {
	conn, reader, writer := setupSMTPConnection(t, port)
	defer conn.Close()

	// Test error in MAIL FROM (using verb-prefix pattern: mail550@example.com)
	writeLine(t, writer, "MAIL FROM:<mail550@example.com>")

	response := readLine(t, reader)
	if !strings.Contains(response, "550") {
		t.Errorf("Expected 550 error, got: %s", response)
	}

	// Send QUIT
	writeLine(t, writer, "QUIT")
	readLine(t, reader)
}

func testAuthentication(t *testing.T, port int) {
	conn, reader, writer := setupSMTPConnection(t, port)
	defer conn.Close()

	// Test PLAIN authentication with good credentials
	writeLine(t, writer, "AUTH PLAIN AGdvb2RhdXRoQGV4YW1wbGUuY29tAHBhc3N3b3Jk")

	response := readLine(t, reader)
	if !strings.Contains(response, "235") {
		t.Errorf("Expected 235 success, got: %s", response)
	}

	// Send QUIT
	writeLine(t, writer, "QUIT")
	readLine(t, reader)
}

func testMultipleRecipients(t *testing.T, port int) {
	conn, reader, writer := setupSMTPConnection(t, port)
	defer conn.Close()

	// Send MAIL FROM
	writeLine(t, writer, "MAIL FROM:<sender@example.com>")
	response := readLine(t, reader)
	if !strings.Contains(response, "250 OK") {
		t.Errorf("Expected 250 OK for MAIL FROM, got: %s", response)
	}

	// Send multiple RCPT TO
	recipients := []string{"recipient1@example.com", "recipient2@example.com", "recipient3@example.com"}
	for _, recipient := range recipients {
		writeLine(t, writer, fmt.Sprintf("RCPT TO:<%s>", recipient))
		response := readLine(t, reader)
		if !strings.Contains(response, "250 OK") {
			t.Errorf("Expected 250 OK for RCPT TO %s, got: %s", recipient, response)
		}
	}

	// Send DATA
	writeLine(t, writer, "DATA")
	response = readLine(t, reader)
	if !strings.Contains(response, "354") {
		t.Errorf("Expected 354 for DATA, got: %s", response)
	}

	// Send message content
	writeLine(t, writer, "Subject: Test Message")
	writeLine(t, writer, "")
	writeLine(t, writer, "This is a test message to multiple recipients.")
	writeLine(t, writer, ".")

	response = readLine(t, reader)
	if !strings.Contains(response, "250 OK") {
		t.Errorf("Expected 250 OK for message, got: %s", response)
	}

	// Send QUIT
	writeLine(t, writer, "QUIT")
	readLine(t, reader)
}

func testMessageStorage(t *testing.T, port int, tempDir string) {
	conn, reader, writer := setupSMTPConnection(t, port)
	defer conn.Close()

	// Send complete message
	writeLine(t, writer, "MAIL FROM:<sender@example.com>")
	readLine(t, reader)

	recipients := []string{"recipient@example.com"}
	for _, recipient := range recipients {
		writeLine(t, writer, fmt.Sprintf("RCPT TO:<%s>", recipient))
		response := readLine(t, reader)
		if !strings.Contains(response, "250 OK") {
			t.Errorf("Expected 250 OK for RCPT TO %s, got: %s", recipient, response)
		}
	}

	// Send DATA command
	writeLine(t, writer, "DATA")
	response := readLine(t, reader)
	if !strings.Contains(response, "354") {
		t.Errorf("Expected 354 for DATA, got: %s", response)
	}

	// Send message content
	testSubject := "Test Message Storage"
	testBody := "This is a test message for storage verification."
	writeLine(t, writer, fmt.Sprintf("Subject: %s", testSubject))
	writeLine(t, writer, "From: sender@example.com")
	writeLine(t, writer, "To: recipient@example.com")
	writeLine(t, writer, "")
	writeLine(t, writer, testBody)
	writeLine(t, writer, ".")

	response = readLine(t, reader)
	if !strings.Contains(response, "250 OK") {
		t.Errorf("Expected 250 OK for message, got: %s", response)
	}

	// Send QUIT
	writeLine(t, writer, "QUIT")
	readLine(t, reader)

	// Verify message was stored (Maildir format: check new/ directory)
	newDir := filepath.Join(tempDir, "new")
	files, err := filepath.Glob(filepath.Join(newDir, "*"))
	if err != nil {
		t.Fatalf("Failed to list message files: %v", err)
	}

	if len(files) == 0 {
		t.Error("No message files found in mailbox/new directory")
		return
	}

	// Read the stored message and verify content
	messageFile := files[len(files)-1] // Get the most recent message
	content, err := os.ReadFile(messageFile)
	if err != nil {
		t.Fatalf("Failed to read message file: %v", err)
	}

	messageContent := string(content)
	if !strings.Contains(messageContent, "From: sender@example.com") {
		t.Error("Message should contain From header")
	}
	if !strings.Contains(messageContent, "To: recipient@example.com") {
		t.Error("Message should contain To header")
	}
	if !strings.Contains(messageContent, testSubject) {
		t.Error("Message should contain subject")
	}
	if !strings.Contains(messageContent, testBody) {
		t.Error("Message should contain body text")
	}
	if !strings.Contains(messageContent, "Received: by badsmtp.test") {
		t.Error("Message should contain Received header")
	}
}

func testCommandSequence(t *testing.T, port int) {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Read greeting
	reader.ReadString('\n')

	// Try to send MAIL FROM before EHLO
	writer.WriteString("MAIL FROM:<sender@example.com>\r\n")
	writer.Flush()

	response, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}
	if !strings.Contains(response, "503") {
		t.Errorf("Expected 503 bad sequence, got: %s", response)
	}

	// Send QUIT
	writer.WriteString("QUIT\r\n")
	writer.Flush()
	reader.ReadString('\n')
}

func testRSETCommand(t *testing.T, port int) {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	writer.WriteString("EHLO client.example.com\r\n")
	writer.Flush()

	// Read EHLO response
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read EHLO line: %v", err)
		}
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	// Start mail transaction
	writer.WriteString("MAIL FROM:<sender@example.com>\r\n")
	writer.Flush()
	reader.ReadString('\n')

	writer.WriteString("RCPT TO:<recipient@example.com>\r\n")
	writer.Flush()
	reader.ReadString('\n')

	// Send RSET
	writer.WriteString("RSET\r\n")
	writer.Flush()

	response, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read RSET response: %v", err)
	}
	if !strings.Contains(response, "250 OK") {
		t.Errorf("Expected 250 OK for RSET, got: %s", response)
	}

	// Should be able to start new transaction
	writer.WriteString("MAIL FROM:<sender2@example.com>\r\n")
	writer.Flush()

	response, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read MAIL FROM response: %v", err)
	}
	if !strings.Contains(response, "250 OK") {
		t.Errorf("Expected 250 OK for MAIL FROM after RSET, got: %s", response)
	}

	// Send QUIT
	writer.WriteString("QUIT\r\n")
	writer.Flush()
	reader.ReadString('\n')
}

func testNOOPCommand(t *testing.T, port int) {
	conn, reader, writer := setupSMTPConnection(t, port)
	defer conn.Close()

	// Send NOOP
	writeLine(t, writer, "NOOP")

	response := readLine(t, reader)
	if !strings.Contains(response, "250 OK") {
		t.Errorf("Expected 250 OK for NOOP, got: %s", response)
	}

	// Send QUIT
	writeLine(t, writer, "QUIT")
	readLine(t, reader)
}
