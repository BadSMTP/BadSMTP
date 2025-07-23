package server

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// helper: create in-memory connection pair
func connPair() (c1, c2 net.Conn) {
	c1, c2 = net.Pipe()
	return
}

// Test that pipelining queues responses and flushes them in order
func TestPipeliningQueuesResponses(t *testing.T) {
	client, serverConn := connPair()
	defer client.Close()
	defer serverConn.Close()

	cfg := &Config{Port: 2525}
	sess := NewSession(serverConn, cfg, nil)

	// run session handler in background
	go func() {
		_ = sess.Handle()
	}()

	r := bufio.NewReader(client)
	// Read initial greeting (220)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	greet, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}
	if !strings.Contains(greet, "220") {
		t.Fatalf("expected 220 greeting, got: %q", greet)
	}

	// perform client writes in a separate goroutine so a blocked write doesn't deadlock the test
	go func() {
		// send EHLO to enable pipelining in our implementation
		_, _ = client.Write([]byte("EHLO example.com\r\n"))
		// small pause to emulate a client that sends commands back-to-back
		time.Sleep(10 * time.Millisecond)
		// send two NOOPs and then QUIT which should flush queued responses
		_, _ = client.Write([]byte("NOOP\r\nNOOP\r\nQUIT\r\n"))
	}()

	// we expect EHLO multiline response next
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, _ := r.ReadString('\n')
	if !strings.Contains(line, "250-badsmtp.test") {
		t.Fatalf("expected EHLO banner, got: %q", line)
	}
	// consume remaining EHLO lines until final 250 <space>
	for {
		client.SetReadDeadline(time.Now().Add(2 * time.Second))
		l, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("error reading EHLO continuation: %v", err)
		}
		if strings.HasPrefix(l, "250 ") {
			break
		}
	}

	// Now read the two NOOP responses - they were queued and should be delivered when QUIT triggers a flush
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n1, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read first NOOP response: %v", err)
	}
	if !strings.Contains(n1, "250") {
		t.Fatalf("expected 250 for NOOP, got: %q", n1)
	}

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n2, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read second NOOP response: %v", err)
	}
	if !strings.Contains(n2, "250") {
		t.Fatalf("expected 250 for NOOP, got: %q", n2)
	}

	// QUIT response
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	q, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read QUIT response: %v", err)
	}
	if !strings.Contains(q, "221") {
		t.Fatalf("expected 221 for QUIT, got: %q", q)
	}

	// clean up
	_ = client.Close()
	// give server a moment to exit
	time.Sleep(50 * time.Millisecond)
}
