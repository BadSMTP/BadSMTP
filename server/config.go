// Package server provides the SMTP server implementation for BadSMTP.
package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"badsmtp/logging"
)

const (
	// DefaultPort is the normal port that the server listens on, with no special behaviour.
	// It is above 1000 so it can be run as an unprivileged user.
	DefaultPort = 2525
	// DefaultGreetingDelayStart is the first port number in the range used to trigger greeting delays.
	DefaultGreetingDelayStart = 25200
	// DefaultDropDelayStart is the first port number in the range used to trigger delayed drops.
	DefaultDropDelayStart = 25600
	// DefaultTLSPort is the port number to listen for implicit TLS connections (SMTPS).
	DefaultTLSPort = 25465
	// DefaultSTARTTLSPort is the port number to listen for STARTTLS connections (SMTP+STARTTLS).
	DefaultSTARTTLSPort = 25587

	// DefaultTLSHostname is the default hostname used for generated self-signed certificates.
	DefaultTLSHostname = "badsmtp.test"

	// CertValidityHours is the number of hours that a generated certificate is valid for.
	CertValidityHours = 24

	// DefaultRSAKeySize is the default size for RSA keys used in self-signed certificates.
	DefaultRSAKeySize = 2048

	// MaxMessageSize is the maximum allowed message size in bytes (10MB)
	MaxMessageSize = 10 * 1024 * 1024
	// MaxCommandLength is the maximum allowed SMTP command length in bytes
	MaxCommandLength = 4096
	// MaxScannerBuffer is the maximum buffer size for scanning input
	MaxScannerBuffer = 64 * 1024
)

// DelayOptions is the discrete list of supported delays (seconds) mapped by offset within a behaviour base port.
// Offsets: 0..9 -> delays {0s,1s,2s,8s,10s,30s,60s,120s,300s,600s}
var DelayOptions = []int{0, 1, 2, 8, 10, 30, 60, 120, 300, 600}

// DelayCount is the number of entries in DelayOptions
const DelayCount = 10

// PortRange represents a range of ports with validation capabilities.
type PortRange struct {
	Name  string
	Start int
	End   int
}

// NewPortRange creates a new port range.
func NewPortRange(name string, start, rangeSize int) PortRange {
	return PortRange{
		Name:  name,
		Start: start,
		End:   start + rangeSize,
	}
}

// Contains checks if a port is within this range.
func (pr PortRange) Contains(port int) bool {
	return port >= pr.Start && port <= pr.End
}

// OverlapsWith checks if this range overlaps with another range.
func (pr PortRange) OverlapsWith(other PortRange) bool {
	return pr.Start <= other.End && other.Start <= pr.End
}

// ConflictError returns a formatted error for range conflicts.
func (pr PortRange) ConflictError(other PortRange) error {
	return fmt.Errorf("port ranges overlap: %s (%d-%d) and %s (%d-%d)",
		pr.Name, pr.Start, pr.End, other.Name, other.Start, other.End)
}

// PortConflictError returns a formatted error for port conflicts with this range.
func (pr PortRange) PortConflictError(port int, portName string) error {
	return fmt.Errorf("%s port %d conflicts with %s range (%d-%d)",
		portName, port, pr.Name, pr.Start, pr.End)
}

// PortValidator manages port validation across multiple ranges and individual ports.
type PortValidator struct {
	ranges []PortRange
	ports  map[string]int
}

// NewPortValidator creates a new port validator.
func NewPortValidator() *PortValidator {
	return &PortValidator{
		ranges: make([]PortRange, 0),
		ports:  make(map[string]int),
	}
}

// AddRange adds a port range to validate.
func (pv *PortValidator) AddRange(pr PortRange) {
	pv.ranges = append(pv.ranges, pr)
}

// AddPort adds an individual port to validate.
func (pv *PortValidator) AddPort(name string, port int) {
	pv.ports[name] = port
}

// ValidateRangeOverlaps checks for overlaps between all ranges.
func (pv *PortValidator) ValidateRangeOverlaps() error {
	for i := 0; i < len(pv.ranges); i++ {
		for j := i + 1; j < len(pv.ranges); j++ {
			if pv.ranges[i].OverlapsWith(pv.ranges[j]) {
				return pv.ranges[i].ConflictError(pv.ranges[j])
			}
		}
	}
	return nil
}

// ValidatePortRangeConflicts checks if any individual ports conflict with ranges.
func (pv *PortValidator) ValidatePortRangeConflicts() error {
	for portName, port := range pv.ports {
		for _, pr := range pv.ranges {
			if pr.Contains(port) {
				return pr.PortConflictError(port, portName)
			}
		}
	}
	return nil
}

// ValidatePortConflicts checks whether any individual ports conflict with each other.
func (pv *PortValidator) ValidatePortConflicts() error {
	portList := make([]struct {
		name string
		port int
	}, 0, len(pv.ports))
	for name, port := range pv.ports {
		portList = append(portList, struct {
			name string
			port int
		}{name, port})
	}

	for i := 0; i < len(portList); i++ {
		for j := i + 1; j < len(portList); j++ {
			if portList[i].port == portList[j].port {
				return fmt.Errorf("%s port %d conflicts with %s port %d",
					portList[i].name, portList[i].port, portList[j].name, portList[j].port)
			}
		}
	}
	return nil
}

// ValidateAll runs all validation checks.
func (pv *PortValidator) ValidateAll() error {
	if err := pv.ValidateRangeOverlaps(); err != nil {
		return err
	}
	if err := pv.ValidatePortRangeConflicts(); err != nil {
		return err
	}
	if err := pv.ValidatePortConflicts(); err != nil {
		return err
	}
	return nil
}

// Config represents the server configuration.
type Config struct {
	Port          int    `mapstructure:"port"`
	MailboxDir    string `mapstructure:"mailbox_dir"`
	ListenAddress string `mapstructure:"listen_address"` // new: IP to bind listeners to
	GreetingDelay int    `mapstructure:"greeting_delay"`
	DropDelay     int    `mapstructure:"drop_delay"`
	DropImmediate bool   `mapstructure:"drop_immediate"`

	// Port range configurations
	GreetingDelayPortStart int `mapstructure:"greeting_delay_port_start"`
	DropDelayPortStart     int `mapstructure:"drop_delay_port_start"`

	// TLS configuration
	TLSCertFile  string `mapstructure:"tls_cert_file"`
	TLSKeyFile   string `mapstructure:"tls_key_file"`
	TLSPort      int    `mapstructure:"tls_port"`      // Port for implicit TLS (default 25465)
	STARTTLSPort int    `mapstructure:"starttls_port"` // Port for STARTTLS (default 25587)
	TLSHostname  string `mapstructure:"tls_hostname"`  // Hostname for TLS certificate (default: "badsmtp.test")

	// Hostname-based mailbox routing
	EnableHostnameRouting bool              `mapstructure:"enable_hostname_routing"` // Enable hostname-based routing
	HostnameMailboxMap    map[string]string `mapstructure:"hostname_mailbox_map"`
	// hostname -> mailbox directory mapping (for static config)
	DefaultMailboxDir string `mapstructure:"default_mailbox_dir"` // fallback directory for unmapped hostnames

	// Extensions: Pluggable architecture for extending functionality
	// These interfaces allow external packages to extend functionality
	MessageStore     MessageStore     `mapstructure:"-"` // Where messages are stored (default: local files)
	Authenticator    Authenticator    `mapstructure:"-"` // How users authenticate (default: goodauth/badauth patterns)
	Authorizer       Authorizer       `mapstructure:"-"` // What authenticated users can do (default: allow all)
	RateLimiter      RateLimiter      `mapstructure:"-"` // Connection/message rate limiting (default: no limits)
	Observer         SessionObserver  `mapstructure:"-"` // Session event notifications (default: no-op)
	CapabilityParser CapabilityParser `mapstructure:"-"` // EHLO hostname capability parsing (default: pass-through)
	SMTPExtensions   []SMTPExtension  `mapstructure:"-"` // Custom SMTP commands and capabilities (default: empty slice)

	// Logging configuration
	LogConfig logging.LogConfig `mapstructure:"-"`
}

// EnsureDefaults sets default extension implementations if not provided.
// This allows the server to work out-of-the-box with sensible defaults.
func (c *Config) EnsureDefaults() {
	c.ensureScalarDefaults()
	c.ensureMapDefaults()
	c.ensureExtensionDefaults()
}

func (c *Config) ensureScalarDefaults() {
	// Set simple scalar defaults if zero-valued
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.MailboxDir == "" {
		c.MailboxDir = "./mailbox"
	}
	if c.ListenAddress == "" {
		c.ListenAddress = "127.0.0.1"
	}
	if c.GreetingDelayPortStart == 0 {
		c.GreetingDelayPortStart = DefaultGreetingDelayStart
	}
	if c.DropDelayPortStart == 0 {
		c.DropDelayPortStart = DefaultDropDelayStart
	}
	if c.TLSPort == 0 {
		c.TLSPort = DefaultTLSPort
	}
	if c.STARTTLSPort == 0 {
		c.STARTTLSPort = DefaultSTARTTLSPort
	}
	if c.TLSHostname == "" {
		c.TLSHostname = DefaultTLSHostname
	}
}

func (c *Config) ensureMapDefaults() {
	// Ensure maps are initialised
	if c.HostnameMailboxMap == nil {
		c.HostnameMailboxMap = make(map[string]string)
	}
}

func (c *Config) ensureExtensionDefaults() {
	// Ensure extension defaults
	if c.MessageStore == nil {
		c.MessageStore = NewDefaultMessageStore(c.MailboxDir)
	}
	if c.Authenticator == nil {
		c.Authenticator = NewDefaultAuthenticator()
	}
	if c.Authorizer == nil {
		c.Authorizer = NewAllowAllAuthorizer()
	}
	if c.RateLimiter == nil {
		// Use a simple in-memory rate limiter by default
		c.RateLimiter = NewSimpleRateLimiter()
	}
	if c.Observer == nil {
		c.Observer = &NoOpObserver{}
	}
	if c.CapabilityParser == nil {
		c.CapabilityParser = NewDefaultCapabilityParser()
	}
}

// loadHostnameMappingsViper loads hostname mappings from environment variables.
// Keep this independent of any config library — callers can invoke it after
// they populate the Config struct from flags/env/files.
func loadHostnameMappingsViper(config *Config) {
	if config.HostnameMailboxMap == nil {
		config.HostnameMailboxMap = make(map[string]string)
	}

	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "BADSMTP_HOSTNAME_MAPPING_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				envName := parts[0]
				hostname := strings.TrimPrefix(envName, "BADSMTP_HOSTNAME_MAPPING_")
				hostname = strings.ReplaceAll(hostname, "_", ".")
				config.HostnameMailboxMap[hostname] = parts[1]
			}
		}
	}
}

// GetMailboxDir returns the appropriate mailbox directory for a given hostname.
func (c *Config) GetMailboxDir(hostname string) string {
	if !c.EnableHostnameRouting {
		return c.MailboxDir
	}

	// Clean hostname (remove port if present)
	if h, _, ok := strings.Cut(hostname, ":"); ok {
		hostname = h
	}

	// Look for exact hostname match in static mapping
	if dir, exists := c.HostnameMailboxMap[hostname]; exists {
		return dir
	}

	// Use default directory for unmapped hostnames
	if c.DefaultMailboxDir != "" {
		return c.DefaultMailboxDir
	}

	// Fall back to original mailbox directory
	return c.MailboxDir
}

// HasTLS checks if TLS is enabled.
func (c *Config) HasTLS() bool {
	// TLS is available if certificate files are provided OR if we can generate self-signed certificates
	return (c.TLSCertFile != "" && c.TLSKeyFile != "") || true
}

// GetTLSHostname returns the hostname for TLS certificates.
func (c *Config) GetTLSHostname() string {
	if c.TLSHostname != "" {
		return c.TLSHostname
	}
	return DefaultTLSHostname
}

// GenerateSelfSignedCert generates a self-signed certificate for the given hostname.
func (c *Config) GenerateSelfSignedCert(hostname string) (tls.Certificate, error) {
	// Generate private key (ECDSA P-256)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate private key: %v", err)
	}

	// Certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			//nolint:misspell // 'Organization' is the stdlib field name
			Organization:       []string{"BadSMTP Test Server"},
			Country:            []string{"US"},
			Province:           []string{"Test"},
			Locality:           []string{"Test"},
			OrganizationalUnit: []string{"Test"},
			CommonName:         hostname,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(CertValidityHours * time.Hour), // Valid for 24 hours
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add hostname as SAN
	template.DNSNames = []string{hostname}
	if ip := net.ParseIP(hostname); ip != nil {
		template.IPAddresses = []net.IP{ip}
	}

	// Generate certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create certificate: %v", err)
	}

	// Create PEM blocks (ECDSA private key marshalling to ASN.1 DER via x509.MarshalECPrivateKey)
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to marshal EC private key: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	// Create TLS certificate
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create X509 key pair: %v", err)
	}

	// Do not log certificate generation here; caller (session/server) may log with full context if desired.
	return cert, nil
}

// AnalysePortBehaviour analyses the port number to determine connection behaviour.
func (c *Config) AnalysePortBehaviour() {
	port := c.Port

	// Greeting delay: discrete options (default 25200..25209)
	if port >= c.GreetingDelayPortStart && port < c.GreetingDelayPortStart+DelayCount {
		offset := port - c.GreetingDelayPortStart
		c.GreetingDelay = DelayOptions[offset]
	}

	// Drop with delay: discrete options (default 25600..25609)
	if port >= c.DropDelayPortStart && port < c.DropDelayPortStart+DelayCount {
		offset := port - c.DropDelayPortStart
		c.DropDelay = DelayOptions[offset]
		// Treat offset 0 as immediate drop (0s)
		if offset == 0 {
			c.DropImmediate = true
		}
	}

	// No separate ImmediateDropPort anymore; immediate drop represented by DropDelayPortStart + offset 0
}

// GetBehaviourDescription returns a human-readable description of the port behaviour.
func (c *Config) GetBehaviourDescription() string {
	port := c.Port

	switch {
	case port >= c.GreetingDelayPortStart && port < c.GreetingDelayPortStart+DelayCount:
		offset := port - c.GreetingDelayPortStart
		delay := DelayOptions[offset]
		return fmt.Sprintf("Greeting delay: %ds", delay)
	case port >= c.DropDelayPortStart && port < c.DropDelayPortStart+DelayCount:
		offset := port - c.DropDelayPortStart
		delay := DelayOptions[offset]
		if offset == 0 {
			return "Immediate drop"
		}
		return fmt.Sprintf("Drop with delay: %ds", delay)
	default:
		return "Normal behaviour"
	}
}

// ValidatePortConfiguration validates that all port configurations don't conflict
func (c *Config) ValidatePortConfiguration() error {
	// The number of offsets used from each base start is DelayCount
	const RangeSize = DelayCount - 1

	validator := NewPortValidator()

	// Add port ranges (now small discrete ranges of DelayCount ports)
	validator.AddRange(NewPortRange("greeting delay", c.GreetingDelayPortStart, RangeSize))
	validator.AddRange(NewPortRange("drop delay", c.DropDelayPortStart, RangeSize))

	// Add individual ports
	validator.AddPort("normal", c.Port)

	// Add TLS ports if TLS is enabled
	if c.HasTLS() {
		validator.AddPort("TLS", c.TLSPort)
		validator.AddPort("STARTTLS", c.STARTTLSPort)
	}

	// Run all validations
	if err := validator.ValidateAll(); err != nil {
		return err
	}

	// Enforce Option C: reject ports that fall into the old-style extended ranges but outside our lookup.
	// If the user configures a normal server Port that lies within the previous wide ranges but outside the
	// allowed DelayCount window, it's likely a misconfiguration.
	// Check if server Port falls into greeting/drop ranges but offset >= DelayCount -> reject.
	if c.Port >= c.GreetingDelayPortStart && c.Port < c.GreetingDelayPortStart+100 {
		offset := c.Port - c.GreetingDelayPortStart
		if offset >= DelayCount {
			return fmt.Errorf("configured port %d is within greeting delay base but outside supported offsets 0..%d", c.Port, DelayCount-1)
		}
	}
	if c.Port >= c.DropDelayPortStart && c.Port < c.DropDelayPortStart+100 {
		offset := c.Port - c.DropDelayPortStart
		if offset >= DelayCount {
			return fmt.Errorf("configured port %d is within drop delay base but outside supported offsets 0..%d", c.Port, DelayCount-1)
		}
	}

	return nil
}

// LoadConfig creates a Config populated from defaults and environment variables.
// This function mirrors the behaviour the test-suite expects: apply sensible
// defaults, then allow BADSMTP_* environment variables to override specific
// fields. It also reads hostname mappings via loadHostnameMappingsViper.
func LoadConfig() (*Config, error) {
	cfg := &Config{}

	// Apply defaults first so we have sensible baseline values.
	cfg.EnsureDefaults()

	// Consolidate string env overrides into a map to reduce branching and cyclomatic complexity.
	stringEnvMap := map[string]*string{
		"BADSMTP_MAILBOXDIR":     &cfg.MailboxDir,
		"BADSMTP_TLSCERTFILE":    &cfg.TLSCertFile,
		"BADSMTP_TLSKEYFILE":     &cfg.TLSKeyFile,
		"BADSMTP_TLSHOSTNAME":    &cfg.TLSHostname,
		"BADSMTP_LISTEN_ADDRESS": &cfg.ListenAddress,
	}
	for key, dest := range stringEnvMap {
		if v := os.Getenv(key); v != "" {
			*dest = v
		}
	}

	// Consolidate integer env overrides into a map and parse in a loop.
	intEnvMap := map[string]*int{
		"BADSMTP_PORT":                   &cfg.Port,
		"BADSMTP_GREETINGDELAYPORTSTART": &cfg.GreetingDelayPortStart,
		"BADSMTP_DROPDELAYPORTSTART":     &cfg.DropDelayPortStart,
		"BADSMTP_TLSPORT":                &cfg.TLSPort,
		"BADSMTP_STARTTLSPORT":           &cfg.STARTTLSPort,
	}
	for key, dest := range intEnvMap {
		if v := os.Getenv(key); v != "" {
			i, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("invalid integer for %s: %w", key, err)
			}
			*dest = i
		}
	}

	// Load hostname mappings from env prefixed variables
	loadHostnameMappingsViper(cfg)

	return cfg, nil
}
