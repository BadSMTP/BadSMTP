package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewMailbox(t *testing.T) {
	// Test creating a new mailbox
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	if mailbox.Directory != tempDir {
		t.Errorf("Expected mailbox directory %s, got %s", tempDir, mailbox.Directory)
	}

	// Verify directory exists
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Error("Mailbox directory should exist")
	}
}

func TestNewMailboxCreatesDirectory(t *testing.T) {
	// Test creating a mailbox with non-existent directory
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Remove the directory
	if err := os.RemoveAll(tempDir); err != nil {
		t.Logf("Failed to remove temp directory: %v", err)
	}

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	if mailbox.Directory != tempDir {
		t.Errorf("Expected mailbox directory %s, got %s", tempDir, mailbox.Directory)
	}

	// Verify directory was created
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Error("Mailbox directory should be created")
	}
}

func TestNewMailboxInvalidPath(t *testing.T) {
	// Create a temp file and pass its path as the mailbox directory; this should fail because a file exists at that path.
	f, err := os.CreateTemp("", "badsmtp-file-")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	fpath := f.Name()
	_ = f.Close()
	defer func() {
		_ = os.Remove(fpath)
	}()

	_, err = NewMailbox(fpath)
	if err == nil {
		t.Error("Expected error when creating mailbox with invalid path (file exists)")
	}
}

func TestSaveMessage(t *testing.T) {
	// Test saving a message
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	message := &Message{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Content: "Subject: Test Message\r\n\r\nThis is a test message.",
	}

	err = mailbox.SaveMessage(message)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}

	// Verify file was created in new/ directory (Maildir format)
	newDir := filepath.Join(tempDir, "new")
	files, err := os.ReadDir(newDir)
	if err != nil {
		t.Fatalf("Failed to read new/ directory: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("Expected 1 file in new/, got %d", len(files))
	}

	// Verify filename format (Maildir: timestamp.unique.hostname)
	filename := files[0].Name()
	if first, rest, ok := strings.Cut(filename, "."); !ok || first == "" || rest == "" {
		t.Errorf("Expected Maildir filename format (timestamp.unique.hostname), got %s", filename)
	}
}

func TestSaveMessageContent(t *testing.T) {
	// Test message content is saved correctly
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	message := &Message{
		From:    "sender@example.com",
		To:      []string{"recipient1@example.com", "recipient2@example.com"},
		Content: "Subject: Test Message\r\n\r\nThis is a test message with multiple recipients.",
	}

	err = mailbox.SaveMessage(message)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}

	// Read the saved file from new/ directory
	newDir := filepath.Join(tempDir, "new")
	files, err := os.ReadDir(newDir)
	if err != nil {
		t.Fatalf("Failed to read new/ directory: %v", err)
	}

	filePath := filepath.Join(newDir, files[0].Name())
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}

	contentStr := string(content)

	// Verify headers
	if !strings.Contains(contentStr, "From: sender@example.com") {
		t.Error("Expected From header not found")
	}
	if !strings.Contains(contentStr, "To: recipient1@example.com, recipient2@example.com") {
		t.Error("Expected To header not found")
	}
	if !strings.Contains(contentStr, "Received: by badsmtp.test;") {
		t.Error("Expected Received header not found")
	}

	// Verify content
	if !strings.Contains(contentStr, "Subject: Test Message") {
		t.Error("Expected subject not found")
	}
	if !strings.Contains(contentStr, "This is a test message with multiple recipients.") {
		t.Error("Expected message content not found")
	}
}

func TestSaveMessageTimestamp(t *testing.T) {
	// Test that timestamp is included in saved message
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	message := &Message{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Content: "Subject: Test Message\r\n\r\nThis is a test message.",
	}

	before := time.Now()
	err = mailbox.SaveMessage(message)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}
	after := time.Now()

	// Read the saved file using ListMessages
	files, err := mailbox.ListMessages()
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(files))
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}

	// Verify Maildir filename format (timestamp.unique.hostname)
	filename := filepath.Base(files[0])
	if first, rest, ok := strings.Cut(filename, "."); !ok || first == "" || rest == "" {
		t.Errorf("Expected Maildir filename format (timestamp.unique.hostname), got %s", filename)
	}

	// Verify Received header has timestamp
	contentStr := string(content)
	if !strings.Contains(contentStr, "Received: by badsmtp.test;") {
		t.Error("Expected Received header with timestamp not found")
	}

	// Verify the timestamp is reasonable (within test execution time)
	receivedTime := extractReceivedTime(contentStr)
	if receivedTime.Before(before.Add(-time.Second)) || receivedTime.After(after.Add(time.Second)) {
		t.Errorf("Received timestamp %v is outside expected range %v - %v", receivedTime, before, after)
	}
}

func TestSaveMessageMultipleRecipients(t *testing.T) {
	// Test saving message with multiple recipients
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	message := &Message{
		From: "sender@example.com",
		To: []string{
			"recipient1@example.com",
			"recipient2@example.com",
			"recipient3@example.com",
		},
		Content: "Subject: Test Message\r\n\r\nThis is a test message.",
	}

	err = mailbox.SaveMessage(message)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}

	// Read the saved file using ListMessages
	files, err := mailbox.ListMessages()
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(files))
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}

	contentStr := string(content)

	// Verify all recipients are in To header
	expectedTo := "To: recipient1@example.com, recipient2@example.com, recipient3@example.com"
	if !strings.Contains(contentStr, expectedTo) {
		t.Errorf("Expected To header '%s' not found in content", expectedTo)
	}
}

func TestSaveMessageEmpty(t *testing.T) {
	// Test saving empty message
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	message := &Message{
		From:    "",
		To:      []string{},
		Content: "",
	}

	err = mailbox.SaveMessage(message)
	if err != nil {
		t.Fatalf("Failed to save empty message: %v", err)
	}

	// Verify file was created using ListMessages
	files, err := mailbox.ListMessages()
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("Expected 1 message, got %d", len(files))
	}
}

func TestSaveMessageSpecialCharacters(t *testing.T) {
	// Test saving message with special characters
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	message := &Message{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Content: "Subject: Test with émojis 🎉\r\n\r\nThis message contains special characters: àáâãäåæçèéêë",
	}

	err = mailbox.SaveMessage(message)
	if err != nil {
		t.Fatalf("Failed to save message with special characters: %v", err)
	}

	// Read the saved file using ListMessages
	files, err := mailbox.ListMessages()
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(files))
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}

	contentStr := string(content)

	// Verify special characters are preserved
	if !strings.Contains(contentStr, "émojis 🎉") {
		t.Error("Expected special characters not found")
	}
	if !strings.Contains(contentStr, "àáâãäåæçèéêë") {
		t.Error("Expected accented characters not found")
	}
}

func TestSaveMultipleMessages(t *testing.T) {
	// Test saving multiple messages
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	// Save multiple messages
	for i := 0; i < 3; i++ {
		message := &Message{
			From:    "sender@example.com",
			To:      []string{"recipient@example.com"},
			Content: "Subject: Test Message " + string(rune(i+1)) + "\r\n\r\nThis is test message " + string(rune(i+1)) + ".",
		}

		err = mailbox.SaveMessage(message)
		if err != nil {
			t.Fatalf("Failed to save message %d: %v", i+1, err)
		}

		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Verify all files were created using ListMessages
	files, err := mailbox.ListMessages()
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(files) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(files))
	}

	// Verify filenames are unique
	filenames := make(map[string]bool)
	for _, file := range files {
		basename := filepath.Base(file)
		if filenames[basename] {
			t.Errorf("Duplicate filename: %s", basename)
		}
		filenames[basename] = true
	}
}

func TestSaveMessageFilePermissions(t *testing.T) {
	// Test that saved files have correct permissions
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	message := &Message{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Content: "Subject: Test Message\r\n\r\nThis is a test message.",
	}

	err = mailbox.SaveMessage(message)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}

	// Check file permissions using ListMessages
	files, err := mailbox.ListMessages()
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(files))
	}

	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("Failed to get file info: %v", err)
	}
	mode := info.Mode()

	// Verify file is readable
	if mode&0400 == 0 {
		t.Error("File should be readable by owner")
	}
	// Verify file is writable
	if mode&0200 == 0 {
		t.Error("File should be writable by owner")
	}
}

// Helper function to extract received time from message content
func extractReceivedTime(content string) time.Time {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Received: by badsmtp.test;") {
			// Extract timestamp from line
			if _, rest, ok := strings.Cut(line, ";"); ok {
				timeStr := strings.TrimSpace(rest)
				if ts, _, ok2 := strings.Cut(timeStr, "\r"); ok2 {
					timeStr = strings.TrimSpace(ts)
				}
				// Parse RFC1123 format
				t, err := time.Parse(time.RFC1123Z, timeStr)
				if err == nil {
					return t
				}
			}
		}
	}
	return time.Time{}
}

func TestMessageStruct(t *testing.T) {
	// Test Message struct
	message := &Message{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Content: "Test content",
	}

	if message.From != "sender@example.com" {
		t.Errorf("Expected From %s, got %s", "sender@example.com", message.From)
	}
	if len(message.To) != 1 || message.To[0] != "recipient@example.com" {
		t.Errorf("Expected To [recipient@example.com], got %v", message.To)
	}
	if message.Content != "Test content" {
		t.Errorf("Expected Content 'Test content', got %s", message.Content)
	}
}

func TestMailboxStruct(t *testing.T) {
	// Test Mailbox struct
	tempDir, err := os.MkdirTemp("", "badsmtp-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	mailbox, err := NewMailbox(tempDir)
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	if mailbox.Directory != tempDir {
		t.Errorf("Expected mailbox dir %s, got %s", tempDir, mailbox.Directory)
	}
}
