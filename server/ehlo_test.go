package server

import (
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// EHLOClient is a minimal client used in tests that speaks enough SMTP
// to perform EHLO and MAIL commands and expose extensions.
type EHLOClient struct {
	conn net.Conn
	tp   *textproto.Conn
	text map[string]string
}

// Close closes the underlying connection.
func (c *EHLOClient) Close() error {
	if c.tp != nil {
		_ = c.tp.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Extension reports whether an EHLO extension is present and returns its parameter.
func (c *EHLOClient) Extension(name string) (ok bool, val string) {
	if c.text == nil {
		return false, ""
	}
	val, ok = c.text[strings.ToUpper(name)]
	return
}

// Mail sends a MAIL FROM command and returns an error if the server responded with an error code.
func (c *EHLOClient) Mail(addr string) error {
	if _, err := c.tp.Cmd("MAIL FROM:<%s>", addr); err != nil {
		return err
	}
	// Read response line
	line, err := c.tp.ReadLine()
	if err != nil {
		return err
	}
	// textproto returns the raw line; parse leading code
	if len(line) >= 3 {
		if code := line[:3]; code >= "400" {
			return fmt.Errorf("%s", line)
		}
	}
	return nil
}

// doEhloClient performs EHLO and returns an EHLOClient with parsed extensions.
func doEhloClient(t *testing.T, client net.Conn, hostname string) *EHLOClient {
	// Wrap with textproto.Conn to make command/response handling easier
	tp := textproto.NewConn(client)
	// Read the initial greeting (textproto expects the server to send it first)
	// Use ReadLine to consume the greeting
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := tp.ReadLine(); err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}
	_ = client.SetReadDeadline(time.Time{})

	// Send EHLO
	if err := tp.PrintfLine("EHLO %s", hostname); err != nil {
		t.Fatalf("failed to send EHLO: %v", err)
	}

	// Read EHLO multiline response: lines beginning with 250- and final 250
	ext := make(map[string]string)
	for {
		line, err := tp.ReadLine()
		if err != nil {
			t.Fatalf("error reading EHLO response: %v", err)
		}
		if strings.HasPrefix(line, "250-") || strings.HasPrefix(line, "250 ") {
			// trim leading code and optional dash/space
			body := strings.TrimSpace(line[4:])
			if strings.Contains(body, " ") {
				parts := strings.SplitN(body, " ", 2)
				k := strings.ToUpper(parts[0])
				v := ""
				if len(parts) == 2 {
					v = parts[1]
				}
				ext[k] = v
			}
			if strings.HasPrefix(line, "250 ") {
				break
			}
		}
	}

	return &EHLOClient{conn: client, tp: tp, text: ext}
}

func startSession(_ *testing.T) (client net.Conn, cleanup func()) {
	client, serverConn := connPair()
	cfg := &Config{Port: 2525}
	sess := NewSession(serverConn, cfg, nil)
	go func() { _ = sess.Handle() }()
	cleanup = func() { client.Close(); serverConn.Close(); time.Sleep(10 * time.Millisecond) }
	return
}

func TestEHLOSizeAdvertisementAndClamping(t *testing.T) {
	cases := []struct {
		host     string
		expected string
	}{
		{"size100000.example.com", "100000"},
		{"size1.example.com", "1000"},
		{"size20000000.example.com", "10000000"},
	}

	for _, c := range cases {
		client, cleanup := startSession(t)
		func() {
			defer cleanup()
			cli := doEhloClient(t, client, c.host)
			defer cli.Close()
			// Check SIZE extension value
			if ok, val := cli.Extension("SIZE"); !ok {
				t.Fatalf("EHLO %s did not advertise SIZE", c.host)
			} else if val != c.expected {
				t.Fatalf("EHLO %s advertised SIZE=%s, want %s", c.host, val, c.expected)
			}
		}()
	}
}

func TestEnhancedStatusCodesToggle(t *testing.T) {
	// Case 1: default (enhanced enabled) - MAIL FROM with extended should return extended code
	client, cleanup := startSession(t)
	defer cleanup()
	cli := doEhloClient(t, client, "example.com")
	defer cli.Close()

	// send MAIL FROM that encodes extended error
	if err := cli.Mail("mail550_5.1.1@example.com"); err == nil {
		t.Fatalf("expected MAIL FROM to error with extended code, but got success")
	} else if !strings.Contains(err.Error(), "5.1.1") {
		// textproto error will be returned via error string
		t.Fatalf("expected extended code in response when enhanced codes enabled; got: %v", err)
	}

	// Case 2: disabled via EHLO hostname
	client2, cleanup2 := startSession(t)
	defer cleanup2()
	cli2 := doEhloClient(t, client2, "noenhancedstatuscodes.example.com")
	defer cli2.Close()

	if err := cli2.Mail("mail550_5.1.1@example.com"); err == nil {
		t.Fatalf("expected MAIL FROM to error when enhanced codes disabled, but got success")
	} else if strings.Contains(err.Error(), "5.1.1") {
		t.Fatalf("did not expect extended code when enhanced codes disabled; got: %v", err)
	} else if !strings.HasPrefix(err.Error(), "550") {
		t.Fatalf("expected generic 550 response when enhanced codes disabled; got: %v", err)
	}
}

func TestAuthMechanismKeywords(t *testing.T) {
	cases := []struct{ host, want string }{
		{"authplain.example.com", "PLAIN"},
		{"authlogin.example.com", "LOGIN"},
		{"authcram.example.com", "CRAM-MD5"},
		{"authoauth.example.com", "XOAUTH2"},
	}

	for _, c := range cases {
		client, cleanup := startSession(t)
		func() {
			defer cleanup()
			cli := doEhloClient(t, client, c.host)
			defer cli.Close()
			if ok, val := cli.Extension("AUTH"); !ok {
				t.Fatalf("EHLO %s did not advertise AUTH", c.host)
			} else if !strings.Contains(val, c.want) {
				t.Fatalf("EHLO %s AUTH=%q does not contain %q", c.host, val, c.want)
			}
		}()
	}
}
