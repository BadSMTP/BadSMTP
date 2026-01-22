package server

import (
	"fmt"
	"sync"
	"time"

	"badsmtp/auth"
	"badsmtp/logging"
	"badsmtp/storage"
)

const (
	// Default per-IP limits for the in-memory rate limiter
	defaultMaxConnsPerMinute    = 60
	defaultMaxMessagesPerMinute = 120

	// ServerGreeting is the banner shown in the SMTP 220 greeting
	ServerGreeting = "BadSMTP - The Reliably Unreliable Mail Server https://badsmtp.com"
)

var (
	defaultsLoggerCfg = logging.DefaultConfig()
	stdLogger         = logging.NewStdoutLogger(&defaultsLoggerCfg)
)

// DefaultMessageStore stores messages to local files.
type DefaultMessageStore struct {
	mailboxDir string
}

// NewDefaultMessageStore creates a file-based message store.
func NewDefaultMessageStore(mailboxDir string) *DefaultMessageStore {
	return &DefaultMessageStore{
		mailboxDir: mailboxDir,
	}
}

// Store saves a message to a local file.
func (dms *DefaultMessageStore) Store(msg *Message) error {
	mailbox, err := storage.NewMailbox(dms.mailboxDir)
	if err != nil {
		return fmt.Errorf("failed to create mailbox: %w", err)
	}

	storageMsg := &storage.Message{
		From:    msg.From,
		To:      msg.To,
		Content: msg.Content,
	}

	if err := mailbox.SaveMessage(storageMsg); err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	stdLogger.Info("Message stored locally",
		logging.F("from", msg.From),
		logging.F("to", msg.To),
		logging.F("size", msg.Size))
	return nil
}

// DefaultAuthenticator uses pattern-based authentication (goodauth/badauth).
// This is the current OSS behaviour for testing SMTP clients.
type DefaultAuthenticator struct{}

// NewDefaultAuthenticator creates a pattern-based authenticator.
func NewDefaultAuthenticator() *DefaultAuthenticator {
	return &DefaultAuthenticator{}
}

// Authenticate checks username against patterns (goodauth/badauth).
func (da *DefaultAuthenticator) Authenticate(username, _ string) (user *User, err error) {
	if !auth.IsValidAuth(username) {
		return nil, fmt.Errorf("authentication failed for user: %s", username)
	}

	user = &User{
		ID:       username,
		Username: username,
		Active:   true,
		Metadata: map[string]interface{}{
			"auth_method": "pattern",
		},
	}
	return user, nil
}

// NoOpObserver is a no-op implementation of SessionObserver.
// Used when no observers are registered.
type NoOpObserver struct{}

// OnConnect does nothing.
func (n *NoOpObserver) OnConnect(_ *SessionContext) {}

// OnAuth does nothing.
func (n *NoOpObserver) OnAuth(_ *SessionContext, _ *User) {}

// OnMessage does nothing.
func (n *NoOpObserver) OnMessage(_ *SessionContext, _ *Message) {}

// OnError does nothing.
func (n *NoOpObserver) OnError(_ *SessionContext, _ error, _ string) {}

// OnDisconnect does nothing.
func (n *NoOpObserver) OnDisconnect(_ *SessionContext, _ string) {}

// NoOpRateLimiter allows all connections and messages (default OSS behaviour).
type NoOpRateLimiter struct{}

// NewNoOpRateLimiter creates a rate limiter that allows everything.
func NewNoOpRateLimiter() *NoOpRateLimiter {
	return &NoOpRateLimiter{}
}

// AllowConnection always allows connections.
func (n *NoOpRateLimiter) AllowConnection(_ string) (ok bool, reason string) {
	return true, ""
}

// AllowMessage always allows messages.
func (n *NoOpRateLimiter) AllowMessage(_ *User, _ string) (ok bool, reason string) {
	return true, ""
}

// RecordConnection does nothing.
func (n *NoOpRateLimiter) RecordConnection(_ string) {}

// RecordMessage does nothing.
func (n *NoOpRateLimiter) RecordMessage(_ *User, _ string) {}

// ReleaseConnection does nothing.
func (n *NoOpRateLimiter) ReleaseConnection(_ string) {}

// SimpleRateLimiter is a basic in-memory rate limiter that enforces per-IP
// connection and message limits. It's intentionally simple: token counters with
// periodic resets. This is suitable as a default conservative limiter.
// Not intended for production-scale use.
type SimpleRateLimiter struct {
	mu sync.Mutex
	// per-IP state
	clients map[string]*clientState
	// limits
	maxConnsPerMinute    int
	maxMessagesPerMinute int
}

type clientState struct {
	connections int
	messages    int
	resetAt     time.Time
}

// NewSimpleRateLimiter creates a rate limiter with default reasonable limits.
func NewSimpleRateLimiter() *SimpleRateLimiter {
	return &SimpleRateLimiter{
		clients:              make(map[string]*clientState),
		maxConnsPerMinute:    defaultMaxConnsPerMinute,
		maxMessagesPerMinute: defaultMaxMessagesPerMinute,
	}
}

func (r *SimpleRateLimiter) resetIfNeeded(cs *clientState) {
	if time.Now().After(cs.resetAt) {
		cs.connections = 0
		cs.messages = 0
		cs.resetAt = time.Now().Add(time.Minute)
	}
}

// AllowConnection checks whether a new connection from clientIP should be allowed.
func (r *SimpleRateLimiter) AllowConnection(clientIP string) (allowed bool, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs, ok := r.clients[clientIP]
	if !ok {
		cs = &clientState{resetAt: time.Now().Add(time.Minute)}
		r.clients[clientIP] = cs
	}
	r.resetIfNeeded(cs)
	if cs.connections >= r.maxConnsPerMinute {
		return false, "rate limit exceeded: too many connections"
	}
	return true, ""
}

// AllowMessage checks whether a message from a user/client should be allowed.
func (r *SimpleRateLimiter) AllowMessage(_ *User, clientIP string) (allowed bool, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs, ok := r.clients[clientIP]
	if !ok {
		cs = &clientState{resetAt: time.Now().Add(time.Minute)}
		r.clients[clientIP] = cs
	}
	r.resetIfNeeded(cs)
	if cs.messages >= r.maxMessagesPerMinute {
		return false, "rate limit exceeded: too many messages"
	}
	return true, ""
}

// RecordConnection records that a connection was made for accounting.
func (r *SimpleRateLimiter) RecordConnection(clientIP string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs, ok := r.clients[clientIP]
	if !ok {
		cs = &clientState{resetAt: time.Now().Add(time.Minute)}
		r.clients[clientIP] = cs
	}
	r.resetIfNeeded(cs)
	cs.connections++
}

// RecordMessage records that a message was sent for accounting.
func (r *SimpleRateLimiter) RecordMessage(_ *User, clientIP string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs, ok := r.clients[clientIP]
	if !ok {
		cs = &clientState{resetAt: time.Now().Add(time.Minute)}
		r.clients[clientIP] = cs
	}
	r.resetIfNeeded(cs)
	cs.messages++
}

// ReleaseConnection records that a connection was closed.
func (r *SimpleRateLimiter) ReleaseConnection(_ string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// no-op for now; keep counts until reset window expires
}

// AllowAllAuthorizer allows all sending operations (default OSS behaviour).
type AllowAllAuthorizer struct{}

// NewAllowAllAuthorizer creates an authorizer that allows everything.
func NewAllowAllAuthorizer() *AllowAllAuthorizer {
	return &AllowAllAuthorizer{}
}

// CanSendFrom always returns true.
func (a *AllowAllAuthorizer) CanSendFrom(_ *User, _ string) bool { return true }

// CanSendTo always returns true.
func (a *AllowAllAuthorizer) CanSendTo(_ *User, _ string) bool { return true }

// GetQuota returns unlimited quota (-1).
func (a *AllowAllAuthorizer) GetQuota(_ *User) int64 { return -1 }

// DefaultCapabilityParser is a pass-through parser that doesn't modify capability parts.
// Extensions can provide custom implementations to extract tokens or custom data.
type DefaultCapabilityParser struct{}

// NewDefaultCapabilityParser creates a default capability parser.
func NewDefaultCapabilityParser() *DefaultCapabilityParser {
	return &DefaultCapabilityParser{}
}

// ParseCapabilities returns the parts unchanged with empty metadata (default behaviour).
func (p *DefaultCapabilityParser) ParseCapabilities(_ string, parts []string) (modifiedParts []string, metadata map[string]interface{}) {
	return parts, make(map[string]interface{})
}
