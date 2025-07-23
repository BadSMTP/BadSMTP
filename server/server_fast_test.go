//go:build fasttests

package server

import (
	"testing"
)

func TestServerFast(t *testing.T) {
	t.Log("Running fast server tests only")

	// Add fast server tests here that don't involve port delays
	// For example, testing config loading, basic server creation, etc.
}
