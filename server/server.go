// Package server provides the SMTP server implementation for BadSMTP.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"badsmtp/logging"
	"badsmtp/storage"
)

const (
	// PortRangeSize is the number of ports in each special behaviour range
	PortRangeSize = DelayCount
	// PortRangeEnd is the last offset in a port range
	PortRangeEnd = DelayCount - 1
	// MinTLSVersion is the minimum TLS version supported by the server
	MinTLSVersion = tls.VersionTLS12

	// DefaultShutdownTimeout is the graceful shutdown timeout used by the server
	DefaultShutdownTimeout = 10 * time.Second
)

// Server represents an SMTP test server instance
type Server struct {
	config  *Config
	mailbox *storage.Mailbox
	logger  logging.Logger

	// listeners we opened so they can be closed on shutdown
	listeners   []net.Listener
	listenersMu sync.Mutex

	// active sessions tracking
	sessions   map[*Session]struct{}
	sessionsMu sync.Mutex
	sessionsWG sync.WaitGroup

	// shutdown flag
	shuttingDown int32
}

// NewServer creates a new SMTP server with the specified configuration.
func NewServer(config *Config) (*Server, error) {
	// Initialise logger first
	logger, err := logging.NewLogger(&config.LogConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise logger: %w", err)
	}

	// Validate port configuration for conflicts
	if err := validatePortConfiguration(config); err != nil {
		return nil, fmt.Errorf("port configuration error: %w", err)
	}

	// Analyse port behaviour based on configuration
	config.AnalysePortBehaviour()

	// Remap Unix-style /tmp paths to OS temp dir on Windows for consistency with tests
	if runtime.GOOS == "windows" && config.MailboxDir != "" {
		s := filepath.ToSlash(config.MailboxDir)
		if strings.HasPrefix(s, "/tmp") || strings.HasPrefix(s, "/var/tmp") {
			tail := strings.TrimPrefix(s, "/tmp")
			tail = strings.TrimPrefix(tail, "/")
			if tail == "" {
				config.MailboxDir = os.TempDir()
			} else {
				config.MailboxDir = filepath.Join(os.TempDir(), filepath.FromSlash(tail))
			}
		}
	}

	var mailbox *storage.Mailbox

	// Only create mailbox if directory is specified
	if config.MailboxDir != "" {
		mailbox, err = storage.NewMailbox(config.MailboxDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create mailbox: %w", err)
		}
	}

	return &Server{
		config:   config,
		mailbox:  mailbox,
		logger:   logger,
		sessions: make(map[*Session]struct{}),
	}, nil
}

// GetMailbox returns the server's mailbox
func (s *Server) GetMailbox() *storage.Mailbox {
	return s.mailbox
}

// Start begins listening on all configured ports
func (s *Server) Start() error {
	// Install a termination handler to perform graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		// Trigger shutdown with configured default timeout
		ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
		defer cancel()
		s.logger.Info("Shutdown signal received, initiating graceful shutdown")
		if err := s.Shutdown(ctx); err != nil {
			s.logger.Error("Graceful shutdown failed", err)
		}
	}()

	// Start normal behaviour port
	go s.startPortListener(s.config.Port, "Normal behaviour")

	// Start all special behaviour ports using discrete DelayOptions offsets
	go s.startPortRangeListeners(s.config.GreetingDelayPortStart, PortRangeSize, "Greeting delay")
	go s.startPortRangeListeners(s.config.DropDelayPortStart, PortRangeSize, "Drop delay")

	// Start TLS ports (always available with self-signed certificates)
	go s.startTLSPortListener(s.config.TLSPort, "Implicit TLS")
	go s.startPortListener(s.config.STARTTLSPort, "STARTTLS")

	// Log the started ports and ranges explicitly
	// (we intentionally log the base/range rather than the full slice of ports)

	s.logger.Info("BadSMTP server started",
		logging.F("normal_port", s.config.Port),
		logging.F("greeting_delay_ports", fmt.Sprintf("%d-%d", s.config.GreetingDelayPortStart, s.config.GreetingDelayPortStart+PortRangeEnd)),
		logging.F("drop_delay_ports", fmt.Sprintf("%d-%d", s.config.DropDelayPortStart, s.config.DropDelayPortStart+PortRangeEnd)),
		logging.F("tls_port", s.config.TLSPort),
		logging.F("starttls_port", s.config.STARTTLSPort),
		logging.F("log_level", s.config.LogConfig.Level.String()),
		logging.F("log_output", s.config.LogConfig.Output))

	// Keep main goroutine alive until shutdown completes.
	// Block until sessionsWG is done and no listeners are active.
	// We'll wait on a channel that Shutdown will close when finished.
	<-make(chan struct{})
	return nil
}

func (s *Server) startPortListener(port int, description string) {
	addr := net.JoinHostPort(s.config.ListenAddress, fmt.Sprintf("%d", port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		// If the port is already in use, log a warning and skip starting this listener.
		var warnErr bool
		if errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") {
			warnErr = true
		}
		if warnErr {
			s.logger.Warn("Port already in use; skipping listener",
				logging.F("port", port), logging.F("desc", description), logging.F("addr", addr))
			return
		}
		s.logger.Error("Failed to listen on port",
			fmt.Errorf("%v", err),
			logging.F("port", port), logging.F("desc", description), logging.F("addr", addr))
		return
	}
	// register listener for shutdown
	s.addListener(listener)
	defer func() {
		s.removeListener(listener)
		// Defer only removes this listener record; actual Close is performed centrally
	}()

	s.logger.Info("Listening on port", logging.F("port", port), logging.F("desc", description), logging.F("addr", addr))

	for {
		conn, err := listener.Accept()
		if err != nil {
			// If the listener was closed as part of shutdown, then exit loop.
			// Some implementations return an error whose message contains
			// "use of closed network connection" instead of net.ErrClosed; treat both as closed.
			if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") {
				s.logger.Info("Listener closed, exiting accept loop", logging.F("port", port))
				return
			}
			s.logger.Warn("Failed to accept connection on port", logging.F("port", port), logging.F("err", err))
			continue
		}

		go s.handleConnectionForPort(conn, port)
	}
}

func (s *Server) startPortRangeListeners(startPort, count int, description string) {
	// Start only the discrete offsets defined in DelayOptions
	for i := 0; i < count; i++ {
		port := startPort + i
		delay := DelayOptions[i]
		desc := fmt.Sprintf("%s (%ds)", description, delay)
		go s.startPortListener(port, desc)
	}
}

// createTLSListener builds a tls.Config with dynamic certificate generation and
// starts listening on the given port. It returns the listener or an error.
func (s *Server) createTLSListener(port int) (net.Listener, error) {
	// Create TLS configuration with dynamic certificate generation
	tlsConfig := &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			hostname := hello.ServerName
			if hostname == "" {
				hostname = s.config.GetTLSHostname()
			}

			// Try to load certificate from files first
			if s.config.HasTLS() {
				if cert, err := tls.LoadX509KeyPair(s.config.TLSCertFile, s.config.TLSKeyFile); err == nil {
					return &cert, nil
				}
				s.logger.Warn("Failed to load TLS certificate from files, generating self-signed for", logging.F("hostname", hostname))
			}

			// Generate self-signed certificate for the requested hostname
			cert, err := s.config.GenerateSelfSignedCert(hostname)
			if err != nil {
				return nil, fmt.Errorf("failed to generate self-signed certificate: %v", err)
			}
			return &cert, nil
		},
		MinVersion: MinTLSVersion,
	}

	addr := net.JoinHostPort(s.config.ListenAddress, fmt.Sprintf("%d", port))
	listener, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return nil, err
	}
	return listener, nil
}

func (s *Server) startTLSPortListener(port int, description string) {
	// Create the listener using helper
	listener, err := s.createTLSListener(port)
	if err != nil {
		// Handle address-in-use similarly to plain TCP listener
		var warnErr bool
		if errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") {
			warnErr = true
		}
		if warnErr {
			s.logger.Warn(
				"TLS port already in use; skipping TLS listener",
				logging.F("port", port),
				logging.F("desc", description),
				logging.F("addr", net.JoinHostPort(s.config.ListenAddress, fmt.Sprintf("%d", port))),
			)
			return
		}
		s.logger.Error(
			"Failed to listen on TLS port",
			err,
			logging.F("port", port),
			logging.F("desc", description),
			logging.F("addr", net.JoinHostPort(s.config.ListenAddress, fmt.Sprintf("%d", port))),
		)
		return
	}

	// register listener for shutdown
	s.addListener(listener)
	defer func() {
		s.removeListener(listener)
		// Defer only removes this listener record; listener.Close() will be called by Shutdown via closeAllListeners
	}()

	s.logger.Info(
		"Listening on TLS port",
		logging.F("port", port),
		logging.F("desc", description),
		logging.F("addr", net.JoinHostPort(s.config.ListenAddress, fmt.Sprintf("%d", port))),
	)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") {
				s.logger.Info("TLS listener closed, exiting accept loop", logging.F("port", port))
				return
			}
			s.logger.Warn("Failed to accept TLS connection on port", logging.F("port", port), logging.F("err", err))
			continue
		}

		go s.handleTLSConnectionForPort(conn, port)
	}
}

func (s *Server) handleConnectionForPort(conn net.Conn, port int) {
	// Create a config specific to this port
	portConfig := *s.config
	portConfig.Port = port
	portConfig.AnalysePortBehaviour()

	// Extract hostname from local address if possible
	hostname := s.extractHostname(conn)

	session := NewSessionWithHostname(conn, &portConfig, s.mailbox, hostname)
	// register active session so shutdown can close it
	s.registerSession(session)
	defer s.unregisterSession(session)

	if err := session.Handle(); err != nil {
		s.logger.Error("Session error on port", err, logging.F("port", port))
	}
}

func (s *Server) handleTLSConnectionForPort(conn net.Conn, port int) {
	// Create a config specific to this port
	portConfig := *s.config
	portConfig.Port = port
	portConfig.AnalysePortBehaviour()

	// Extract hostname from local address or TLS SNI
	hostname := s.extractHostname(conn)
	if tlsConn, ok := conn.(*tls.Conn); ok {
		// Use SNI hostname if available
		state := tlsConn.ConnectionState()
		if state.ServerName != "" {
			hostname = state.ServerName
		}
	}

	session := NewSessionWithHostname(conn, &portConfig, s.mailbox, hostname)
	// For implicit TLS, mark the connection as already TLS-enabled
	if tlsConn, ok := conn.(*tls.Conn); ok {
		tlsState := tlsConn.ConnectionState()
		session.tlsState = &tlsState
	}

	// register active session so shutdown can close it
	s.registerSession(session)
	defer s.unregisterSession(session)

	if err := session.Handle(); err != nil {
		s.logger.Error("TLS session error on port", err, logging.F("port", port))
	}
}

// extractHostname attempts to extract the hostname from the connection
func (s *Server) extractHostname(conn net.Conn) string {
	// For SMTP, the hostname is typically the local address the client connected to
	// This works when using different hostnames that resolve to the same IP
	localAddr := conn.LocalAddr()
	if tcpAddr, ok := localAddr.(*net.TCPAddr); ok {
		// Try to do a reverse DNS lookup to get the hostname
		if names, err := net.LookupAddr(tcpAddr.IP.String()); err == nil && len(names) > 0 {
			// Return the first hostname found
			hostname := names[0]
			// Remove trailing dot if present
			hostname = strings.TrimSuffix(hostname, ".")
			s.logger.Debug("Resolved hostname via reverse DNS",
				logging.F("ip", tcpAddr.IP.String()),
				logging.F("hostname", hostname))
			return hostname
		} else if err != nil {
			s.logger.Debug("Reverse DNS lookup failed",
				logging.F("ip", tcpAddr.IP.String()),
				logging.F("error", err.Error()))
		}
		// If reverse DNS fails, return the IP as a fallback
		return tcpAddr.IP.String()
	}
	return ""
}

func validatePortConfiguration(config *Config) error {
	return config.ValidatePortConfiguration()
}

func rangesOverlap(start1, end1, start2, end2 int) bool {
	return start1 <= end2 && start2 <= end1
}

// addListener registers a listener so it can be closed on shutdown
func (s *Server) addListener(l net.Listener) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	s.listeners = append(s.listeners, l)
}

// removeListener removes a registered listener
func (s *Server) removeListener(l net.Listener) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	for i := range s.listeners {
		if s.listeners[i] == l {
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			return
		}
	}
}

// registerSession records an active session and increments the waitgroup
func (s *Server) registerSession(sess *Session) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[*Session]struct{})
	}
	s.sessions[sess] = struct{}{}
	s.sessionsWG.Add(1)
}

// unregisterSession removes a session and decrements the waitgroup
func (s *Server) unregisterSession(sess *Session) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	delete(s.sessions, sess)
	s.sessionsWG.Done()
}

// activeSessionSnapshot returns a slice copy of active sessions
func (s *Server) activeSessionSnapshot() []*Session {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	out := make([]*Session, 0, len(s.sessions))
	for k := range s.sessions {
		out = append(out, k)
	}
	return out
}

// activeSessionCount returns number of active sessions
func (s *Server) activeSessionCount() int {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	return len(s.sessions)
}

// closeAllListeners closes all registered listeners to stop accepting new connections
func (s *Server) closeAllListeners() {
	s.listenersMu.Lock()
	listeners := append([]net.Listener(nil), s.listeners...)
	s.listenersMu.Unlock()
	for _, l := range listeners {
		if err := l.Close(); err != nil {
			// Ignore expected "use of closed network connection" errors
			if !errors.Is(err, net.ErrClosed) {
				s.logger.Debug("Error closing listener in closeAllListeners", logging.F("err", err))
			}
		}
	}
}

// Shutdown attempts a graceful shutdown: stop accepting new connections, notify active sessions
// to terminate with a 421 and wait up to the provided context for them to finish.
func (s *Server) Shutdown(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&s.shuttingDown, 0, 1) {
		// already shutting down
		return nil
	}

	// Stop accepting new connections
	s.closeAllListeners()

	count := s.activeSessionCount()
	if count == 0 {
		s.logger.Info("No active sessions; shutdown complete")
		return nil
	}

	s.logger.Info("Shutting down: notifying active sessions", logging.F("sessions", count))

	// Notify each session to close with 421. Use a short timeout derived from ctx.
	// Build a per-session context with remaining time, but at least 2s.
	deadline, hasDeadline := ctx.Deadline()
	perSessTimeout := 2 * time.Second
	if hasDeadline {
		remaining := time.Until(deadline)
		if remaining > 0 {
			perSessTimeout = remaining
		}
	}

	sessions := s.activeSessionSnapshot()
	for _, sess := range sessions {
		go func(ss *Session) {
			cctx, cancel := context.WithTimeout(ctx, perSessTimeout)
			defer cancel()
			if err := ss.CloseWith421(cctx, "Service shutting down"); err != nil {
				s.logger.Debug("CloseWith421 returned error", logging.F("err", err))
			}
		}(sess)
	}

	// Wait for sessions to finish or context timeout
	done := make(chan struct{})
	go func() {
		s.sessionsWG.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		s.logger.Info("All sessions closed; shutdown complete")
		return nil
	}
}
