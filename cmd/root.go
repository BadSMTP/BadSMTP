// Package cmd contains the CLI wiring for the badsmtp application.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"badsmtp/server"

	"github.com/knadh/koanf"
	kyaml "github.com/knadh/koanf/parsers/yaml"
	kenv "github.com/knadh/koanf/providers/env"
	kfile "github.com/knadh/koanf/providers/file"
	kposflag "github.com/knadh/koanf/providers/posflag"
	"github.com/spf13/cobra"
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
					configPath := fmt.Sprintf("%s/badsmtp.%s", dir, ext)
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

		// Load environment variables (prefix BADSMTP) - medium priority, overrides config file
		// use a replacer function to map ENV names to koanf keys
		if err := k.Load(kenv.Provider("BADSMTP_", "_", createEnvReplacer().Replace), nil); err != nil {
			return fmt.Errorf("failed to load env: %w", err)
		}

		// Load command-line flags last (highest priority) - overrides everything
		if err := k.Load(kposflag.Provider(cmd.PersistentFlags(), ":", k), nil); err != nil {
			return fmt.Errorf("failed to load flags: %w", err)
		}

		// Unmarshal into typed config
		var cfg server.Config
		if err := k.Unmarshal("", &cfg); err != nil {
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

func createEnvReplacer() *strings.Replacer {
	return strings.NewReplacer("-", "_", ".", "_")
}

// getConfigSearchPaths returns the directories to search for config files, in order of precedence.
// The order is: current directory, $HOME/.badsmtp/, /etc/badsmtp/
func getConfigSearchPaths() []string {
	paths := []string{"."}

	// Add $HOME/.badsmtp/ if HOME is set
	if home := os.Getenv("HOME"); home != "" {
		paths = append(paths, home+"/.badsmtp")
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
}

// Execute sets the version and runs the root command.
func Execute(version string) error {
	rootCmd.Version = version
	return rootCmd.Execute()
}
