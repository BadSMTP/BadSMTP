package server

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// TestShutdownSends421 verifies that when Shutdown is invoked, connected clients
// receive a 421 response and the connection is closed.
func TestShutdownSends421(t *testing.T) {
	// Reserve a port by listening on :0, then close and reuse the port for the server.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to reserve port: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	port := addr.Port
	_ = l.Close()

	cfg := &Config{Port: port}
	cfg.EnsureDefaults()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start the server in background
	go func() {
		_ = srv.Start()
	}()

	// Allow listener to start
	time.Sleep(150 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	r := bufio.NewReader(conn)
	// Read greeting line
	greet, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}
	if !strings.Contains(greet, "220") {
		t.Fatalf("expected 220 greeting, got %q", greet)
	}

	// Trigger graceful shutdown with 5s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	// Expect a 421 response during shutdown
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("expected 421 response, read error: %v", err)
	}
	if !strings.Contains(resp, "421") {
		t.Fatalf("expected 421 response, got %q", resp)
	}
}

// TestShutdownMultipleClients verifies that Shutdown notifies all active sessions.
func TestShutdownMultipleClients(t *testing.T) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to reserve port: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	port := addr.Port
	_ = l.Close()

	cfg := &Config{Port: port}
	cfg.EnsureDefaults()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	go func() { _ = srv.Start() }()
	// Allow listener to start
	time.Sleep(150 * time.Millisecond)

	conns := make([]net.Conn, 0, 2)
	readers := make([]*bufio.Reader, 0, 2)
	for i := 0; i < 2; i++ {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
		if err != nil {
			t.Fatalf("failed to connect client %d: %v", i, err)
		}
		conns = append(conns, c)
		readers = append(readers, bufio.NewReader(c))
		// consume greeting
		if _, err := readers[i].ReadString('\n'); err != nil {
			for _, cc := range conns {
				_ = cc.Close()
			}
			t.Fatalf("failed to read greeting for client %d: %v", i, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	for i, r := range readers {
		_ = conns[i].SetReadDeadline(time.Now().Add(2 * time.Second))
		resp, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("expected 421 for client %d, read error: %v", i, err)
		}
		if !strings.Contains(resp, "421") {
			t.Fatalf("expected 421 for client %d, got %q", i, resp)
		}
		_ = conns[i].Close()
	}
}
