package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"time"

	"badsmtp/auth"
	"badsmtp/logging"
	"badsmtp/smtp"
	"badsmtp/storage"
)

// Precompile commonly used regexes
var (
	sizeRegex = regexp.MustCompile(`size(\d+)`)
)

// parseCapabilityLabel extracts and parses the capability configuration from an EHLO hostname.
// The leftmost label (before the first dot) is extracted and split by dashes into capability parts.
// Example: "size10000-no8bit-authplain.example.com" returns ["size10000", "no8bit", "authplain"]
func parseCapabilityLabel(hostname string) []string {
	// Extract leftmost label before first dot
	label := hostname
	if idx := strings.Index(hostname, "."); idx != -1 {
		label = hostname[:idx]
	}

	// Convert to lowercase for case-insensitive matching
	label = strings.ToLower(label)

	// Split by dashes
	parts := strings.Split(label, "-")

	return parts
}

// hasCapability checks if any of the capability parts match the given pattern
func hasCapability(parts []string, pattern string) bool {
	pattern = strings.ToLower(pattern)
	for _, part := range parts {
		if strings.Contains(part, pattern) {
			return true
		}
	}
	return false
}

// Capabilities tracks which SMTP extensions are enabled for this session
type Capabilities struct {
	Size                bool // SIZE - advertises maximum message size
	Pipelining          bool // PIPELINING - allows batching commands
	EnhancedStatusCodes bool // ENHANCEDSTATUSCODES - uses enhanced status codes
	SMTPUTF8            bool // SMTPUTF8 - supports UTF-8 in addresses
	Chunking            bool // CHUNKING - supports BDAT command
	STARTTLS            bool // STARTTLS - TLS upgrade available
	EightBitMIME        bool // 8BITMIME - 8-bit MIME support
}

// Session represents a single SMTP client connection
type Session struct {
	conn          net.Conn
	connReader    *bufio.Reader
	connTP        *textproto.Reader
	state         smtp.State
	heloName      string
	mailFrom      string
	rcptTo        []string
	authenticated bool
	config        *Config
	mailbox       *storage.Mailbox
	tlsState      *tls.ConnectionState
	hostname      string // The hostname this session is serving
	logger        *logging.SMTPLogger
	startTime     time.Time
	capabilities  Capabilities           // SMTP extensions enabled for this session
	metadata      map[string]interface{} // Custom metadata from extensions (e.g., parsed tokens from EHLO hostname)

	// Pipelining support
	responseQueue  []string // Buffer for pipelined responses
	pipeliningMode bool     // Whether currently processing pipelined commands

	// Per-session advertised SIZE limit (0 means use global MaxMessageSize)
	advertisedSize int

	// BDAT buffering
	bdatBuffer []byte

	// Error simulation results extracted from MAIL FROM and triggered at specific commands
	dataErrorResult     *smtp.ErrorResult // Stores DATA error from MAIL FROM for delayed execution
	rsetErrorResult     *smtp.ErrorResult // Stores RSET error from MAIL FROM for delayed execution
	quitErrorResult     *smtp.ErrorResult // Stores QUIT error from MAIL FROM for delayed execution
	startTLSErrorResult *smtp.ErrorResult // Stores STARTTLS error from MAIL FROM for delayed execution
	noopErrorResult     *smtp.ErrorResult // Stores NOOP error from MAIL FROM for delayed execution
	authErrorResult     *smtp.ErrorResult // Stores AUTH error from MAIL FROM for delayed execution
}

// NewSession creates a new SMTP session with the default hostname
func NewSession(conn net.Conn, config *Config, mailbox *storage.Mailbox) *Session {
	return NewSessionWithHostname(conn, config, mailbox, "")
}

// NewSessionWithHostname creates a new SMTP session with a custom hostname
func NewSessionWithHostname(conn net.Conn, config *Config, mailbox *storage.Mailbox, hostname string) *Session {
	// Create logger - use default config if not available
	loggerConfig := config.LogConfig
	if (loggerConfig == logging.LogConfig{}) {
		loggerConfig = logging.DefaultConfig()
	}
	baseLogger, err := logging.NewLogger(&loggerConfig)
	if err != nil {
		// Fallback to stdout logger if initialization fails
		baseLogger = logging.NewStdoutLogger(&loggerConfig)
	}
	smtpLogger := logging.NewSMTPLogger(baseLogger, conn, hostname)

	session := &Session{
		conn:           conn,
		state:          smtp.StateGreeting,
		config:         config,
		mailbox:        mailbox,
		hostname:       hostname,
		logger:         smtpLogger,
		startTime:      time.Now(),
		advertisedSize: 0,                            // 0 means fallback to global MaxMessageSize
		metadata:       make(map[string]interface{}), // Initialise metadata map for extensions
	}

	return session
}

// Handle processes the SMTP session until completion or error
func (s *Session) Handle() error {
	// Log connection establishment
	tlsEnabled := s.tlsState != nil
	s.logger.LogConnection(s.config.Port, tlsEnabled)

	defer func() {
		duration := time.Since(s.startTime)
		s.logger.LogConnectionClosed(duration)
		if closeErr := s.conn.Close(); closeErr != nil {
			s.logger.Error("Error closing connection", closeErr)
		}
	}()

	if err := s.setupSessionBehaviourAndGreet(); err != nil {
		return err
	}

	// Create a shared buffered reader for the connection and store it on the session
	s.connReader = bufio.NewReader(s.conn)
	s.connTP = textproto.NewReader(s.connReader)
	return s.runCommandLoop()
}

// runCommandLoop contains the main read/process loop extracted from Handle to keep Handle short.
func (s *Session) runCommandLoop() error {
	for {
		// Read a single line (command) from client using textproto to normalise line endings
		line, err := s.connTP.ReadLine()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Security: Check command length limit to prevent DoS attacks
		if len(line) > MaxCommandLength {
			s.logger.Warn("Command length limit exceeded",
				logging.F("command_length", len(line)),
				logging.F("max_length", MaxCommandLength),
				logging.F("client_ip", s.logger.GetClientIP()))
			if err := s.writeResponse("500 Command too long"); err != nil {
				return err
			}
			continue
		}

		// Apply command delay if configured
		if s.config.CommandDelay > 0 {
			s.logger.LogBehaviourTriggered("command_delay", s.config.Port, s.config.CommandDelay)
			time.Sleep(time.Duration(s.config.CommandDelay) * time.Second)
		}

		// Detect whether the client is pipelining: if the server supports PIPELINING
		// and there's more data immediately available from the client, enable
		// pipeliningMode so responses are queued and flushed in order.
		if s.capabilities.Pipelining && !s.pipeliningMode {
			if s.detectPipelining() {
				s.pipeliningMode = true
				s.logger.Debug("Pipelining detected; enabling queued responses", logging.F("client_ip", s.logger.GetClientIP()))
			}
		}

		if err := s.handleCommand(line); err != nil {
			if err == io.EOF {
				return nil
			}
			s.logger.Error("Error handling command", err, logging.F("command", line))
			return err
		}
	}
}

// detectPipelining does a short non-blocking peek to see if the client has written
// more data (multiple commands) without waiting for a response. It sets a short
// read deadline so the peek does not block.
func (s *Session) detectPipelining() bool {
	// Set a tiny read deadline to avoid blocking; log any error but don't treat it as fatal.
	if err := s.conn.SetReadDeadline(time.Now().Add(pipeliningDetectTimeout)); err != nil {
		s.logger.Debug("failed to set read deadline for pipelining detection", logging.F("err", err))
	}
	defer func() {
		if err := s.conn.SetReadDeadline(time.Time{}); err != nil {
			s.logger.Debug("failed to clear read deadline after pipelining detection", logging.F("err", err))
		}
	}()

	// Peek 1 byte: if data is available immediately, Peek will succeed; if not,
	// it will return an error (possibly timeout). We consider pipelining detected
	// only when Peek succeeds.
	if s.connReader == nil {
		return false
	}
	if _, err := s.connReader.Peek(1); err == nil {
		return true
	}
	return false
}

func (s *Session) handleCommand(line string) error {
	cmd, _, err := s.parseAndLogCommand(line)
	if err != nil {
		// Write errors from parseAndLogCommand are fatal to the session
		return err
	}

	// If parseAndLogCommand handled the error/response and returned nil cmd,
	// it's not a fatal condition; continue the loop.
	if cmd == nil {
		return nil
	}

	// Check if this command breaks pipelining
	if s.breaksPipelining(cmd.Name) {
		if err := s.flushResponses(); err != nil {
			return err
		}
		s.pipeliningMode = false
	}

	handlers := s.commandHandlers()
	var cmdErr error
	if h, ok := handlers[cmd.Name]; ok {
		cmdErr = h(cmd)
	} else {
		// Try custom SMTP extensions
		handled, err := s.tryExtensionHandlers(cmd)
		if err != nil {
			cmdErr = err
		} else if !handled {
			cmdErr = s.writeResponse("500 Command not recognised")
		}
	}

	if cmdErr != nil || s.breaksPipelining(cmd.Name) {
		return s.flushAndReturn(cmdErr)
	}
	return cmdErr
}

// tryExtensionHandlers attempts to handle a command using registered SMTP extensions.
// Returns (handled, error) where handled indicates if an extension handled the command.
func (s *Session) tryExtensionHandlers(cmd *smtp.Command) (bool, error) {
	if s.config.SMTPExtensions == nil {
		return false, nil
	}

	for _, ext := range s.config.SMTPExtensions {
		// Check if this extension allows the command in the current state
		if !s.isCommandAllowedByExtension(ext, cmd.Name) {
			continue
		}

		// State is allowed (or no restriction), try to handle the command
		handled, err := ext.HandleCommand(cmd.Name, cmd.Args, s)
		if err != nil {
			return handled, err
		}
		if handled {
			return true, nil
		}
	}

	return false, nil
}

// isCommandAllowedByExtension checks if an extension allows a command in the current state.
func (s *Session) isCommandAllowedByExtension(ext SMTPExtension, command string) bool {
	allowedStates := ext.GetAllowedStates(command)
	if len(allowedStates) == 0 {
		// No restriction, command allowed in any state
		return true
	}

	// Check if current state is in allowed states
	for _, state := range allowedStates {
		if state == s.state {
			return true
		}
	}

	return false
}

// parseAndLogCommand parses, logs, and validates a command line. If it writes a response
// to the client (e.g. for parse/validation errors) it will return a non-nil error.
func (s *Session) parseAndLogCommand(line string) (*smtp.Command, []string, error) {
	cmd, err := smtp.ParseCommand(line)
	if err != nil {
		s.logger.LogCommand("UNKNOWN", []string{line}, s.state.String())
		if werr := s.writeResponse("500 Command not recognised"); werr != nil {
			return nil, nil, werr
		}
		// Continue session; caller should treat nil cmd as non-fatal.
		return nil, nil, nil
	}

	if !cmd.IsValid() {
		// Check if this might be a custom extension command before rejecting
		mightBeCustom := false
		if len(s.config.SMTPExtensions) > 0 {
			// We can't check HandleCommand here (would cause double handling),
			// so we just allow any command to pass if extensions are registered
			// The actual handling will happen in handleCommand
			mightBeCustom = true
		}

		if !mightBeCustom {
			loggedArgs := cmd.Args
			if cmd.Name == smtp.CmdAUTH {
				loggedArgs = auth.RedactAuthArgs(cmd.Args)
			}
			s.logger.LogCommand(cmd.Name, loggedArgs, s.state.String())
			if werr := s.writeResponse("500 Command not recognised"); werr != nil {
				return nil, nil, werr
			}
			return nil, nil, nil
		}
		// Allow command to pass through to extension handlers
	}

	loggedArgs := cmd.Args
	if cmd.Name == smtp.CmdAUTH {
		loggedArgs = auth.RedactAuthArgs(cmd.Args)
	}
	s.logger.LogCommand(cmd.Name, loggedArgs, s.state.String())

	// For unknown/potentially custom commands, skip state and args validation
	// Extensions will handle their own validation
	isCustomCommand := !cmd.IsValid()

	// Check if command is allowed in current state (skip for custom commands)
	if !isCustomCommand && !cmd.IsAllowedInState(s.state) {
		if werr := s.writeResponse(fmt.Sprintf("503 Bad sequence - %s not allowed in %s state", cmd.Name, s.state.String())); werr != nil {
			return nil, nil, werr
		}
		return nil, nil, nil
	}

	// Validate command arguments (skip for custom commands)
	if !isCustomCommand {
		if err := cmd.ValidateArgs(); err != nil {
			if werr := s.writeResponse(err.Error()); werr != nil {
				return nil, nil, werr
			}
			return nil, nil, nil
		}
	}

	return cmd, loggedArgs, nil
}

// commandHandlers returns the dispatch map for SMTP commands.
func (s *Session) commandHandlers() map[string]func(*smtp.Command) error {
	return map[string]func(*smtp.Command) error{
		smtp.CmdHELO:     func(c *smtp.Command) error { return s.handleHelo(c) },
		smtp.CmdEHLO:     func(c *smtp.Command) error { return s.handleHelo(c) },
		smtp.CmdAUTH:     func(c *smtp.Command) error { return s.handleAuth(c) },
		smtp.CmdMAIL:     func(c *smtp.Command) error { return s.handleMail(c) },
		smtp.CmdRCPT:     func(c *smtp.Command) error { return s.handleRcpt(c) },
		smtp.CmdDATA:     func(_ *smtp.Command) error { return s.handleData() },
		smtp.CmdBDAT:     func(c *smtp.Command) error { return s.handleBdat(c) },
		smtp.CmdRSET:     func(_ *smtp.Command) error { return s.handleRset() },
		smtp.CmdNOOP:     func(_ *smtp.Command) error { return s.handleNoop() },
		smtp.CmdSTARTTLS: func(_ *smtp.Command) error { return s.handleStartTLS() },
		smtp.CmdQUIT:     func(_ *smtp.Command) error { return s.handleQuit() },
		smtp.CmdVRFY:     func(c *smtp.Command) error { return s.handleVrfy(c) },
	}
}

// flushAndReturn flushes any pending responses and returns the error
func (s *Session) flushAndReturn(cmdErr error) error {
	if flushErr := s.flushResponses(); flushErr != nil {
		return flushErr
	}
	return cmdErr
}

func (s *Session) handleHelo(cmd *smtp.Command) error {
	if s.state != smtp.StateHelo {
		return s.writeResponse("503 Bad sequence of commands")
	}

	hostname := cmd.Args[0]
	s.heloName = hostname

	// Check for HELO/EHLO error patterns first
	if errorResult := smtp.ExtractHeloError(hostname); errorResult != nil {
		s.logger.LogErrorSimulation(errorResult.Code, hostname, cmd.Name)
		return s.writeResponse(s.formatErrorResult(errorResult))
	}

	s.state = smtp.StateMail

	if cmd.Name == smtp.CmdEHLO {
		return s.handleEhlo(hostname)
	}

	return s.writeResponse("250 badsmtp.test")
}

func (s *Session) handleEhlo(hostname string) error {
	hostname = strings.ToLower(hostname)

	// Parse capability label for rejection patterns
	parts := parseCapabilityLabel(hostname)

	// Check for EHLO rejection patterns
	if hasCapability(parts, "reject") || hasCapability(parts, "noehl") {
		return s.writeResponse("502 Command not implemented")
	}

	response := s.buildEhloResponse(hostname)
	return s.writeResponse(strings.Join(response, "\r\n"))
}

// buildEhloResponse constructs the EHLO response lines for a given hostname.
func (s *Session) buildEhloResponse(hostname string) []string {
	// Parse capability label from hostname (leftmost label before first dot, split by dashes)
	parts := parseCapabilityLabel(hostname)

	// Call extension hook to allow custom parsing and metadata extraction
	if s.config.CapabilityParser != nil {
		modifiedParts, metadata := s.config.CapabilityParser.ParseCapabilities(hostname, parts)
		parts = modifiedParts
		// Store extracted metadata in session for access by other extensions
		for k, v := range metadata {
			s.metadata[k] = v
		}
	}

	response := []string{fmt.Sprintf("%d-badsmtp.test", smtp.Code250)}

	// Build standard capabilities
	s.addStandardCapabilities(&response, parts)

	// Add custom SMTP extension capabilities
	s.addExtensionCapabilities(&response)

	// Final line without dash
	response = append(response, fmt.Sprintf("%d OK", smtp.Code250))
	return response
}

// addStandardCapabilities adds all standard SMTP capabilities to the EHLO response.
func (s *Session) addStandardCapabilities(response *[]string, parts []string) {
	// AUTH - enabled by default
	if !hasCapability(parts, "noauth") {
		authMechanisms := s.getAuthMechanisms(parts)
		if authMechanisms != "" {
			*response = append(*response, fmt.Sprintf("%d-AUTH %s", smtp.Code250, authMechanisms))
		}
	}

	// 8BITMIME - enabled by default
	s.capabilities.EightBitMIME = !hasCapability(parts, "no8bit")
	if s.capabilities.EightBitMIME {
		*response = append(*response, fmt.Sprintf("%d-8BITMIME", smtp.Code250))
	}

	// SIZE - enabled by default, but allow hostname to set a custom value using `size<digits>`
	s.capabilities.Size = !hasCapability(parts, "nosize")
	if s.capabilities.Size {
		sz := MaxMessageSize
		if v, ok := s.parseAndClampSize(parts); ok {
			s.advertisedSize = v
			sz = v
		}
		*response = append(*response, fmt.Sprintf("%d-SIZE %d", smtp.Code250, sz))
	}

	// PIPELINING - enabled by default
	s.capabilities.Pipelining = !hasCapability(parts, "nopipelining")
	if s.capabilities.Pipelining {
		*response = append(*response, fmt.Sprintf("%d-PIPELINING", smtp.Code250))
	}

	// STARTTLS - enabled by default if TLS is available
	s.capabilities.STARTTLS = !hasCapability(parts, "nostarttls") && s.config.HasTLS()
	if s.capabilities.STARTTLS {
		*response = append(*response, fmt.Sprintf("%d-STARTTLS", smtp.Code250))
	}

	// CHUNKING - enabled by default
	s.capabilities.Chunking = !hasCapability(parts, "nochunking")
	if s.capabilities.Chunking {
		*response = append(*response, fmt.Sprintf("%d-CHUNKING", smtp.Code250))
	}

	// SMTPUTF8 - enabled by default
	s.capabilities.SMTPUTF8 = !hasCapability(parts, "nosmtputf8")
	if s.capabilities.SMTPUTF8 {
		*response = append(*response, fmt.Sprintf("%d-SMTPUTF8", smtp.Code250))
	}

	// ENHANCEDSTATUSCODES - enabled by default
	s.capabilities.EnhancedStatusCodes = !hasCapability(parts, "noenhancedstatuscodes")
	if s.capabilities.EnhancedStatusCodes {
		*response = append(*response, fmt.Sprintf("%d-ENHANCEDSTATUSCODES", smtp.Code250))
	}
}

// addExtensionCapabilities adds custom SMTP extension capabilities to the EHLO response.
func (s *Session) addExtensionCapabilities(response *[]string) {
	if s.config.SMTPExtensions == nil {
		return
	}

	for _, ext := range s.config.SMTPExtensions {
		if capability := ext.GetCapability(); capability != "" {
			*response = append(*response, fmt.Sprintf("%d-%s", smtp.Code250, capability))
		}
	}
}

// parseAndClampSize looks for 'size<digits>' in the capability parts and returns the clamped value
// and true if a value was successfully parsed. If not present / parse error, returns (0,false).
func (s *Session) parseAndClampSize(parts []string) (int, bool) {
	// Check each part for size pattern
	for _, part := range parts {
		m := sizeRegex.FindStringSubmatch(part)
		if len(m) == 2 {
			v, err := strconv.Atoi(m[1])
			if err != nil || v <= 0 {
				continue
			}
			if v < advertisedSizeMin {
				s.logger.Debug("advertised SIZE below minimum; clamping to min", logging.F("requested", v), logging.F("min", advertisedSizeMin))
				v = advertisedSizeMin
			} else if v > advertisedSizeMax {
				s.logger.Debug("advertised SIZE above maximum; clamping to max", logging.F("requested", v), logging.F("max", advertisedSizeMax))
				v = advertisedSizeMax
			}
			return v, true
		}
	}
	return 0, false
}

func (s *Session) handleAuth(cmd *smtp.Command) error {
	if s.state != smtp.StateMail && s.state != smtp.StateAuth {
		return s.writeResponse("503 Bad sequence of commands")
	}

	// Check for AUTH error configured from MAIL FROM
	if s.authErrorResult != nil {
		s.logger.LogErrorSimulation(s.authErrorResult.Code, s.mailFrom, "AUTH")
		return s.writeResponse(s.formatErrorResult(s.authErrorResult))
	}

	mech := cmd.Args[0]
	handler := auth.NewHandler(mech)
	if handler == nil {
		return s.writeResponse("504 Authentication mechanism not supported")
	}

	username, err := handler.Authenticate(s.conn, append([]string{cmd.Name}, cmd.Args...))
	if err != nil {
		s.logger.LogAuthentication(mech, username, false)
		return s.writeResponse("535 Authentication failed")
	}

	// Use the extension Authenticator interface for validation
	user, err := s.config.Authenticator.Authenticate(username, "")
	if err != nil {
		s.logger.LogAuthentication(mech, username, false)
		return s.writeResponse("535 Authentication failed")
	}

	// Check if user is active
	if !user.Active {
		s.logger.LogAuthentication(mech, username, false)
		return s.writeResponse("535 Authentication failed: account inactive")
	}

	s.authenticated = true
	s.state = smtp.StateMail
	s.logger.LogAuthentication(mech, username, true)
	return s.writeResponse("235 Authentication successful")
}

func (s *Session) handleMail(cmd *smtp.Command) error {
	if s.state != smtp.StateMail {
		return s.writeResponse("503 Bad sequence of commands")
	}

	// Extract raw mailbox from argument
	raw := smtp.ExtractMailboxFromArg(cmd.Args[0])
	if raw == "" {
		return s.writeResponse("501 Syntax error in parameters")
	}

	// Validate mailbox according to session capabilities (SMTPUTF8)
	if !smtp.IsValidMailbox(raw, s.capabilities.SMTPUTF8) {
		return s.writeResponse("501 Syntax error in parameters")
	}

	// Normalise for internal storage (preserve local-part case, lowercase domain)
	fromAddr := smtp.NormaliseMailbox(raw)

	// Check for MAIL FROM specific error patterns (mail452@example.com, mail550_571@example.com)
	if errorResult := smtp.ExtractMailFromError(fromAddr); errorResult != nil {
		s.logger.LogErrorSimulation(errorResult.Code, fromAddr, "MAIL")
		return s.writeResponse(s.formatErrorResult(errorResult))
	}

	// Extract ALL error patterns from MAIL FROM for delayed execution at their respective commands
	// This allows one MAIL FROM address to configure errors for multiple commands
	s.dataErrorResult = smtp.ExtractDataError(fromAddr)
	s.rsetErrorResult = smtp.ExtractRsetError(fromAddr)
	s.quitErrorResult = smtp.ExtractQuitError(fromAddr)
	s.startTLSErrorResult = smtp.ExtractStartTLSError(fromAddr)
	s.noopErrorResult = smtp.ExtractNoopError(fromAddr)
	s.authErrorResult = smtp.ExtractAuthError(fromAddr)

	s.mailFrom = fromAddr
	s.logger.LogStateTransition(s.state.String(), smtp.StateRcpt.String(), "MAIL")
	s.state = smtp.StateRcpt
	return s.writeResponse("250 OK")
}

func (s *Session) handleRcpt(cmd *smtp.Command) error {
	if s.state != smtp.StateRcpt {
		return s.writeResponse("503 Bad sequence of commands")
	}

	// Extract raw mailbox from argument
	raw := smtp.ExtractMailboxFromArg(cmd.Args[0])
	if raw == "" {
		return s.writeResponse("501 Syntax error in parameters")
	}

	// Validate mailbox according to session capabilities (SMTPUTF8)
	if !smtp.IsValidMailbox(raw, s.capabilities.SMTPUTF8) {
		return s.writeResponse("501 Syntax error in parameters")
	}

	// Normalise for internal use
	toAddr := smtp.NormaliseMailbox(raw)

	// Check for RCPT TO specific error patterns first (rcpt452@example.com, rcpt550_571@example.com)
	if errorResult := smtp.ExtractRcptToError(toAddr); errorResult != nil {
		s.logger.LogErrorSimulation(errorResult.Code, toAddr, "RCPT")
		return s.writeResponse(s.formatErrorResult(errorResult))
	}

	s.rcptTo = append(s.rcptTo, toAddr)
	// Stay in StateRcpt to allow multiple recipients
	return s.writeResponse("250 OK")
}

// handleVrfy implements the VRFY command; it does not change session state and may be issued any time.
func (s *Session) handleVrfy(cmd *smtp.Command) error {
	// Combine arguments into a single mailbox specification
	target := strings.Join(cmd.Args, " ")
	// Use ExtractMailbox which better handles UTF-8 and display-name forms
	raw := smtp.ExtractMailbox(target)
	if raw == "" || !strings.Contains(raw, "@") {
		return s.writeResponse("501 Syntax error in parameters")
	}

	// Log extracted raw address and SMTPUTF8 flag via logger (no stdout prints)
	s.logger.Debug("VRFY extracted mailbox", logging.F("raw", raw), logging.F("allow_smtputf8", s.capabilities.SMTPUTF8))

	// Validate mailbox according to session capabilities
	if !smtp.IsValidMailbox(raw, s.capabilities.SMTPUTF8) {
		// If SMTPUTF8 is enabled, be lenient: accept if there's an '@' and the domain validates.
		if s.capabilities.SMTPUTF8 {
			if parts := strings.SplitN(raw, "@", 2); len(parts) == 2 {
				domain := parts[1]
				if smtp.ValidateDomain(domain) {
					// accept raw as a mailbox
					addr := smtp.NormaliseMailbox(raw)
					local := strings.ToLower(strings.SplitN(addr, "@", 2)[0])
					switch {
					case strings.HasPrefix(local, "exists"):
						return s.writeResponse(fmt.Sprintf("250 %s User exists", addr))
					case strings.HasPrefix(local, "unknown"):
						return s.writeResponse("551 User not local; please try forward path")
					case strings.HasPrefix(local, "ambiguous"):
						return s.writeResponse("553 User ambiguous")
					default:
						return s.writeResponse("550 Requested action not taken: mailbox unavailable")
					}
				}
			}
		}
		return s.writeResponse("501 Syntax error in parameters")
	}

	addr := smtp.NormaliseMailbox(raw)
	local := strings.ToLower(strings.SplitN(addr, "@", 2)[0])

	switch {
	case strings.HasPrefix(local, "exists"):
		// User exists
		return s.writeResponse(fmt.Sprintf("250 %s User exists", addr))
	case strings.HasPrefix(local, "unknown"):
		// User not local
		return s.writeResponse("551 User not local; please try forward path")
	case strings.HasPrefix(local, "ambiguous"):
		// Mailbox name not allowed / ambiguous
		return s.writeResponse("553 Requested action not taken: mailbox name not allowed")
	default:
		// Default: mailbox unavailable
		return s.writeResponse("550 Requested action not taken: mailbox unavailable")
	}
}

func (s *Session) handleData() error {
	if s.state != smtp.StateRcpt {
		return s.writeResponse("503 Bad sequence of commands")
	}

	// Check for DATA error set up from MAIL FROM command first
	if s.dataErrorResult != nil {
		s.logger.LogErrorSimulation(s.dataErrorResult.Code, s.mailFrom, "DATA")
		return s.writeResponse(s.formatErrorResult(s.dataErrorResult))
	}

	// Log message start
	s.logger.LogMessageStart(s.mailFrom, s.rcptTo)

	// Transition to StateData
	s.logger.LogStateTransition(s.state.String(), smtp.StateData.String(), "DATA")
	s.state = smtp.StateData

	if err := s.writeResponse("354 End data with <CR><LF>.<CR><LF>"); err != nil {
		return err
	}

	// Read message content
	messageContent, err := s.readMessageContent()
	if err != nil {
		return err
	}

	// Store the message using the injected handler
	if err := s.storeMessage(messageContent); err != nil {
		return s.handleStorageError(err)
	}

	// Reset session state for next message
	s.resetSessionState()

	return s.writeResponse("250 OK Message accepted for delivery")
}

// readMessageContent reads the message content from the connection with size limits
func (s *Session) readMessageContent() (string, error) {
	// Use textproto.Reader.ReadDotBytes which correctly handles the SMTP dot-stuffing and termination
	if s.connReader == nil {
		s.connReader = bufio.NewReader(s.conn)
	}
	tp := textproto.NewReader(s.connReader)

	// ReadDotBytes returns the message bytes up to but not including the terminating dot line
	data, err := tp.ReadDotBytes()
	if err != nil {
		s.logger.Error("Error reading message content", err,
			logging.F("client_ip", s.logger.GetClientIP()))
		return "", err
	}

	totalSize := len(data)
	maxSize := s.getMaxMessageSize()
	if totalSize > maxSize {
		s.logger.Warn("Message size limit exceeded",
			logging.F("current_size", totalSize),
			logging.F("max_size", maxSize),
			logging.F("client_ip", s.logger.GetClientIP()))
		return "", fmt.Errorf("message size exceeds limit of %d bytes", maxSize)
	}

	s.logger.Debug("Message content read successfully",
		logging.F("message_size", totalSize),
		logging.F("client_ip", s.logger.GetClientIP()))

	// Ensure CRLF line endings and return as string
	return string(data), nil
}

// storeMessage stores the message using the configured message store
func (s *Session) storeMessage(content string) error {
	startTime := time.Now()

	// Parse headers and body using the standard library mail parser
	headers := make(map[string]string)
	var bodyBytes []byte
	if mr, err := mail.ReadMessage(strings.NewReader(content)); err == nil {
		for k, vals := range mr.Header {
			headers[k] = strings.Join(vals, ", ")
		}
		if b, err := io.ReadAll(mr.Body); err == nil {
			bodyBytes = b
		}
	} else {
		// Fallback: leave headers empty and preserve original content
		s.logger.Debug("mail.ReadMessage failed, falling back to simple parsing", logging.F("err", err))
	}

	// Create Message struct for the extension interface
	msg := &Message{
		From:      s.mailFrom,
		To:        s.rcptTo,
		Content:   content,
		Headers:   headers,
		Size:      len(content),
		ClientIP:  s.logger.GetClientIP(),
		Hostname:  s.hostname,
		TLSUsed:   s.tlsState != nil,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	// If we successfully parsed bodyBytes, update Size to reflect body length instead
	if len(bodyBytes) > 0 {
		msg.Size = len(bodyBytes)
	}

	// Store using the extension interface
	err := s.config.MessageStore.Store(msg)
	duration := time.Since(startTime)

	storageType := "local"

	if err != nil {
		s.logger.LogMessageStorageError(s.mailFrom, s.rcptTo, msg.Size, storageType, err)
		return err
	}

	s.logger.LogMessageStored(s.mailFrom, s.rcptTo, msg.Size, storageType, duration)
	return nil
}

// handleStorageError converts storage errors to appropriate SMTP responses
func (s *Session) handleStorageError(err error) error {
	if strings.Contains(err.Error(), "not active") {
		return s.writeResponse("550 Requested action not taken: mailbox unavailable")
	}
	if strings.Contains(err.Error(), "quota") {
		return s.writeResponse("452 Requested action not taken: insufficient system storage")
	}
	return s.writeResponse("450 Requested action not taken: mailbox temporarily unavailable")
}

// resetSessionState resets the session state for the next message
func (s *Session) resetSessionState() {
	s.logger.LogStateTransition(s.state.String(), smtp.StateMail.String(), "reset")
	s.state = smtp.StateMail
	s.rcptTo = nil
	s.mailFrom = ""

	// Reset all error simulation results for next message
	s.dataErrorResult = nil
	s.rsetErrorResult = nil
	s.quitErrorResult = nil
	s.startTLSErrorResult = nil
	s.noopErrorResult = nil
	s.authErrorResult = nil
}

func (s *Session) handleRset() error {
	// Check for RSET error configured from MAIL FROM
	if s.rsetErrorResult != nil {
		s.logger.LogErrorSimulation(s.rsetErrorResult.Code, s.mailFrom, "RSET")
		// Don't reset state on error - let the client try again
		return s.writeResponse(s.formatErrorResult(s.rsetErrorResult))
	}

	s.logger.LogStateTransition(s.state.String(), smtp.StateMail.String(), "RSET")
	s.state = smtp.StateMail
	s.mailFrom = ""
	s.rcptTo = nil
	return s.writeResponse("250 OK")
}

func (s *Session) handleNoop() error {
	// Check for NOOP error configured from MAIL FROM
	if s.noopErrorResult != nil {
		s.logger.LogErrorSimulation(s.noopErrorResult.Code, s.mailFrom, "NOOP")
		return s.writeResponse(s.formatErrorResult(s.noopErrorResult))
	}

	return s.writeResponse("250 OK")
}

// setupSessionBehaviourAndGreet handles initial session behaviours (drops/delays) and sends the greeting.
func (s *Session) setupSessionBehaviourAndGreet() error {
	if s.config.DropImmediate {
		s.logger.LogBehaviourTriggered("immediate_drop", s.config.Port, 0)
		return nil
	}

	if s.config.GreetingDelay > 0 {
		s.logger.LogBehaviourTriggered("greeting_delay", s.config.Port, s.config.GreetingDelay)
		time.Sleep(time.Duration(s.config.GreetingDelay) * time.Second)
	}

	if s.config.DropDelay > 0 {
		s.logger.LogBehaviourTriggered("drop_delay", s.config.Port, s.config.DropDelay)
		time.Sleep(time.Duration(s.config.DropDelay) * time.Second)
		return nil
	}

	// Use configured hostname if present, otherwise fall back to the session hostname or default identity.
	identity := "badsmtp.test"
	if s.hostname != "" {
		identity = s.hostname
	} else if s.config != nil && s.config.TLSHostname != "" {
		identity = s.config.TLSHostname
	}

	if err := s.writeResponse(fmt.Sprintf("%d %s ESMTP %s", smtp.Code220, identity, ServerGreeting)); err != nil {
		return err
	}
	s.state = smtp.StateHelo
	return nil
}

// obtainTLSCertificate loads TLS certs from files or generates a self-signed certificate.
func (s *Session) obtainTLSCertificate(hostname string) (tls.Certificate, error) {
	var cert tls.Certificate
	var err error
	if s.config.HasTLS() {
		cert, err = tls.LoadX509KeyPair(s.config.TLSCertFile, s.config.TLSKeyFile)
		if err != nil {
			s.logger.Warn("Failed to load TLS certificate from files, generating self-signed",
				logging.F("hostname", hostname),
				logging.F("cert_file", s.config.TLSCertFile),
				logging.F("key_file", s.config.TLSKeyFile))
			cert, err = s.config.GenerateSelfSignedCert(hostname)
			if err != nil {
				return tls.Certificate{}, fmt.Errorf("failed to generate self-signed certificate: %v", err)
			}
		}
	} else {
		cert, err = s.config.GenerateSelfSignedCert(hostname)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("failed to generate self-signed certificate: %v", err)
		}
	}
	return cert, nil
}

// upgradeToTLS performs the TLS handshake, updates the session connection and logs the result.
func (s *Session) upgradeToTLS(cert *tls.Certificate) error {
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
	}

	tlsConn := tls.Server(s.conn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		s.logger.LogTLSHandshake(false, "", "", err)
		return fmt.Errorf("TLS handshake failed: %v", err)
	}

	s.conn = tlsConn
	tlsState := tlsConn.ConnectionState()
	s.tlsState = &tlsState
	s.logTLSInfo(&tlsState)
	return nil
}

// logTLSInfo extracts friendly TLS version and cipher names and logs the handshake success.
func (s *Session) logTLSInfo(tlsState *tls.ConnectionState) {
	tlsVersion := "unknown"
	cipher := "unknown"
	if tlsState != nil && tlsState.Version != 0 {
		switch tlsState.Version {
		case tls.VersionTLS12:
			tlsVersion = "TLS 1.2"
		case tls.VersionTLS13:
			tlsVersion = "TLS 1.3"
		}
	}
	if tlsState != nil && tlsState.CipherSuite != 0 {
		cipher = tls.CipherSuiteName(tlsState.CipherSuite)
	}
	s.logger.LogTLSHandshake(true, tlsVersion, cipher, nil)
}

func (s *Session) handleStartTLS() error {
	// Check for STARTTLS error configured from MAIL FROM
	if s.startTLSErrorResult != nil {
		s.logger.LogErrorSimulation(s.startTLSErrorResult.Code, s.mailFrom, "STARTTLS")
		return s.writeResponse(s.formatErrorResult(s.startTLSErrorResult))
	}

	if s.tlsState != nil {
		return s.writeResponse("554 TLS already started")
	}

	if err := s.writeResponse("220 Ready to start TLS"); err != nil {
		return err
	}

	// Use the hostname from EHLO command for certificate generation
	hostname := s.heloName
	if hostname == "" {
		hostname = s.config.GetTLSHostname()
	}

	// Generate or load certificate for hostname
	cert, err := s.obtainTLSCertificate(hostname)
	if err != nil {
		s.logger.LogTLSHandshake(false, "", "", err)
		return err
	}

	// Perform TLS upgrade
	if err := s.upgradeToTLS(&cert); err != nil {
		return err
	}

	s.logger.LogStateTransition(s.state.String(), smtp.StateHelo.String(), "STARTTLS")
	s.state = smtp.StateHelo // Reset to HELO state after TLS
	return nil
}

func (s *Session) handleQuit() error {
	// Check for QUIT error configured from MAIL FROM
	if s.quitErrorResult != nil {
		s.logger.LogErrorSimulation(s.quitErrorResult.Code, s.mailFrom, "QUIT")
		if err := s.writeResponse(s.formatErrorResult(s.quitErrorResult)); err != nil {
			return err
		}
		// Still close the connection after error
		s.state = smtp.StateQuit
		return io.EOF
	}

	if err := s.writeResponse("221 Bye"); err != nil {
		return err
	}
	s.state = smtp.StateQuit
	return io.EOF
}

// isResponse421 reports whether the response (possibly multiline) indicates a 421.
// It checks the first non-empty line for a numeric 421 prefix, or for the canonical message.
func (s *Session) isResponse421(response string) bool {
	if response == "" {
		return false
	}
	// Look at first line
	first := response
	if idx := strings.Index(response, "\r\n"); idx != -1 {
		first = response[:idx]
	}
	first = strings.TrimLeft(first, " \t")
	if len(first) >= 3 {
		if code, err := strconv.Atoi(first[:3]); err == nil {
			return code == smtp.Code421
		}
	}
	// Fallback: compare canonical message (case-insensitive)
	canon := strings.ToLower(strings.TrimSpace(smtp.GetErrorMessage(smtp.Code421)))
	if strings.EqualFold(strings.TrimSpace(first), canon) || strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(first, "421 ")), canon) {
		return true
	}
	return false
}

func (s *Session) writeResponse(response string) error {
	is421 := s.isResponse421(response)

	// If pipelining mode is active, queue the response instead of sending immediately
	if s.pipeliningMode && !is421 {
		s.responseQueue = append(s.responseQueue, response)
		s.logger.LogResponse(response, " (queued)")
		return nil
	}

	// If pipelining mode is active and we have a 421, flush queued responses,
	// send the 421, close the connection and signal EOF to stop the session.
	if s.pipeliningMode && is421 {
		if err := s.flushResponses(); err != nil {
			return err
		}
		// Ensure response lines are properly terminated
		if _, err := s.conn.Write([]byte(response + "\r\n")); err != nil {
			s.logger.LogResponse(response, "")
			return err
		}
		s.logger.LogResponse(response, "")
		if err := s.conn.Close(); err != nil {
			s.logger.Debug("Error closing connection after sending 421 (pipelined)", logging.F("err", err))
		}
		return io.EOF
	}

	// Normal mode: send response immediately
	if _, err := s.conn.Write([]byte(response + "\r\n")); err != nil {
		s.logger.LogResponse(response, "")
		return err
	}
	s.logger.LogResponse(response, "")

	// If response was 421, close and signal EOF
	if is421 {
		if err := s.conn.Close(); err != nil {
			s.logger.Debug("Error closing connection after sending 421", logging.F("err", err))
		}
		return io.EOF
	}

	return nil
}

// CloseWith421 attempts to notify the client with a 421 response and close the connection.
// It uses the provided context to bound write/close operations.
func (s *Session) CloseWith421(ctx context.Context, reason string) error {
	// Prepare canonical 421 message
	msg := smtp.GetErrorMessage(smtp.Code421)
	if reason != "" {
		msg = reason
	}
	resp := fmt.Sprintf("%d %s", smtp.Code421, msg)

	// Flush any queued responses first
	if err := s.flushResponses(); err != nil {
		s.logger.Debug("flushResponses failed during shutdown", logging.F("err", err))
		// continue trying to notify client even if flushing fails
	}

	// Determine write deadline from context (max 5s)
	var dl time.Time
	if d, ok := ctx.Deadline(); ok {
		// use context deadline but not more than maxWriteDeadline into future
		maxDl := time.Now().Add(maxWriteDeadline)
		if d.After(maxDl) {
			dl = maxDl
		} else {
			dl = d
		}
	} else {
		dl = time.Now().Add(2 * time.Second)
	}

	// Set write deadline to avoid blocking indefinitely
	if err := s.conn.SetWriteDeadline(dl); err != nil {
		s.logger.Debug("failed to set write deadline for CloseWith421", logging.F("err", err))
	}
	// Attempt to write the 421 response directly
	if _, err := s.conn.Write([]byte(resp + "\r\n")); err != nil {
		s.logger.Debug("Failed to write 421 on shutdown", logging.F("err", err))
		// Attempt to close regardless
		if cerr := s.conn.Close(); cerr != nil {
			s.logger.Debug("Failed to close connection after failed write", logging.F("err", cerr))
		}
		return err
	}

	// Close connection
	if err := s.conn.Close(); err != nil {
		s.logger.Debug("Failed to close connection after 421", logging.F("err", err))
		return err
	}

	s.logger.LogResponse(resp, " (shutdown)")
	return nil
}

// breaksPipelining returns true if a command requires breaking pipelining mode.
// These commands require immediate processing and cannot be queued.
func (s *Session) breaksPipelining(cmdName string) bool {
	switch cmdName {
	case smtp.CmdDATA:
		// DATA requires reading message content from stream
		return true
	case smtp.CmdBDAT:
		// BDAT requires reading chunk data from stream
		return true
	case smtp.CmdAUTH:
		// AUTH may require multi-step exchanges
		return true
	case smtp.CmdSTARTTLS:
		// STARTTLS changes the connection state
		return true
	case smtp.CmdQUIT:
		// QUIT terminates the connection
		return true
	default:
		return false
	}
}

// flushResponses sends all queued responses at once.
func (s *Session) flushResponses() error {
	if len(s.responseQueue) == 0 {
		return nil
	}

	// Send all responses together
	for _, response := range s.responseQueue {
		if _, err := s.conn.Write([]byte(response + "\r\n")); err != nil {
			return err
		}
	}

	// Clear the queue
	s.responseQueue = nil
	return nil
}

// getAuthMechanisms returns auth mechanisms string based on capability parts.
// If multiple auth options are provided, the last one takes precedence.
func (s *Session) getAuthMechanisms(parts []string) string {
	// Check all parts and use the last matching auth mechanism
	lastAuth := ""
	for _, part := range parts {
		part = strings.ToLower(part)
		if strings.Contains(part, "authplain") {
			lastAuth = "PLAIN"
		} else if strings.Contains(part, "authlogin") {
			lastAuth = "LOGIN"
		} else if strings.Contains(part, "authcram") {
			lastAuth = "CRAM-MD5 CRAM-SHA256"
		} else if strings.Contains(part, "authoauth") {
			lastAuth = "XOAUTH2"
		}
	}

	// If an auth mechanism was specified, return it
	if lastAuth != "" {
		return lastAuth
	}

	// Default: all mechanisms
	return "PLAIN LOGIN CRAM-MD5 CRAM-SHA256 XOAUTH2"
}

// getMaxMessageSize returns the effective maximum message size for the session.
// If a per-session advertisedSize was set via EHLO hostname, use that; otherwise use global MaxMessageSize.
func (s *Session) getMaxMessageSize() int {
	if s.advertisedSize > 0 {
		return s.advertisedSize
	}
	return MaxMessageSize
}

// SessionWriter interface implementation
// These methods allow extensions to interact with the session

// WriteResponse sends a response to the client (implements SessionWriter)
func (s *Session) WriteResponse(response string) error {
	return s.writeResponse(response)
}

// GetMetadata returns session metadata set by extensions (implements SessionWriter)
func (s *Session) GetMetadata() map[string]interface{} {
	return s.metadata
}

// SetMetadata stores custom data in session metadata (implements SessionWriter)
func (s *Session) SetMetadata(key string, value interface{}) {
	if s.metadata == nil {
		s.metadata = make(map[string]interface{})
	}
	s.metadata[key] = value
}

const (
	// pipeliningDetectTimeout is the deadline used when peeking the connection to
	// detect whether a client has sent additional commands without waiting for
	// responses (i.e. is pipelining). A small timeout avoids blocking I/O.
	pipeliningDetectTimeout = 10 * time.Millisecond

	// Bounds for per-session advertised SIZE from EHLO hostname.
	advertisedSizeMin = 1000     // minimum allowed advertised SIZE (bytes)
	advertisedSizeMax = 10000000 // maximum allowed advertised SIZE (bytes)

	// maxWriteDeadline is the maximum write deadline used when trying to notify clients
	// during shutdown; this bounds per-session write waits.
	maxWriteDeadline = 5 * time.Second
)

// formatErrorResult returns the proper SMTP response string for an ErrorResult
// depending on whether enhanced status codes are enabled for this session.
func (s *Session) formatErrorResult(err *smtp.ErrorResult) string {
	if err == nil {
		return ""
	}
	// Always return a 3-digit response code first. If enhanced status codes
	// are enabled for this session and an enhanced code is available, include it.
	if s.capabilities.EnhancedStatusCodes && err.Enhanced != "" {
		return fmt.Sprintf("%d %s %s", err.Code, err.Enhanced, smtp.GetErrorMessage(err.Code))
	}
	// Default: numeric code followed by standard message
	return fmt.Sprintf("%d %s", err.Code, smtp.GetErrorMessage(err.Code))
}

// handleBdat implements BDAT chunk handling for CHUNKING extension support.
// BDAT <n> [LAST]
func (s *Session) handleBdat(cmd *smtp.Command) error {
	if !(s.state == smtp.StateRcpt || s.state == smtp.StateBdat) {
		return s.writeResponse("503 Bad sequence of commands")
	}

	// Parse size
	n, err := strconv.Atoi(cmd.Args[0])
	if err != nil || n < 0 {
		return s.writeResponse("501 Syntax error in parameters")
	}

	last := false
	if len(cmd.Args) > 1 && strings.EqualFold(cmd.Args[1], "LAST") {
		last = true
	}

	// Check for DATA error configured from MAIL FROM (only relevant on final chunk)
	if s.dataErrorResult != nil && last {
		s.logger.LogErrorSimulation(s.dataErrorResult.Code, s.mailFrom, "BDAT")
		return s.writeResponse(s.formatErrorResult(s.dataErrorResult))
	}

	// Enforce maximum message size
	totalSoFar := len(s.bdatBuffer)
	maxSize := s.getMaxMessageSize()
	if totalSoFar+n > maxSize {
		s.logger.Warn("BDAT would exceed message size limit",
			logging.F("current_size", totalSoFar),
			logging.F("incoming_chunk", n),
			logging.F("max_size", maxSize),
			logging.F("client_ip", s.logger.GetClientIP()))
		return s.writeResponse(fmt.Sprintf("552 Message size exceeds fixed maximum of %d bytes", maxSize))
	}

	// Read chunk
	chunk, err := s.readBDATChunk(n)
	if err != nil {
		s.logger.Error("Error reading BDAT chunk", err, logging.F("client_ip", s.logger.GetClientIP()))
		return err
	}

	// Append to buffer
	s.bdatBuffer = append(s.bdatBuffer, chunk...)

	// If LAST, finalise message: store the message and reset state
	if last {
		content := string(s.bdatBuffer)
		if err := s.storeMessage(content); err != nil {
			return s.handleStorageError(err)
		}
		s.bdatBuffer = nil
		s.resetSessionState()
		return s.writeResponse("250 OK Message accepted for delivery")
	}

	// If not LAST, remain in Bdat state and acknowledge
	s.state = smtp.StateBdat
	return s.writeResponse("250 OK")
}

// readBDATChunk reads exactly n bytes from the connection reader and consumes an optional following CRLF.
func (s *Session) readBDATChunk(n int) ([]byte, error) {
	if s.connReader == nil {
		s.connReader = bufio.NewReader(s.conn)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(s.connReader, buf); err != nil {
		return nil, err
	}

	// If BDAT chunk is terminated by CRLF per RFC, attempt to peek and discard CRLF if present
	peek, err := s.connReader.Peek(2)
	if err == nil {
		if len(peek) == 2 && peek[0] == '\r' && peek[1] == '\n' {
			// consume CRLF and check error
			if _, derr := s.connReader.Discard(2); derr != nil {
				s.logger.Debug("failed to discard CRLF after BDAT chunk", logging.F("err", derr))
			}
		}
	}
	return buf, nil
}
