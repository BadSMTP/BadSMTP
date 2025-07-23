#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
# Load pinned versions
if [ -f "$ROOT_DIR/.github/tool-versions.env" ]; then
  # shellcheck source=/dev/null
  source "$ROOT_DIR/.github/tool-versions.env"
fi

echo "Installing CI tools with pinned versions..."

echo "Ensuring GOPATH/bin is available"
export PATH="$(go env GOPATH 2>/dev/null)/bin:$PATH"

# Install golangci-lint
if [ -n "${GOLANGCI_LINT_VERSION:-}" ]; then
  echo "Installing golangci-lint ${GOLANGCI_LINT_VERSION}"
  go install github.com/golangci/golangci-lint/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}
else
  echo "Installing golangci-lint latest"
  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
fi

# Install staticcheck
if [ -n "${STATICCHECK_VERSION:-}" ]; then
  echo "Installing staticcheck ${STATICCHECK_VERSION}"
  go install honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION}
else
  echo "Installing staticcheck latest"
  go install honnef.co/go/tools/cmd/staticcheck@latest
fi

# Install govulncheck
if [ -n "${GOVULNCHECK_VERSION:-}" ]; then
  echo "Installing govulncheck ${GOVULNCHECK_VERSION}"
  go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}
else
  echo "Installing govulncheck latest"
  go install golang.org/x/vuln/cmd/govulncheck@latest
fi

echo "Tool install completed. Ensure \"$(go env GOPATH)/bin\" is in your PATH before system-wide binaries."

echo "Installed versions:"
command -v golangci-lint >/dev/null && golangci-lint --version || true
command -v staticcheck >/dev/null && staticcheck --version || true
command -v govulncheck >/dev/null && govulncheck --version || true

