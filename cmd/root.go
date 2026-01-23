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

		// Bind flags provider (posflag) using the root command's persistent flags
		if err := k.Load(kposflag.Provider(cmd.PersistentFlags(), ":", k), nil); err != nil {
			return fmt.Errorf("failed to load flags: %w", err)
		}

		// Load config file if provided via flag
		cfgPath, err := cmd.PersistentFlags().GetString("config")
		if err != nil {
			return fmt.Errorf("failed to read config flag: %w", err)
		}
		if cfgPath != "" {
			if err := k.Load(kfile.Provider(cfgPath), kyaml.Parser()); err != nil {
				return fmt.Errorf("failed to load config file %s: %w", cfgPath, err)
			}
		} else {
			// attempt default filenames (badsmtp.yaml, badsmtp.yml)
			for _, fn := range []string{"badsmtp.yaml", "badsmtp.yml", "badsmtp.json"} {
				if _, err := os.Stat(fn); err == nil {
					if err := k.Load(kfile.Provider(fn), kyaml.Parser()); err != nil {
						return fmt.Errorf("failed to load config file %s: %w", fn, err)
					}
					break
				}
			}
		}

		// Load environment variables (prefix BADSMTP) into koanf
		// use a replacer function to map ENV names to koanf keys
		if err := k.Load(kenv.Provider("BADSMTP_", "_", createEnvReplacer().Replace), nil); err != nil {
			return fmt.Errorf("failed to load env: %w", err)
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

// RegisterFlags registers persistent flags for the root command. This replaces an init() function
// to satisfy the linter rule against init usage and allows callers to control ordering.
func RegisterFlags() {
	pf := rootCmd.PersistentFlags()
	pf.IntP("port", "p", server.DefaultPort, "Port to listen on")
	pf.StringP("mailbox", "m", "./mailbox", "Directory to store messages")
	pf.StringP("config", "c", "", "Configuration file path")
	pf.Bool("enable-hostname-routing", false, "Enable hostname-based routing")
	pf.String("default-mailbox-dir", "", "Default mailbox directory for unmapped hostnames")

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
