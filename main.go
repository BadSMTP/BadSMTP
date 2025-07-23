package main

import (
	"log"

	"badsmtp/cmd"
)

// Version is set at build time via ldflags
var Version = "dev"

func main() {
	// Ensure CLI flags are registered before executing the command
	cmd.RegisterFlags()

	if err := cmd.Execute(Version); err != nil {
		log.Fatalf("%v", err)
	}
}
