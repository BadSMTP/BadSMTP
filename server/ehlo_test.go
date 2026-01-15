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
			if body != "" && body != "OK" {
				if strings.Contains(body, " ") {
					parts := strings.SplitN(body, " ", 2)
					k := strings.ToUpper(parts[0])
					v := ""
					if len(parts) == 2 {
						v = parts[1]
					}
					ext[k] = v
				} else {
					// Extension without value (e.g., 8BITMIME, PIPELINING)
					ext[strings.ToUpper(body)] = ""
				}
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

// testCapabilityParser is a test implementation of CapabilityParser
// that demonstrates extracting custom data from capability parts.
// This is just an example - real extensions would implement their own parsing logic.
type testCapabilityParser struct{}

func (p *testCapabilityParser) ParseCapabilities(_ string, parts []string) (modifiedParts []string, metadata map[string]interface{}) {
	metadata = make(map[string]interface{})
	modifiedParts = []string{}

	// Example: Extract parts starting with "xtoken"
	for _, part := range parts {
		if strings.HasPrefix(part, "xtoken") {
			// Extract the token value after the prefix
			token := strings.TrimPrefix(part, "xtoken")
			metadata["auth_token"] = token
		} else {
			// Keep non-token parts for capability processing
			modifiedParts = append(modifiedParts, part)
		}
	}

	return modifiedParts, metadata
}

func TestCapabilityParserExtension(t *testing.T) {
	// Test that extensions can parse and extract custom data from EHLO hostname

	// Create session with custom parser
	client, serverConn := connPair()
	cfg := &Config{Port: 2525}
	cfg.EnsureDefaults()
	cfg.CapabilityParser = &testCapabilityParser{}

	sess := NewSession(serverConn, cfg, nil)
	go func() { _ = sess.Handle() }()
	defer func() { client.Close(); serverConn.Close(); time.Sleep(10 * time.Millisecond) }()

	// Send EHLO with custom token part and capabilities (DNS-compatible, no underscores)
	cli := doEhloClient(t, client, "xtoken9abc123xyz-size50000-authplain.example.com")
	defer cli.Close()

	// Verify capabilities were processed correctly (token part removed)
	if ok, val := cli.Extension("SIZE"); !ok || val != "50000" {
		t.Fatalf("SIZE extension not set correctly, got: %v", val)
	}

	if ok, val := cli.Extension("AUTH"); !ok || !strings.Contains(val, "PLAIN") {
		t.Fatalf("AUTH extension not set correctly, got: %v", val)
	}

	// Verify token was extracted to session metadata
	if sess.metadata == nil {
		t.Fatal("Session metadata not initialised")
	}

	token, ok := sess.metadata["auth_token"]
	if !ok {
		t.Fatal("auth_token not found in session metadata")
	}

	if token != "9abc123xyz" {
		t.Fatalf("Expected token '9abc123xyz', got: %v", token)
	}
}

func TestCombinedCapabilityLabels(t *testing.T) {
	// Test combined capability labels using dash-separated format
	cases := []struct {
		host           string
		wantSize       string
		want8BitMIME   bool
		wantAuthSubstr string
		wantPipelining bool
	}{
		{
			host:           "size10000-no8bit-authplain.example.com",
			wantSize:       "10000",
			want8BitMIME:   false,
			wantAuthSubstr: "PLAIN",
			wantPipelining: true,
		},
		{
			host:           "nosize-nopipelining-authlogin.example.com",
			wantSize:       "",
			want8BitMIME:   true,
			wantAuthSubstr: "LOGIN",
			wantPipelining: false,
		},
		{
			host:           "size50000-authcram-nopipelining.example.com",
			wantSize:       "50000",
			want8BitMIME:   true,
			wantAuthSubstr: "CRAM-MD5",
			wantPipelining: false,
		},
		{
			// Test that last auth option takes precedence
			host:           "authplain-authoauth.example.com",
			wantSize:       "10485760",
			want8BitMIME:   true,
			wantAuthSubstr: "XOAUTH2",
			wantPipelining: true,
		},
	}

	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			client, cleanup := startSession(t)
			defer cleanup()

			cli := doEhloClient(t, client, c.host)
			defer cli.Close()

			// Check SIZE extension
			if c.wantSize != "" {
				if ok, val := cli.Extension("SIZE"); !ok {
					t.Fatalf("EHLO %s did not advertise SIZE", c.host)
				} else if val != c.wantSize {
					t.Fatalf("EHLO %s advertised SIZE=%s, want %s", c.host, val, c.wantSize)
				}
			} else {
				if ok, _ := cli.Extension("SIZE"); ok {
					t.Fatalf("EHLO %s advertised SIZE but should not", c.host)
				}
			}

			// Check 8BITMIME extension
			if has8bit, _ := cli.Extension("8BITMIME"); has8bit != c.want8BitMIME {
				t.Fatalf("EHLO %s 8BITMIME=%v, want %v", c.host, has8bit, c.want8BitMIME)
			}

			// Check AUTH extension
			if ok, val := cli.Extension("AUTH"); !ok {
				t.Fatalf("EHLO %s did not advertise AUTH", c.host)
			} else if !strings.Contains(val, c.wantAuthSubstr) {
				t.Fatalf("EHLO %s AUTH=%q does not contain %q", c.host, val, c.wantAuthSubstr)
			}

			// Check PIPELINING extension
			if hasPipelining, _ := cli.Extension("PIPELINING"); hasPipelining != c.wantPipelining {
				t.Fatalf("EHLO %s PIPELINING=%v, want %v", c.host, hasPipelining, c.wantPipelining)
			}
		})
	}
}
