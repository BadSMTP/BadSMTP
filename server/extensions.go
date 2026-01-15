package server

import (
	"badsmtp/smtp"
)

// Extensions provides pluggable functionality for BadSMTP.
// These interfaces allow external packages to extend core SMTP functionality
// without modifying the core codebase.

// User represents an authenticated user in the system.
type User struct {
	ID       string                 // Unique identifier
	Username string                 // Username or email
	Active   bool                   // Whether the user is active
	Metadata map[string]interface{} // Extension data (quota, plan, etc.)
}

// Message represents an SMTP message with all relevant context.
type Message struct {
	From    string            // Envelope sender (MAIL FROM)
	To      []string          // Envelope recipients (RCPT TO)
	Content string            // Full message content including headers
	Headers map[string]string // Parsed headers
	Size    int               // Message size in bytes

	// Context
	ClientIP  string // Client IP address
	Hostname  string // Server hostname used
	TLSUsed   bool   // Whether TLS was used
	Timestamp string // ISO 8601 timestamp
}

// MessageStore handles storage of received messages.
// Implementations can store to files, databases, APIs, etc.
type MessageStore interface {
	// Store saves a message and returns an error if storage fails.
	Store(msg *Message) error
}

// Authenticator handles SMTP authentication.
// Implementations can validate against APIs, databases, LDAP, etc.
type Authenticator interface {
	// Authenticate validates credentials and returns a User if successful.
	// Returns nil user and error if authentication fails.
	Authenticate(username, password string) (*User, error)
}

// SessionObserver receives notifications about session events.
// Multiple observers can be registered to monitor/react to events.
type SessionObserver interface {
	// OnConnect is called when a client connects.
	OnConnect(session *SessionContext)

	// OnAuth is called when authentication succeeds.
	OnAuth(session *SessionContext, user *User)

	// OnMessage is called when a message is received.
	OnMessage(session *SessionContext, msg *Message)

	// OnError is called when an error occurs during the session.
	OnError(session *SessionContext, err error, command string)

	// OnDisconnect is called when the client disconnects.
	OnDisconnect(session *SessionContext, duration string)
}

// SessionContext provides context about the current SMTP session.
type SessionContext struct {
	ID            string                 // Unique session ID
	ClientIP      string                 // Client IP address
	Hostname      string                 // Server hostname
	User          *User                  // Authenticated user (nil if not authenticated)
	Authenticated bool                   // Whether authentication has occurred
	TLSActive     bool                   // Whether TLS is active
	MessagesSent  int                    // Number of messages sent in this session
	Metadata      map[string]interface{} // Custom metadata from extensions (e.g., parsed tokens)
}

// RateLimiter controls connection and message rates.
type RateLimiter interface {
	// AllowConnection checks if a new connection should be allowed.
	AllowConnection(clientIP string) (allowed bool, reason string)

	// AllowMessage checks if a message should be allowed.
	AllowMessage(user *User, clientIP string) (allowed bool, reason string)

	// RecordConnection records that a connection was made.
	RecordConnection(clientIP string)

	// RecordMessage records that a message was sent.
	RecordMessage(user *User, clientIP string)

	// ReleaseConnection records that a connection was closed.
	ReleaseConnection(clientIP string)
}

// Authorizer controls what authenticated users can do.
type Authorizer interface {
	// CanSendFrom checks if user can send from the given address.
	CanSendFrom(user *User, from string) bool

	// CanSendTo checks if user can send to the given address.
	CanSendTo(user *User, to string) bool

	// GetQuota returns the remaining quota for the user.
	// Returns -1 for unlimited quota.
	GetQuota(user *User) int64
}

// ErrorSimulator allows custom error simulation logic.
// The default implementation uses email pattern matching (450@, 550@, etc.)
type ErrorSimulator interface {
	// CheckError examines an address and returns error if pattern matches.
	CheckError(address string, command string) (code string, message string, shouldError bool)
}

// CapabilityParser allows extensions to parse and modify EHLO hostname capability labels.
// This enables extracting custom data (like auth tokens) from the EHLO hostname and
// modifying the capability configuration accordingly.
//
// Example use case: Extract authentication tokens from the hostname label:
//
//	Input:  "tokenabc123xyz-size10000-authplain.example.com"
//	Output: parts=["size10000", "authplain"], metadata={"token": "abc123xyz"}
type CapabilityParser interface {
	// ParseCapabilities receives the original hostname and parsed capability parts.
	// It returns:
	//   - Modified capability parts (with custom parts removed if needed)
	//   - Metadata map with extracted custom data (e.g., tokens, flags)
	//
	// The default implementation simply returns the parts unchanged with empty metadata.
	ParseCapabilities(hostname string, parts []string) ([]string, map[string]interface{})
}

// SMTPExtension allows defining custom SMTP commands and capabilities.
// Extensions can advertise custom capabilities in the EHLO response and handle custom commands.
//
// Example: Implementing a "GOBANANAS" extension with a "BANA" command
type SMTPExtension interface {
	// GetCapability returns the capability string to advertise in EHLO response.
	// Return empty string to not advertise a capability.
	// Example: "GOBANANAS" will appear as "250-GOBANANAS" in EHLO response.
	GetCapability() string

	// GetAllowedStates returns the SMTP states in which this command is allowed.
	// Return nil or empty slice to allow the command in any state.
	// Example: []smtp.State{smtp.StateMail, smtp.StateRcpt} allows command only after EHLO/HELO
	GetAllowedStates(command string) []smtp.State

	// HandleCommand processes a custom SMTP command.
	// Returns true if the command was handled by this extension, false to pass to default handlers.
	// The extension is responsible for sending responses using session.WriteResponse().
	//
	// Example: Handle "BANA" command and respond with "250 OK NA"
	HandleCommand(command string, args []string, session SessionWriter) (handled bool, err error)
}

// SessionWriter provides methods for extensions to interact with the SMTP session.
// This is a subset of Session functionality exposed to extensions for writing responses.
type SessionWriter interface {
	// WriteResponse sends a response to the client
	WriteResponse(response string) error
	// GetMetadata returns session metadata set by other extensions
	GetMetadata() map[string]interface{}
	// SetMetadata stores custom data in session metadata
	SetMetadata(key string, value interface{})
}
