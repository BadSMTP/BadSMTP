// Package auth provides authentication functionality for BadSMTP.
package auth

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"net"
	"net/textproto"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	oauthUserRe = regexp.MustCompile(`user=([^,\x01]+)`)
)

// Handler is the interface for authentication handlers.
type Handler interface {
	Authenticate(conn net.Conn, parts []string) (string, error)
}

// PlainHandler implements the PLAIN authentication mechanism.
type PlainHandler struct{}

// LoginHandler implements the LOGIN authentication mechanism.
type LoginHandler struct{}

// CramHandler implements the CRAM-MD5 and CRAM-SHA256 authentication mechanisms.
type CramHandler struct {
	HashFunc func() hash.Hash
	Name     string
}

// XOAuth2Handler implements the XOAUTH2 authentication mechanism.
type XOAuth2Handler struct{}

// Authenticate handles PLAIN authentication.
func (h *PlainHandler) Authenticate(conn net.Conn, parts []string) (string, error) {
	var authData string

	// Check if auth data is provided in the command args (AUTH PLAIN <data>)
	if len(parts) >= 3 {
		authData = parts[2]
	} else if len(parts) == 2 {
		// Interactive mode - read from connection using textproto
		br := bufio.NewReader(conn)
		tp := textproto.NewReader(br)
		if line, err := tp.ReadLine(); err == nil {
			authData = strings.TrimSpace(line)
		}
	} else {
		return "", fmt.Errorf("invalid PLAIN command")
	}

	if authData == "" {
		return "", fmt.Errorf("no auth data provided")
	}

	decoded, err := base64.StdEncoding.DecodeString(authData)
	if err != nil {
		return "", fmt.Errorf("invalid base64")
	}

	// PLAIN format: \0username\0password
	authParts := strings.SplitN(string(decoded), "\x00", 3)
	if len(authParts) != 3 {
		return "", fmt.Errorf("invalid PLAIN format")
	}

	return authParts[1], nil
}

// Authenticate handles LOGIN authentication.
func (h *LoginHandler) Authenticate(conn net.Conn, _ []string) (string, error) {
	// Send username prompt
	usernamePrompt := "334 " + base64.StdEncoding.EncodeToString([]byte("Username:"))
	if _, err := conn.Write([]byte(usernamePrompt + "\r\n")); err != nil {
		return "", err
	}

	br := bufio.NewReader(conn)
	tp := textproto.NewReader(br)
	usernameLine, err := tp.ReadLine()
	if err != nil {
		return "", fmt.Errorf("failed to read username")
	}
	usernameB64 := strings.TrimSpace(usernameLine)
	username, err := base64.StdEncoding.DecodeString(usernameB64)
	if err != nil {
		return "", fmt.Errorf("invalid username encoding")
	}

	// Send password prompt
	passwordPrompt := "334 " + base64.StdEncoding.EncodeToString([]byte("Password:"))
	if _, err := conn.Write([]byte(passwordPrompt + "\r\n")); err != nil {
		return "", err
	}

	passwordLine, err := tp.ReadLine()
	if err != nil {
		return "", fmt.Errorf("failed to read password")
	}

	// We don't actually verify the password, just return username
	_ = strings.TrimSpace(passwordLine)
	return string(username), nil
}

// Authenticate handles CRAM-MD5 and CRAM-SHA256 authentication.
func (h *CramHandler) Authenticate(conn net.Conn, _ []string) (string, error) {
	challenge := fmt.Sprintf("<%d.%d@badsmtp.test>", time.Now().Unix(), os.Getpid())
	challengeB64 := base64.StdEncoding.EncodeToString([]byte(challenge))

	response := "334 " + challengeB64
	if _, err := conn.Write([]byte(response + "\r\n")); err != nil {
		return "", err
	}

	br := bufio.NewReader(conn)
	tp := textproto.NewReader(br)
	responseLine, err := tp.ReadLine()
	if err != nil {
		return "", fmt.Errorf("failed to read response")
	}
	responseB64 := strings.TrimSpace(responseLine)
	decoded, err := base64.StdEncoding.DecodeString(responseB64)
	if err != nil {
		return "", fmt.Errorf("invalid response encoding")
	}

	// Parse username from response (format: "username hash")
	responseParts := strings.SplitN(string(decoded), " ", 2)
	if len(responseParts) != 2 {
		return "", fmt.Errorf("invalid response format")
	}

	return responseParts[0], nil
}

// Authenticate handles XOAUTH2 authentication.
func (h *XOAuth2Handler) Authenticate(conn net.Conn, parts []string) (string, error) {
	var authDataB64 string

	// Check if auth data is provided in the command args (AUTH XOAUTH2 <data>)
	if len(parts) >= 3 {
		authDataB64 = parts[2]
	} else if len(parts) == 2 {
		// Interactive mode - send challenge and read from connection
		if _, err := conn.Write([]byte("334 \r\n")); err != nil {
			return "", err
		}

		br := bufio.NewReader(conn)
		tp := textproto.NewReader(br)
		line, err := tp.ReadLine()
		if err != nil {
			return "", fmt.Errorf("failed to read response")
		}
		authDataB64 = strings.TrimSpace(line)
	} else {
		return "", fmt.Errorf("invalid XOAUTH2 command")
	}

	if authDataB64 == "" {
		return "", fmt.Errorf("no auth data provided")
	}

	decoded, err := base64.StdEncoding.DecodeString(authDataB64)
	if err != nil {
		return "", fmt.Errorf("invalid base64")
	}

	// Extract username from OAuth2 string (simplified)
	authString := string(decoded)
	matches := oauthUserRe.FindStringSubmatch(authString)

	if len(matches) < 2 {
		return "", fmt.Errorf("username not found in OAuth2 string")
	}

	return matches[1], nil
}

const (
	// AuthMechanismPlain represents the PLAIN authentication mechanism.
	AuthMechanismPlain = "PLAIN"

	// AuthMechanismLogin represents the LOGIN authentication mechanism.
	AuthMechanismLogin = "LOGIN"

	// AuthMechanismCramMD5 represents the CRAM-MD5 authentication mechanism.
	AuthMechanismCramMD5 = "CRAM-MD5"

	// AuthMechanismCramSHA256 represents the CRAM-SHA256 authentication mechanism.
	AuthMechanismCramSHA256 = "CRAM-SHA256"

	// AuthMechanismXOAuth2 represents the XOAUTH2 authentication mechanism.
	AuthMechanismXOAuth2 = "XOAUTH2"
)

// NewHandler creates a new authentication handler for the specified mechanism.
func NewHandler(mechanism string) Handler {
	switch strings.ToUpper(mechanism) {
	case AuthMechanismPlain:
		return &PlainHandler{}
	case AuthMechanismLogin:
		return &LoginHandler{}
	case AuthMechanismCramMD5:
		return &CramHandler{HashFunc: sha256.New, Name: AuthMechanismCramMD5}
	case AuthMechanismCramSHA256:
		return &CramHandler{HashFunc: sha256.New, Name: AuthMechanismCramSHA256}
	case AuthMechanismXOAuth2:
		return &XOAuth2Handler{}
	default:
		return nil
	}
}

// IsValidAuth checks if the provided username is valid for authentication.
func IsValidAuth(username string) bool {
	return !strings.Contains(username, "badauth")
}

// GenerateCramResponse generates a CRAM-SHA256 response.
// Helper functions for CRAM authentication
func GenerateCramResponse(username, password, challenge string) string {
	h := hmac.New(sha256.New, []byte(password))
	h.Write([]byte(challenge))
	hash := hex.EncodeToString(h.Sum(nil))
	return username + " " + hash
}

// GenerateCramSHA256Response generates a CRAM-SHA256 response.
func GenerateCramSHA256Response(username, password, challenge string) string {
	h := hmac.New(sha256.New, []byte(password))
	h.Write([]byte(challenge))
	hash := hex.EncodeToString(h.Sum(nil))
	return username + " " + hash
}

// RedactAuthArgs returns a copy of args safe for logging by redacting any
// credential/token payloads typically present in AUTH commands.
// Examples:
//   - AUTH PLAIN <base64> -> AUTH PLAIN [redacted]
//   - AUTH XOAUTH2 <base64> -> AUTH XOAUTH2 [redacted]
func RedactAuthArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	out := make([]string, len(args))
	copy(out, args)
	// AUTH mechanisms normally have the credential data in args[1]
	if len(out) > 1 {
		out[1] = "[redacted]"
	}
	return out
}
