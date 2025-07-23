package server_test

import (
	"bufio"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"badsmtp/server"
	smtpPkg "badsmtp/smtp"
)

// connPair mirrors the helper used in other server tests
func connPair() (c1, c2 net.Conn) {
	c1, c2 = net.Pipe()
	return
}

func sendCmd(conn net.Conn, r *bufio.Reader, cmd string) (string, error) {
	// set a combined deadline for the roundtrip to avoid hanging forever
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	defer conn.SetDeadline(time.Time{})

	_, err := conn.Write([]byte(cmd + "\r\n"))
	if err != nil {
		return "", err
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// has421 returns true if either the response string or the error indicates a 421 response.
// Strict detection: either a numeric 421 prefix (after trimming leading space) OR the
// response/error exactly equals the canonical message returned by smtpPkg.GetErrorMessage(421)
// (case-insensitive comparison). No fuzzy substring heuristics or reverse lookups are used.
func has421(resp string, err error) bool {
	canon := strings.ToLower(strings.TrimSpace(smtpPkg.GetErrorMessage(smtpPkg.Code421)))

	// check response string first
	if resp != "" {
		r := strings.TrimSpace(resp)
		// numeric prefix
		if strings.HasPrefix(r, "421") {
			return true
		}
		// exact canonical message match (case-insensitive)
		if strings.EqualFold(r, canon) || strings.EqualFold(strings.TrimPrefix(r, "421 "), canon) {
			return true
		}
	}

	// check error if any
	if err == nil {
		return false
	}
	if te, ok := err.(*textproto.Error); ok {
		r := strings.TrimSpace(te.Msg)
		if strings.HasPrefix(r, "421") {
			return true
		}
		if strings.EqualFold(r, canon) || strings.EqualFold(strings.TrimPrefix(r, "421 "), canon) {
			return true
		}
		return false
	}

	// generic error string
	errStr := strings.TrimSpace(err.Error())
	if strings.HasPrefix(errStr, "421") {
		return true
	}
	if strings.EqualFold(errStr, canon) || strings.EqualFold(strings.TrimPrefix(errStr, "421 "), canon) {
		return true
	}
	return false
}

func Test421DropsConnection(t *testing.T) {
	cases := []struct {
		name  string
		setup []string
		trig  string
	}{
		{"MAIL", []string{"EHLO client.example.com"}, "mail421@example.com"},
		{"RCPT", []string{"EHLO client.example.com", "user@example.com"}, "rcpt421@example.com"},
		{"DATA", []string{"EHLO client.example.com", "data421@example.com", "user@example.com"}, "DATA"},
		{"RSET", []string{"EHLO client.example.com", "rset421@example.com"}, "RSET"},
		{"NOOP", []string{"EHLO client.example.com", "noop421@example.com"}, "NOOP"},
		{"QUIT", []string{"EHLO client.example.com", "quit421@example.com"}, "QUIT"},
		{"HELO_EHLO_HOST", nil, "EHLO helo421.example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, serverConn := connPair()
			defer client.Close()
			defer serverConn.Close()

			cfg := &server.Config{Port: 2525}
			sess := server.NewSession(serverConn, cfg, nil)
			go func() { _ = sess.Handle() }()

			// Prepare a reader for raw reads when needed (do not pre-read greeting here)
			r := bufio.NewReader(client)

			// For all cases read the initial banner (net/smtp.NewClient previously expected to read it)
			client.SetDeadline(time.Now().Add(2 * time.Second))
			greet, err := r.ReadString('\n')
			if err != nil {
				t.Fatalf("failed to read greeting: %v", err)
			}
			if !strings.Contains(greet, "220") {
				t.Fatalf("expected 220 greeting, got: %q", greet)
			}
			// clear deadline so subsequent operations can set their own
			_ = client.SetDeadline(time.Time{})

			if tc.name == "HELO_EHLO_HOST" {
				// Raw EHLO with hostname trigger (no further EHLO multiline read required)
				_, err := client.Write([]byte(tc.trig + "\r\n"))
				if err != nil {
					t.Fatalf("write EHLO error: %v", err)
				}
				line, _ := r.ReadString('\n')
				if !strings.HasPrefix(line, "421") {
					t.Fatalf("expected 421 reply, got: %q", line)
				}
				client.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				_, err = r.ReadByte()
				if err == nil {
					t.Fatalf("expected connection closed after 421, but read succeeded")
				}
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					t.Fatalf("server did not close connection quickly after 421")
				}
				return
			}

			// Send EHLO and consume multiline
			if _, err := client.Write([]byte("EHLO client.example.com\r\n")); err != nil {
				t.Fatalf("write EHLO failed: %v", err)
			}
			for {
				// per-read deadline to avoid hangs
				_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
				line, err := r.ReadString('\n')
				_ = client.SetReadDeadline(time.Time{})
				if err != nil {
					t.Fatalf("error reading EHLO response: %v", err)
				}
				if strings.HasPrefix(line, "250 ") {
					break
				}
			}

			switch tc.name {
			case "MAIL":
				resp, err := sendCmd(client, r, "MAIL FROM:<"+tc.trig+">")
				if !has421(resp, err) {
					if err != nil {
						t.Fatalf("expected MAIL to fail with 421, got err=%v resp=%q", err, resp)
					}
					t.Fatalf("expected MAIL to fail with 421, but got success: %q", resp)
				}

			case "RCPT":
				// MAIL FROM
				if _, err := sendCmd(client, r, "MAIL FROM:<"+tc.setup[1]+">"); err != nil {
					t.Fatalf("MAIL FROM failed: %v", err)
				}
				// RCPT which should trigger 421
				resp, err := sendCmd(client, r, "RCPT TO:<"+tc.trig+">")
				if !has421(resp, err) {
					if err != nil {
						t.Fatalf("expected RCPT to fail with 421, got err=%v resp=%q", err, resp)
					}
					t.Fatalf("expected RCPT to fail with 421, but got success: %q", resp)
				}

			case "DATA":
				if _, err := sendCmd(client, r, "MAIL FROM:<"+tc.setup[1]+">"); err != nil {
					t.Fatalf("MAIL FROM failed: %v", err)
				}
				if _, err := sendCmd(client, r, "RCPT TO:<"+tc.setup[2]+">"); err != nil {
					t.Fatalf("RCPT failed: %v", err)
				}
				resp, err := sendCmd(client, r, "DATA")
				if !has421(resp, err) {
					if err != nil {
						t.Fatalf("expected DATA to fail with 421, got err=%v resp=%q", err, resp)
					}
					t.Fatalf("expected DATA to fail with 421, but got success: %q", resp)
				}

			case "RSET":
				if _, err := sendCmd(client, r, "MAIL FROM:<"+tc.setup[1]+">"); err != nil {
					t.Fatalf("MAIL FROM failed: %v", err)
				}
				resp, err := sendCmd(client, r, "RSET")
				if !has421(resp, err) {
					t.Fatalf("expected 421 reply on RSET, got: %q", resp)
				}

			case "NOOP":
				if _, err := sendCmd(client, r, "MAIL FROM:<"+tc.setup[1]+">"); err != nil {
					t.Fatalf("MAIL FROM failed: %v", err)
				}
				resp, err := sendCmd(client, r, "NOOP")
				if !has421(resp, err) {
					t.Fatalf("expected 421 reply on NOOP, got: %q", resp)
				}

			case "QUIT":
				if _, err := sendCmd(client, r, "MAIL FROM:<"+tc.setup[1]+">"); err != nil {
					t.Fatalf("MAIL FROM failed: %v", err)
				}
				resp, err := sendCmd(client, r, "QUIT")
				if !has421(resp, err) {
					t.Fatalf("expected QUIT to result in 421 error, but got success: %q", resp)
				}
			}

			// After triggering, ensure connection closed quickly
			client.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
			_, err = r.ReadByte()
			if err == nil {
				t.Fatalf("expected connection to be closed after 421, but read succeeded")
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatalf("server did not close connection quickly after 421 (timeout)")
			}
		})
	}
}
