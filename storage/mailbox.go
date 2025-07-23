// Package storage provides message storage functionality for BadSMTP.
package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"badsmtp/logging"
)

const (
	// MailboxDirPermissions holds the permissions used for the mailbox directory
	MailboxDirPermissions = 0750
	// MaildirFilePermissions holds the permissions used for maildir message files
	MaildirFilePermissions = 0600
)

var messageCounter atomic.Int64
var (
	loggerCfg = logging.DefaultConfig()
	stdLogger = logging.NewStdoutLogger(&loggerCfg)
)

// Mailbox represents a mailbox for storing messages in Maildir format.
type Mailbox struct {
	Directory string
	hostname  string
}

// Message represents an email message.
type Message struct {
	From    string
	To      []string
	Content string
}

// remapUnixTmpOnWindows maps incoming unix-style /tmp or /var/tmp paths to the real OS temp dir on Windows.
func remapUnixTmpOnWindows(dir string) string {
	if runtime.GOOS != "windows" {
		return dir
	}
	slashed := filepath.ToSlash(dir)
	if strings.HasPrefix(slashed, "/tmp") || strings.HasPrefix(slashed, "/var/tmp") {
		tail := strings.TrimPrefix(slashed, "/tmp")
		tail = strings.TrimPrefix(tail, "/")
		if tail == "" {
			return os.TempDir()
		}
		return filepath.Join(os.TempDir(), filepath.FromSlash(tail))
	}
	return dir
}

// validateMailboxPathWindows enforces conservative rules for absolute paths on Windows.
// It returns nil for acceptable paths, or an error describing why a path is rejected.
func validateMailboxPathWindows(dir string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	// Reject unix-style absolute paths that weren't remapped
	if strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "\\") {
		return fmt.Errorf("invalid mailbox directory path: %s", dir)
	}
	if filepath.IsAbs(dir) {
		cleanDir := filepath.ToSlash(filepath.Clean(dir))
		cleanTmp := filepath.ToSlash(filepath.Clean(os.TempDir()))
		cwd, cwdErr := os.Getwd()
		if cwdErr == nil {
			cleanCwd := filepath.ToSlash(filepath.Clean(cwd))
			if !strings.HasPrefix(cleanDir, cleanCwd) && !strings.HasPrefix(cleanDir, cleanTmp) {
				return fmt.Errorf("invalid mailbox directory path: %s", dir)
			}
		} else if !strings.HasPrefix(cleanDir, cleanTmp) {
			return fmt.Errorf("invalid mailbox directory path: %s", dir)
		}
	}
	return nil
}

// NewMailbox creates a new mailbox at the specified directory using Maildir format.
// Maildir format uses three subdirectories: new/, cur/, and tmp/
func NewMailbox(directory string) (*Mailbox, error) {
	// Canonicalise incoming path and apply platform-specific remapping/validation.
	directory = remapUnixTmpOnWindows(directory)

	if err := validateMailboxPathWindows(directory); err != nil {
		return nil, err
	}

	// Create main directory
	if err := os.MkdirAll(directory, MailboxDirPermissions); err != nil {
		return nil, fmt.Errorf("failed to create mailbox directory: %w", err)
	}

	// Create Maildir subdirectories
	subdirs := []string{"new", "cur", "tmp"}
	for _, subdir := range subdirs {
		path := filepath.Join(directory, subdir)
		if err := os.MkdirAll(path, MailboxDirPermissions); err != nil {
			return nil, fmt.Errorf("failed to create maildir subdirectory %s: %w", subdir, err)
		}
	}

	// Get hostname for Maildir filenames
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "badsmtp.test"
	}

	return &Mailbox{
		Directory: directory,
		hostname:  hostname,
	}, nil
}

// SaveMessage saves a message to the mailbox using Maildir format.
// Messages are written to tmp/ first, then atomically moved to new/
func (m *Mailbox) SaveMessage(msg *Message) error {
	now := time.Now()

	// Prepare tmp directory path
	tmpDir := filepath.Join(m.Directory, "tmp")

	// Ensure tmpDir exists (should already, but be defensive)
	if err := os.MkdirAll(tmpDir, MailboxDirPermissions); err != nil {
		return fmt.Errorf("failed to ensure tmp directory: %w", err)
	}

	// Create a secure unique temporary file in tmp/
	tmpFile, err := os.CreateTemp(tmpDir, "msg-*") // returns an opened *os.File
	if err != nil {
		return fmt.Errorf("failed to create temp message file: %w", err)
	}
	// Ensure tmpFile is removed on error; we will rename it into new/ on success
	tmpPath := tmpFile.Name()

	// Validate that the path is within the mailbox directory (security check)
	if err := validatePathWithinDir(m.Directory, tmpPath); err != nil {
		_closeErr := tmpFile.Close()
		if _closeErr != nil {
			stdLogger.Error("Error closing temp file after validation failure", _closeErr)
		}
		_rmErr := os.Remove(tmpPath)
		if _rmErr != nil {
			stdLogger.Error("Failed to remove temp file after validation failure", fmt.Errorf("%s: %v", tmpPath, _rmErr))
		}
		return err
	}

	// Ensure file has the desired permissions (Maildir expects restrictive perms)
	if chmodErr := tmpFile.Chmod(MaildirFilePermissions); chmodErr != nil {
		// Not fatal; log and continue
		stdLogger.Warn("Warning: failed to chmod temp file", logging.F("path", tmpPath), logging.F("err", chmodErr))
	}

	// Use the opened tmpFile directly for writing (avoid reopening)
	file := tmpFile

	// Write message content and headers
	writeErr := writeMessageToFile(file, msg)

	// Close file and capture error
	if closeErr := file.Close(); closeErr != nil {
		stdLogger.Error("Error closing file", closeErr)
		writeErr = errors.Join(writeErr, closeErr)
	}

	if writeErr != nil {
		if rmErr := os.Remove(tmpPath); rmErr != nil {
			stdLogger.Error("Failed to remove temp file", fmt.Errorf("%s: %v", tmpPath, rmErr))
		}
		return fmt.Errorf("failed to write message: %w", writeErr)
	}

	// Atomically move from tmp/ to new/ (Maildir delivery)
	filename := generateMailFilename(now, &messageCounter, m.hostname)
	newPath := filepath.Join(m.Directory, "new", filename)
	if err := os.Rename(tmpPath, newPath); err != nil {
		if rmErr := os.Remove(tmpPath); rmErr != nil {
			stdLogger.Error("Failed to remove temp file after rename failure", fmt.Errorf("%s: %v", tmpPath, rmErr))
		}
		return fmt.Errorf("failed to deliver message to new/: %w", err)
	}

	stdLogger.Info("Message saved", logging.F("path", newPath))
	return nil
}

// generateMailFilename generates a maildir-compliant filename
func generateMailFilename(now time.Time, counter *atomic.Int64, hostname string) string {
	c := counter.Add(1)
	unique := fmt.Sprintf("%d_%d_%d", now.UnixMicro(), os.Getpid(), c)
	return fmt.Sprintf("%d.%s.%s", now.Unix(), unique, hostname)
}

// validatePathWithinDir ensures the targetPath is inside baseDir
func validatePathWithinDir(baseDir, targetPath string) error {
	cleanTarget := filepath.Clean(targetPath)
	cleanBase := filepath.Clean(baseDir)
	relPath, err := filepath.Rel(cleanBase, cleanTarget)
	if err != nil || strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
		return fmt.Errorf("invalid file path: path traversal detected")
	}
	return nil
}

// writeMessageToFile writes headers and body to the open file
func writeMessageToFile(file *os.File, msg *Message) error {
	// Write headers
	if _, err := fmt.Fprintf(file, "From: %s\r\n", msg.From); err != nil {
		return err
	}

	if len(msg.To) > 0 {
		toHeader := "To: " + strings.Join(msg.To, ", ") + "\r\n"
		if _, err := file.WriteString(toHeader); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(file, "Received: by badsmtp.test; %s\r\n", time.Now().Format(time.RFC1123Z)); err != nil {
		return err
	}

	if _, err := file.WriteString("\r\n"); err != nil {
		return err
	}

	// Write message body
	if _, err := file.WriteString(msg.Content); err != nil {
		return err
	}

	return nil
}

// ListMessages lists all messages in the mailbox (from both new/ and cur/ directories).
func (m *Mailbox) ListMessages() ([]string, error) {
	var allFiles []string

	// List messages in new/ and cur/ directories
	for _, subdir := range []string{"new", "cur"} {
		pattern := filepath.Join(m.Directory, subdir, "*")
		files, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to list messages in %s: %w", subdir, err)
		}
		allFiles = append(allFiles, files...)
	}

	return allFiles, nil
}

// DeleteMessage deletes a message from the mailbox.
// Filename should be just the basename, not a full path.
func (m *Mailbox) DeleteMessage(filename string) error {
	// Try both new/ and cur/ directories
	for _, subdir := range []string{"new", "cur"} {
		fullPath := filepath.Join(m.Directory, subdir, filename)

		// Security: Validate that the path is within the mailbox directory (prevent path traversal)
		if err := validatePathWithinDir(m.Directory, fullPath); err != nil {
			stdLogger.Warn("Security: Path traversal attempt detected in DeleteMessage", logging.F("filename", filename))
			return fmt.Errorf("invalid file path: path traversal detected")
		}

		// Try to delete if file exists
		if err := os.Remove(fullPath); err == nil {
			stdLogger.Info("Message deleted", logging.F("path", fullPath))
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete message: %w", err)
		}
	}

	return fmt.Errorf("message not found: %s", filename)
}

// Clear removes all messages from the mailbox (from both new/ and cur/).
func (m *Mailbox) Clear() error {
	files, err := m.ListMessages()
	if err != nil {
		return err
	}

	count := 0
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			stdLogger.Error("Failed to delete message", fmt.Errorf("%s: %v", file, err))
		} else {
			count++
		}
	}

	stdLogger.Info("Cleared messages from mailbox", logging.F("count", count))
	return nil
}
