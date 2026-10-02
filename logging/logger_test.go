package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

var errExample = errors.New("example failure")

// parseJSONLine unmarshals a single JSON log line into a map.
func parseJSONLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("log line is not valid JSON (%v): %q", err, line)
	}
	return m
}

func TestJSONOutputContainsMessageLevelAndFields(t *testing.T) {
	var buf bytes.Buffer
	cfg := LogConfig{Level: INFO, Format: "json"}
	logger := newSlogLogger(&buf, &cfg)

	logger.Info("hello", F("client_ip", "127.0.0.1"), F("port", 2525))

	m := parseJSONLine(t, strings.TrimSpace(buf.String()))
	if m["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", m["msg"])
	}
	if m["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", m["level"])
	}
	if m["client_ip"] != "127.0.0.1" {
		t.Errorf("client_ip = %v, want 127.0.0.1", m["client_ip"])
	}
	// numbers decode as float64 from JSON
	if m["port"] != float64(2525) {
		t.Errorf("port = %v, want 2525", m["port"])
	}
}

func TestTextOutput(t *testing.T) {
	var buf bytes.Buffer
	cfg := LogConfig{Level: INFO, Format: "text"}
	logger := newSlogLogger(&buf, &cfg)

	logger.Warn("careful", F("port", 25))

	out := buf.String()
	for _, want := range []string{"level=WARN", `msg=careful`, "port=25"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output %q missing %q", out, want)
		}
	}
}

func TestErrorIncludesErrorString(t *testing.T) {
	var buf bytes.Buffer
	cfg := LogConfig{Level: INFO, Format: "json"}
	logger := newSlogLogger(&buf, &cfg)

	logger.Error("boom", errExample, F("k", "v"))

	m := parseJSONLine(t, strings.TrimSpace(buf.String()))
	if m["error"] != "example failure" {
		t.Errorf("error = %v, want %q", m["error"], "example failure")
	}
	if m["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", m["level"])
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	cfg := LogConfig{Level: INFO, Format: "json"}
	logger := newSlogLogger(&buf, &cfg)

	logger.Debug("suppressed")
	if buf.Len() != 0 {
		t.Errorf("debug message should be filtered at INFO level, got %q", buf.String())
	}

	logger.Info("shown")
	if !strings.Contains(buf.String(), "shown") {
		t.Errorf("info message should be emitted at INFO level, got %q", buf.String())
	}
}

func TestSetLevelIsLive(t *testing.T) {
	var buf bytes.Buffer
	cfg := LogConfig{Level: INFO, Format: "json"}
	logger := newSlogLogger(&buf, &cfg)

	logger.Debug("before")
	if buf.Len() != 0 {
		t.Fatalf("debug should be filtered before SetLevel, got %q", buf.String())
	}

	logger.SetLevel(DEBUG)
	logger.Debug("after")
	if !strings.Contains(buf.String(), "after") {
		t.Errorf("debug should be emitted after SetLevel(DEBUG), got %q", buf.String())
	}
}

func TestWithAddsFieldsToEveryRecord(t *testing.T) {
	var buf bytes.Buffer
	cfg := LogConfig{Level: INFO, Format: "json"}
	base := newSlogLogger(&buf, &cfg)

	child := base.With(F("session_id", "sess_abc"))
	child.Info("one")
	child.Info("two")

	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		m := parseJSONLine(t, line)
		if m["session_id"] != "sess_abc" {
			t.Errorf("record %q missing session_id", line)
		}
	}
}

// TestConcurrentLoggingProducesIntactLines verifies that a logger shared across
// goroutines (as sessions share one) serialises its writes: every emitted line
// is complete, valid JSON rather than interleaved output. Run with -race.
func TestConcurrentLoggingProducesIntactLines(t *testing.T) {
	var buf bytes.Buffer
	cfg := LogConfig{Level: INFO, Format: "json"}
	logger := newSlogLogger(&buf, &cfg)

	const goroutines, perGoroutine = 16, 50
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			child := logger.With(F("worker", id))
			for i := range perGoroutine {
				child.Info("tick", F("i", i))
			}
		}(g)
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != goroutines*perGoroutine {
		t.Fatalf("expected %d lines, got %d", goroutines*perGoroutine, len(lines))
	}
	for _, line := range lines {
		parseJSONLine(t, line) // fails the test if any line is not intact JSON
	}
}
