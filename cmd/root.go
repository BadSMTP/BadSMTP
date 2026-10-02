// Package cmd contains the CLI wiring for the badsmtp application.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"badsmtp/server"

	kyaml "github.com/knadh/koanf/parsers/yaml"
	kenv "github.com/knadh/koanf/providers/env"
	kfile "github.com/knadh/koanf/providers/file"
	kposflag "github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var rootCmd = &cobra.Command{
	Use:   "badsmtp",
	Short: "BadSMTP SMTP testing server",
	Long:  "BadSMTP is a configurable SMTP server for testing clients and integrations.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Create koanf instance
		k := koanf.New(".")

		// Load config file first (lowest priority, except for built-in defaults)
		// Check for --config flag to see if user specified a custom config path
		cfgPath := cmd.Flag("config").Value.String()
		if cfgPath != "" {
			if err := k.Load(kfile.Provider(cfgPath), kyaml.Parser()); err != nil {
				return fmt.Errorf("failed to load config file %s: %w", cfgPath, err)
			}
		} else {
			// Search for config files in standard locations (in order of precedence)
			searchPaths := getConfigSearchPaths()
			extensions := []string{"yaml", "yml", "json"}

			configFound := false
			for _, dir := range searchPaths {
				for _, ext := range extensions {
					configPath := filepath.Join(dir, "badsmtp."+ext)
					if _, err := os.Stat(configPath); err == nil {
						if err := k.Load(kfile.Provider(configPath), kyaml.Parser()); err != nil {
							return fmt.Errorf("failed to load config file %s: %w", configPath, err)
						}
						configFound = true
						break
					}
				}
				if configFound {
					break
				}
			}
		}

		// Load environment variables (prefix BADSMTP_) - medium priority, overrides
		// config file. Strip the prefix and lower-case the name so that, for example,
		// BADSMTP_LOG_LEVEL maps to the log_level config key.
		envProvider := kenv.Provider("BADSMTP_", ".", func(s string) string {
			return strings.ToLower(strings.TrimPrefix(s, "BADSMTP_"))
		})
		if err := k.Load(envProvider, nil); err != nil {
			return fmt.Errorf("failed to load env: %w", err)
		}

		// Load command-line flags last (highest priority) - overrides everything.
		// Normalise dashed flag names to the underscore config keys (e.g.
		// --greeting-delay-port-start -> greeting_delay_port_start) so they line up
		// with the mapstructure tags and config-file keys.
		flagProvider := kposflag.ProviderWithFlag(cmd.PersistentFlags(), ".", k, func(f *pflag.Flag) (string, any) {
			return flagConfigKey(f.Name), kposflag.FlagVal(cmd.PersistentFlags(), f)
		})
		if err := k.Load(flagProvider, nil); err != nil {
			return fmt.Errorf("failed to load flags: %w", err)
		}

		// Unmarshal into typed config using the mapstructure struct tags.
		var cfg server.Config
		if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "mapstructure"}); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}

		// Apply defaults
		cfg.EnsureDefaults()

		srv, err := server.NewServer(&cfg)
		if err != nil {
			return fmt.Errorf("failed to create server: %w", err)
		}

		return srv.Start()
	},
}

// flagKeyAliases maps flag names whose friendly form differs from their config
// key by more than dash/underscore spelling.
var flagKeyAliases = map[string]string{
	"mailbox": "mailbox_dir",
}

// flagConfigKey maps a flag name to its config key: an explicit alias if one
// exists, otherwise the dashed name with dashes turned into underscores.
func flagConfigKey(name string) string {
	if alias, ok := flagKeyAliases[name]; ok {
		return alias
	}
	return strings.ReplaceAll(name, "-", "_")
}

// getConfigSearchPaths returns the directories to search for config files, in order of precedence.
// The order is: current directory, $HOME/.badsmtp/, /etc/badsmtp/
func getConfigSearchPaths() []string {
	paths := []string{"."}

	// Add $HOME/.badsmtp/ if HOME is set
	if home := os.Getenv("HOME"); home != "" {
		paths = append(paths, filepath.Join(home, ".badsmtp"))
	}

	// Add system-wide config directory
	paths = append(paths, "/etc/badsmtp")

	return paths
}

// RegisterFlags registers persistent flags for the root command. This replaces an init() function
// to satisfy the linter rule against init usage and allows callers to control ordering.
func RegisterFlags() {
	pf := rootCmd.PersistentFlags()
	pf.IntP("port", "p", server.DefaultPort, "Port to listen on")
	pf.StringP("mailbox", "m", "./mailbox", "Directory to store messages")
	pf.StringP("config", "c", "", "Configuration file path")
	pf.Bool("enable-hostname-routing", false, "Enable hostname-based routing")
	pf.String("default-mailbox-dir", "", "Default mailbox directory for unmapped hostnames")

	// Listen address (bind IP)
	pf.String("listen-address", "127.0.0.1", "IP address to bind listeners to (maps to listen_address)")

	// Port range configurations
	pf.Int("greeting-delay-port-start", server.DefaultGreetingDelayStart, "Starting port for greeting delays")
	pf.Int("drop-delay-port-start", server.DefaultDropDelayStart, "Starting port for drop delays")

	// TLS configuration
	pf.String("tls-cert-file", "", "Path to TLS certificate file")
	pf.String("tls-key-file", "", "Path to TLS private key file")
	pf.Int("tls-port", server.DefaultTLSPort, "Port for implicit TLS (SMTPS)")
	pf.Int("starttls-port", server.DefaultSTARTTLSPort, "Port for STARTTLS")
	pf.String("tls-hostname", server.DefaultTLSHostname, "Hostname for TLS certificate")

	// Logging configuration (empty defaults so unset flags fall back to config
	// file/env values and finally the built-in logging defaults)
	pf.String("log-level", "", "Log level: debug, info, warn, error (default: info)")
	pf.String("log-format", "", "Log format: json or text (default: json)")
	pf.String("log-output", "", "Log output: stdout, syslog, tcp, udp (default: stdout)")
	pf.String("log-remote-addr", "", "Remote address (host:port) for tcp/udp log output")
	pf.String("syslog-facility", "", "Syslog facility: mail, daemon, local0-local7 (default: mail)")
	pf.Bool("log-trace", false, "Include source file and line in log records")
}

// Execute sets the version and runs the root command.
func Execute(version string) error {
	rootCmd.Version = version
	return rootCmd.Execute()
}
