package server

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

func TestVrfyPlainAddressResponses(t *testing.T) {
	client, serverConn := connPair()
	defer client.Close()
	defer serverConn.Close()

	cfg := &Config{Port: 2525}
	sess := NewSession(serverConn, cfg, nil)
	go func() { _ = sess.Handle() }()

	r := bufio.NewReader(client)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}

	cases := []struct {
		input      string
		wantPrefix string
	}{
		{"VRFY exists@example.com\r\n", "250"},
		{"VRFY unknown@example.com\r\n", "551"},
		{"VRFY ambiguous@example.com\r\n", "553"},
		{"VRFY other@example.com\r\n", "550"},
	}

	for _, c := range cases {
		client.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if _, err := client.Write([]byte(c.input)); err != nil {
			t.Fatalf("failed to write %q: %v", c.input, err)
		}
		client.SetReadDeadline(time.Now().Add(2 * time.Second))
		resp, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("failed to read response for %q: %v", c.input, err)
		}
		if !strings.HasPrefix(resp, c.wantPrefix) {
			t.Fatalf("VRFY %q: expected prefix %q, got %q", c.input, c.wantPrefix, resp)
		}
	}
}

func TestVrfyDisplayNameAndUTF8(t *testing.T) {
	client, serverConn := connPair()
	defer client.Close()
	defer serverConn.Close()

	cfg := &Config{Port: 2525}
	sess := NewSession(serverConn, cfg, nil)
	// enable SMTPUTF8 capability via EHLO hostname
	_ = sess.buildEhloResponse("smtputf8.example.com")
	go func() { _ = sess.Handle() }()

	r := bufio.NewReader(client)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}

	// Display name form
	client.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Write([]byte("VRFY \"Alice Example\" <exists@example.com>\r\n")); err != nil {
		t.Fatalf("failed to write VRFY display-name: %v", err)
	}
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read VRFY response: %v", err)
	}
	if !strings.HasPrefix(resp, "250") {
		t.Fatalf("expected 250 for VRFY with exists user, got %q", resp)
	}

	// UTF8 local part is accepted only if SMTPUTF8 is enabled; earlier we forced capability in response building,
	// but EHLO wasn't actually sent to the client; simulate by directly enabling capability on session
	sess.capabilities.SMTPUTF8 = true
	client.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Write([]byte("VRFY usér@例え.jp\r\n")); err != nil {
		t.Fatalf("failed to write VRFY UTF8 addr: %v", err)
	}
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read VRFY UTF8 response: %v", err)
	}
	// local part doesn't start with exists/unknown/ambiguous, so default 550
	if !strings.HasPrefix(resp2, "550") {
		t.Fatalf("expected 550 for VRFY UTF8 when local not special, got %q", resp2)
	}
}
